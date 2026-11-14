// SPDX-License-Identifier: GPL-3.0-only

package api

import (
	"context"
	"net/http"
	"strings"
	"testing"
	"time"

	"connectrpc.com/connect"

	lavrayv1 "github.com/anatolijlaptev1991-ctrl/lavray-android/core/internal/gen/lavrayv1"
	"github.com/anatolijlaptev1991-ctrl/lavray-android/core/internal/model"
	"github.com/anatolijlaptev1991-ctrl/lavray-android/core/internal/updates/updatestest"
)

func updatesRelease(version string, data []byte) updatestest.Release {
	return updatestest.Release{Version: version, Serial: 4, TSVersion: 1, Data: data}
}

// P: обновления идут через туннель, когда он поднят, и напрямую, когда нет;
// отказ туннеля не оставляет пользователя без обновлений.
func TestUpdatesTryTunnelFirst(t *testing.T) {
	var direct []string
	f := newSvcFixture(t, &http.Client{Transport: textRT("direct-body", &direct)})
	proxy := startFakeProxy(t, "tunnel-body")
	get := func() string {
		req, _ := http.NewRequest(http.MethodGet, "http://updates.test/timestamp.json", nil)
		resp, err := f.srv.updateHTTPClient().Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		buf := make([]byte, 64)
		n, _ := resp.Body.Read(buf)
		return string(buf[:n])
	}
	if got := get(); got != "direct-body" || len(direct) != 1 {
		t.Fatalf("туннель не поднят — напрямую: %q (%v)", got, direct)
	}
	f.bringUpTunnel(t, proxy.port(), "system_proxy")
	if got := get(); got != "tunnel-body" || len(proxy.requests()) != 1 || len(direct) != 1 {
		t.Fatalf("туннель поднят — сначала через него: %q, прокси=%v, прямых=%d", got, proxy.requests(), len(direct))
	}
	proxy.srv.Close() // mixed-in упал, а инстанс ещё числится запущенным
	if got := get(); got != "direct-body" || len(direct) != 2 {
		t.Fatalf("туннель недоступен — запасной прямой путь: %q прямых=%d", got, len(direct))
	}
}

func TestClientCannotSetTrustKeys(t *testing.T) {
	f := newUpdFix(t, "0.5.0", nil)
	f.release(updatesRelease("0.6.0", nil))
	if _, err := f.check(); err != nil {
		t.Fatal(err)
	}
	keys := []string{model.SettingUpdateRootJSON, model.SettingUpdateTimestampVersion, model.SettingUpdateSerialStable,
		model.SettingUpdateSerialBeta, model.SettingUpdateRootVersion, model.SettingUpdateLastChecked}
	before := map[string]string{}
	vals := map[string]*lavrayv1.SettingValue{}
	for _, k := range keys {
		v, _, _ := f.st.GetSetting(k)
		before[k] = v
		vals[k] = &lavrayv1.SettingValue{Value: &lavrayv1.SettingValue_StringValue{StringValue: "999"}}
	}
	cl := newSettingsClient(t, f)
	resp, err := cl.SetSettings(context.Background(), connect.NewRequest(&lavrayv1.SetSettingsRequest{Values: vals}))
	if err != nil {
		t.Fatalf("SetSettings: %v", err)
	}
	if len(resp.Msg.AcceptedKeys) != 0 {
		t.Fatalf("служебные ключи обновлений не принимаются: accepted=%v", resp.Msg.AcceptedKeys)
	}
	for _, k := range keys {
		if v, _, _ := f.st.GetSetting(k); v != before[k] {
			t.Errorf("%s изменён клиентом: %q → %q", k, before[k], v)
		}
	}
	got, _ := cl.GetSettings(context.Background(), connect.NewRequest(&lavrayv1.GetSettingsRequest{}))
	for _, k := range keys {
		if _, leaked := got.Msg.Values[k]; leaked {
			t.Errorf("служебный ключ %s отдан клиенту", k)
		}
		if !model.BackupExcluded(k) {
			t.Errorf("%s должен исключаться из резервной копии", k)
		}
	}
}

func TestUpdateAutoCheckDescribed(t *testing.T) {
	f := newUpdFix(t, "0.5.0", nil)
	cl := newSettingsClient(t, f)
	d, err := cl.DescribeSettings(context.Background(), connect.NewRequest(&lavrayv1.DescribeSettingsRequest{}))
	if err != nil {
		t.Fatal(err)
	}
	var found bool
	for _, ds := range d.Msg.Descriptors {
		found = found || (ds.Key == "update.auto_check" && ds.GetDefaultValueJson() == "true")
		if strings.HasPrefix(ds.Key, "update.") && model.BackupExcluded(ds.Key) {
			t.Errorf("служебный ключ %s виден в DescribeSettings", ds.Key)
		}
	}
	if !found {
		t.Fatal("update.auto_check (по умолчанию true) не описан")
	}
}

func TestSchedulerJitter(t *testing.T) {
	cases := map[float64]time.Duration{0: 5*time.Hour + 30*time.Minute, 0.5: 6 * time.Hour, 0.999999: 6*time.Hour + 30*time.Minute}
	for r, want := range cases {
		got := nextUpdateDelay(func() float64 { return r })
		if d := got - want; d < -time.Second || d > time.Second {
			t.Errorf("rnd=%v: %v, ожидалось ≈%v", r, got, want)
		}
	}
	for i := 0; i < 1000; i++ {
		if d := nextUpdateDelay(func() float64 { return float64(i) / 1000 }); d < 5*time.Hour+30*time.Minute || d > 6*time.Hour+30*time.Minute {
			t.Fatalf("задержка вне 6ч±30м: %v", d)
		}
	}
}

func TestSchedulerRespectsAutoCheck(t *testing.T) {
	f := newUpdFix(t, "0.5.0", nil)
	f.release(updatesRelease("0.6.0", nil))
	ctx := context.Background()
	if err := f.st.SetSetting("update.auto_check", "false"); err != nil {
		t.Fatal(err)
	}
	if f.srv.updateTick(ctx) || f.srv.loadUpdateState().LastCheckedAt != 0 {
		t.Fatal("автопроверка выключена — проверки быть не должно")
	}
	_ = f.st.SetSetting("update.auto_check", "true")
	if !f.srv.updateTick(ctx) || f.srv.loadUpdateState().LastCheckedAt == 0 {
		t.Fatal("автопроверка включена — состояние обновляется")
	}
	f.src.Down = true // сбой фоновой проверки не меняет состояние и не паникует
	last := f.srv.loadUpdateState().LastCheckedAt
	f.srv.updateTick(ctx)
	if f.srv.loadUpdateState().LastCheckedAt != last {
		t.Fatal("неудачная фоновая проверка не меняет last_checked_at")
	}
	g := newUpdFix(t, "0.5.0", func(c *UpdaterConf) { c.RootJSON = nil })
	if g.srv.updateTick(ctx) {
		t.Fatal("без корня доверия планировщик ничего не делает")
	}
	stop := g.srv.StartUpdateScheduler(ctx)
	stop()
}

// Принятая цепочка корней лежит файлами в updates/roots и читается обратно;
// подложенный файл с подделкой в цепочку не попадает.
func TestRootStoreIsAChainOnDisk(t *testing.T) {
	f := newUpdFix(t, "0.5.0", nil)
	rs := f.srv.rootStore()
	if docs, err := rs.LoadRoots(); err != nil || len(docs) != 0 {
		t.Fatalf("пустое хранилище: %v %v", docs, err)
	}
	forged := updatestest.New(t)
	if err := rs.SaveRoot(7, forged.RootJSON); err != nil {
		t.Fatal(err)
	}
	c, _, err := f.srv.updateClient(lavrayv1.UpdateChannel_UPDATE_CHANNEL_STABLE)
	if err != nil || c.Trust.Root().Version != 1 {
		t.Fatalf("подложенный корень не должен приниматься: %v", err)
	}
}

// После ротации сброшенные счётчики записываются как есть, а не через максимум.
func TestSaveUpdateStateHonorsReset(t *testing.T) {
	f := newUpdFix(t, "0.5.0", nil)
	_ = f.st.SetSetting(setUpdTSVer, "9223372036854775807")
	_ = f.st.SetSetting(setUpdSerialSt, "9223372036854775807")
	st := f.srv.loadUpdateState()
	st.TimestampVersion, st.SerialStable = 5, 2
	f.srv.saveUpdateState(&st)
	if got := f.srv.loadUpdateState(); got.TimestampVersion != 9223372036854775807 || got.SerialStable != 9223372036854775807 {
		t.Fatalf("без сброса счётчики только растут: %+v", got)
	}
	st.TimestampVersion, st.SerialStable, st.ResetTimestamp, st.ResetSerials = 5, 2, true, true
	f.srv.saveUpdateState(&st)
	if got := f.srv.loadUpdateState(); got.TimestampVersion != 5 || got.SerialStable != 2 {
		t.Fatalf("после сброса пишутся новые значения: %+v", got)
	}
	if st.ResetTimestamp || st.ResetSerials {
		t.Fatal("флаги сброса гасятся после записи")
	}
}

func TestUpdateSettingKeysMatchModel(t *testing.T) {
	pairs := map[string]string{
		setUpdRoot: model.SettingUpdateRootVersion, setUpdSerialSt: model.SettingUpdateSerialStable,
		setUpdSerialBe: model.SettingUpdateSerialBeta, setUpdChecked: model.SettingUpdateLastChecked,
		setUpdLastURL: model.SettingUpdateLastURL, setUpdTSVer: model.SettingUpdateTimestampVersion,
	}
	for got, want := range pairs {
		if got != want {
			t.Errorf("ключ api %q расходится с model %q", got, want)
		}
		if !model.BackupExcluded(want) {
			t.Errorf("%s должен исключаться из копии", want)
		}
	}
}
