// SPDX-License-Identifier: GPL-3.0-only

package api

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"connectrpc.com/connect"

	lavrayv1 "github.com/anatolijlaptev1991-ctrl/lavray-android/core/internal/gen/lavrayv1"
	"github.com/anatolijlaptev1991-ctrl/lavray-android/core/internal/updates/updatestest"
)

func TestCheckUpdatesWithFakeSource(t *testing.T) {
	f := newUpdFix(t, "0.5.0", nil)
	f.release(updatestest.Release{Version: "0.5.1", Serial: 3, TSVersion: 10, Mandatory: true,
		Notes: map[string]string{"ru": "Исправления"}})
	got, err := f.check()
	if err != nil || len(got.Available) != 1 {
		t.Fatalf("доступное обновление: %+v %v", got, err)
	}
	u := got.Available[0]
	if u.GetAvailableVersion() != "0.5.1" || u.CurrentVersion != "0.5.0" || !u.Mandatory || u.Notes["ru"] != "Исправления" {
		t.Fatalf("ComponentUpdate: %+v", u)
	}
	if got.CurrentAppVersion != "0.5.0" || got.LastCheckedAtMs == 0 {
		t.Fatalf("версия/время проверки: %+v", got)
	}
	state, _ := f.cl.GetUpdateState(context.Background(), connect.NewRequest(&lavrayv1.GetUpdateStateRequest{}))
	if state.Msg.LastCheckedAtMs != got.LastCheckedAtMs || state.Msg.ChainState.MaxSerial["stable"] != 3 ||
		state.Msg.ChainState.RootVersion != 1 || !state.Msg.ChainState.TimestampFresh {
		t.Fatalf("GetUpdateState после проверки: %+v", state.Msg)
	}
}

func TestCheckUpdatesIgnoresOlderVersion(t *testing.T) {
	f := newUpdFix(t, "0.5.0", nil)
	f.release(updatestest.Release{Version: "0.4.0", Serial: 9, TSVersion: 1})
	got, err := f.check()
	if err != nil || len(got.Available) != 0 || got.LastCheckedAtMs == 0 {
		t.Fatalf("старая версия не предлагается, но проверка успешна: %+v %v", got, err)
	}
}

func TestCheckUpdatesNotConfigured(t *testing.T) {
	for name, mutate := range map[string]func(*UpdaterConf){
		"пустой корень": func(c *UpdaterConf) { c.RootJSON = nil },
		"заглушка":      nil,
	} {
		f := newUpdFix(t, "0.5.0", mutate)
		if name == "заглушка" {
			f.srv.upd.conf = nil // сборка без Deps.Updater
		}
		_, err := f.check()
		var ce *connect.Error
		if err == nil || !errors.As(err, &ce) || ce.Code() != connect.CodeFailedPrecondition {
			t.Fatalf("%s: ожидался FAILED_PRECONDITION: %v", name, err)
		}
		if !strings.HasPrefix(ce.Message(), "not_configured: ") || ce.Meta().Get(UpdateReasonHeader) != ReasonNotConfigured {
			t.Fatalf("%s: причина not_configured обязана быть в сообщении и заголовке: %q", name, ce.Message())
		}
		state, err := f.cl.GetUpdateState(context.Background(), connect.NewRequest(&lavrayv1.GetUpdateStateRequest{}))
		if err != nil || state.Msg.LastCheckedAtMs != 0 {
			t.Fatalf("%s: GetUpdateState без конфигурации: %v", name, err)
		}
	}
}

func TestCheckFailedIsUnavailableNotUpToDate(t *testing.T) {
	f := newUpdFix(t, "0.5.0", nil)
	f.release(updatestest.Release{Version: "0.6.0", Serial: 1, TSVersion: 1})
	if _, err := f.check(); err != nil {
		t.Fatal(err)
	}
	before := f.srv.loadUpdateState().LastCheckedAt
	f.src.Down = true
	_, err := f.check()
	if connect.CodeOf(err) != connect.CodeUnavailable {
		t.Fatalf("все источники недоступны → UNAVAILABLE, получено %v", err)
	}
	if after := f.srv.loadUpdateState().LastCheckedAt; after != before {
		t.Fatalf("last_checked_at изменился при неудачной проверке: %d → %d", before, after)
	}
}

func TestCheckUpdatesReasons(t *testing.T) {
	f := newUpdFix(t, "0.5.0", nil)
	f.release(updatestest.Release{Version: "0.6.0", Serial: 5, TSVersion: 5})
	if _, err := f.check(); err != nil {
		t.Fatal(err)
	}
	// откат serial: источник теперь отдаёт serial 2 при сохранённом 5
	f.release(updatestest.Release{Version: "0.6.0", Serial: 2, TSVersion: 6})
	_, err := f.check()
	var ce *connect.Error
	if !errors.As(err, &ce) || ce.Code() != connect.CodeFailedPrecondition || ce.Meta().Get(UpdateReasonHeader) != ReasonSerialRollback {
		t.Fatalf("откат serial: %v", err)
	}
	// чужой продукт
	g := newUpdFix(t, "0.5.0", func(c *UpdaterConf) { c.Product = "lavray-linux" })
	g.release(updatestest.Release{Version: "0.6.0", Serial: 1, TSVersion: 1})
	if _, err = g.check(); !errors.As(err, &ce) || ce.Meta().Get(UpdateReasonHeader) != ReasonWrongProduct {
		t.Fatalf("чужой продукт: %v", err)
	}
}

func TestAgentUpdateDecisions(t *testing.T) {
	read := "/lavray.v1.UpdateService/GetUpdateState"
	for _, p := range []string{"CheckUpdates", "DownloadUpdates", "ApplyUpdates", "Rollback", "GetUpdateState", "VerifyLocalUpdate"} {
		proc := "/lavray.v1.UpdateService/" + p
		want := agentDeny
		if proc == read {
			want = agentRead
		}
		if agentTable[proc] != want {
			t.Errorf("%s: решение для агентов %v, ожидалось %v", proc, agentTable[proc], want)
		}
	}
}

// Зеркало молчит: проверка не удалась (UNAVAILABLE), но причина названа и в
// сообщении есть адрес зеркала — интерфейс подскажет подключиться и повторить.
func TestCheckMirrorUnreachableNamesTheHost(t *testing.T) {
	dead := httptest.NewServer(http.NotFoundHandler())
	host := strings.TrimPrefix(dead.URL, "http://")
	dead.Close()
	f := newUpdFix(t, "0.5.0", func(c *UpdaterConf) { c.BootstrapMetadata = []string{dead.URL} })
	f.release(updatestest.Release{Version: "0.6.0", Serial: 1, TSVersion: 1})
	_, err := f.check()
	var ce *connect.Error
	if !errors.As(err, &ce) || ce.Code() != connect.CodeUnavailable {
		t.Fatalf("ожидался UNAVAILABLE (проверка не удалась): %v", err)
	}
	if !strings.HasPrefix(ce.Message(), ReasonMirrorUnreachable+": ") || !strings.Contains(ce.Message(), host) {
		t.Fatalf("сообщение без причины и адреса зеркала: %q", ce.Message())
	}
	if ce.Meta().Get(UpdateReasonHeader) != ReasonMirrorUnreachable {
		t.Fatalf("заголовок причины: %q", ce.Meta().Get(UpdateReasonHeader))
	}
	if f.srv.loadUpdateState().LastCheckedAt != 0 {
		t.Fatal("last_checked_at не меняется при неудачной проверке")
	}
}

// Подменённый манифест: FAILED_PRECONDITION с устойчивой причиной manifest_mismatch
// в начале сообщения и в заголовке, как у остальных причин проверки.
func TestCheckSwappedManifestHasReason(t *testing.T) {
	f := newUpdFix(t, "0.5.0", nil)
	f.release(updatestest.Release{Version: "0.6.0", Serial: 1, TSVersion: 1})
	old, err := f.src.Fetch(context.Background(), "manifest-stable.json")
	if err != nil {
		t.Fatal(err)
	}
	f.release(updatestest.Release{Version: "0.7.0", Serial: 2, TSVersion: 2})
	f.src.Put("manifest-stable.json", old) // подпись настоящая, документ не тот, что пиннит timestamp
	_, err = f.check()
	var ce *connect.Error
	if !errors.As(err, &ce) || ce.Code() != connect.CodeFailedPrecondition {
		t.Fatalf("ожидался FAILED_PRECONDITION: %v", err)
	}
	if ce.Meta().Get(UpdateReasonHeader) != ReasonManifestMismatch || !strings.HasPrefix(ce.Message(), ReasonManifestMismatch+": ") {
		t.Fatalf("причина manifest_mismatch в заголовке и сообщении: %q %q", ce.Meta().Get(UpdateReasonHeader), ce.Message())
	}
	if f.srv.loadUpdateState().LastCheckedAt != 0 {
		t.Fatal("last_checked_at не меняется при неудачной проверке")
	}
}
