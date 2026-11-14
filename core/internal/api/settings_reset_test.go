// SPDX-License-Identifier: GPL-3.0-only

package api

import (
	"context"
	"slices"
	"testing"

	"connectrpc.com/connect"

	lavrayv1 "github.com/anatolijlaptev1991-ctrl/lavray-android/core/internal/gen/lavrayv1"
	"github.com/anatolijlaptev1991-ctrl/lavray-android/core/internal/model"
)

func (f *svcFixture) setInt(t *testing.T, key string, v int64) {
	t.Helper()
	_, err := f.srv.SetSettings(context.Background(), newReq(&lavrayv1.SetSettingsRequest{
		Values: map[string]*lavrayv1.SettingValue{key: {Value: &lavrayv1.SettingValue_IntValue{IntValue: v}}},
	}))
	if err != nil {
		t.Fatalf("SetSettings %s: %v", key, err)
	}
}

func (f *svcFixture) reset(keys ...string) (*lavrayv1.SetSettingsResponse, error) {
	res, err := f.srv.ResetSettings(context.Background(), newReq(&lavrayv1.ResetSettingsRequest{Keys: keys}))
	if err != nil {
		return nil, err
	}
	return res.Msg, nil
}

// Сброс отдельных ключей возвращает их к умолчанию, остальные не трогает.
func TestResetKeys(t *testing.T) {
	f := newSvcFixture(t, nil)
	f.setInt(t, "inbound.port", 3000)
	f.setInt(t, "tun.mtu", 1500)

	got, err := f.reset("inbound.port")
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(got.AcceptedKeys, []string{"inbound.port"}) || got.NeedsProfileRestart || got.NeedsAppRestart {
		t.Fatalf("ответ: %v", got)
	}
	if v := f.srv.settingInt("inbound.port"); v != 2080 {
		t.Errorf("inbound.port = %d, want 2080", v)
	}
	if v := f.srv.settingInt("tun.mtu"); v != 1500 {
		t.Errorf("tun.mtu = %d, want 1500 (не сбрасывался)", v)
	}
	if _, ok, _ := f.srv.st.GetSetting("inbound.port"); ok {
		t.Error("запись inbound.port осталась в хранилище")
	}
}

// Ключ, меняющий конфиг, при работающем соединении требует перезапуска профиля.
func TestResetNeedsProfileRestart(t *testing.T) {
	f := newSvcFixture(t, nil)
	f.setInt(t, "inbound.port", 3000)
	f.setInt(t, "tun.mtu", 1500)
	f.activate(t, f.trojan(t).ID)

	got, err := f.reset("inbound.port")
	if err != nil || !got.NeedsProfileRestart {
		t.Fatalf("resp=%v err=%v: нужен перезапуск профиля", got, err)
	}
	// Ключ, который и так по умолчанию, ничего не меняет — перезапуск не нужен.
	again, err := f.reset("inbound.port")
	if err != nil || again.NeedsProfileRestart {
		t.Fatalf("повтор: resp=%v err=%v", again, err)
	}
}

// Пустой список сбрасывает все настройки, кроме служебных и update.*.
func TestResetAll(t *testing.T) {
	f := newSvcFixture(t, nil)
	f.setInt(t, "inbound.port", 3000)
	f.setInt(t, "tun.mtu", 1500)
	for key, raw := range map[string]string{
		model.SettingHWIDID:      `"hwid-123"`,
		"update.serial_stable":   `7`,
		"update.channel":         `"beta"`,
		"update.last_checked_at": `1700000000`,
	} {
		if err := f.srv.st.SetSetting(key, raw); err != nil {
			t.Fatal(err)
		}
	}

	got, err := f.reset()
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(got.AcceptedKeys, "inbound.port") || slices.Contains(got.AcceptedKeys, model.SettingHWIDID) ||
		slices.Contains(got.AcceptedKeys, "update.channel") {
		t.Fatalf("accepted = %v", got.AcceptedKeys)
	}
	if f.srv.settingInt("inbound.port") != 2080 || f.srv.settingInt("tun.mtu") != 9000 {
		t.Error("настройки не вернулись к умолчанию")
	}
	left, err := f.srv.st.Settings()
	if err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{model.SettingHWIDID, "update.serial_stable", "update.channel", "update.last_checked_at"} {
		if _, ok := left[key]; !ok {
			t.Errorf("служебный ключ %s удалён сбросом", key)
		}
	}
	if len(left) != 4 {
		t.Errorf("осталось %d записей, want только 4 служебные: %v", len(left), left)
	}
}

// Неизвестный ключ — INVALID_ARGUMENT, и ни один из ключей запроса не сброшен.
func TestResetUnknownInvalid(t *testing.T) {
	f := newSvcFixture(t, nil)
	f.setInt(t, "inbound.port", 3000)

	_, err := f.reset("inbound.port", "no.such.key")
	if connect.CodeOf(err) != connect.CodeInvalidArgument {
		t.Fatalf("code = %v, err = %v", connect.CodeOf(err), err)
	}
	if f.srv.settingInt("inbound.port") != 3000 {
		t.Fatal("допустимый ключ сброшен, хотя запрос отвергнут")
	}
}

// Служебные ключи и update.* сбросить нельзя.
func TestResetSkipsInternal(t *testing.T) {
	f := newSvcFixture(t, nil)
	if err := f.srv.st.SetSetting(model.SettingHWIDID, `"hwid-123"`); err != nil {
		t.Fatal(err)
	}
	if err := f.srv.st.SetSetting("update.channel", `"beta"`); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{model.SettingHWIDID, "update.channel", "update.serial_stable"} {
		if _, err := f.reset(key); connect.CodeOf(err) != connect.CodeInvalidArgument {
			t.Errorf("%s: code = %v, err = %v", key, connect.CodeOf(err), err)
		}
	}
	if v, _, _ := f.srv.st.GetSetting(model.SettingHWIDID); v != `"hwid-123"` {
		t.Errorf("hwid изменён: %q", v)
	}
	if v, _, _ := f.srv.st.GetSetting("update.channel"); v != `"beta"` {
		t.Errorf("update.channel изменён: %q", v)
	}
}

// Сброс публикуется подписчикам и не создаёт событий, когда сбрасывать нечего.
func TestResetPublishesOnlyRemoved(t *testing.T) {
	f := newSvcFixture(t, nil)
	f.setInt(t, "inbound.port", 3000)
	ch, unsubscribe := f.srv.subscribeSettings()
	defer unsubscribe()

	if _, err := f.reset("inbound.port", "tun.mtu"); err != nil {
		t.Fatal(err)
	}
	ev := <-ch
	if !slices.Equal(ev.Removed, []string{"inbound.port"}) {
		t.Fatalf("removed = %v, want только имевшие запись", ev.Removed)
	}
	if _, err := f.reset("inbound.port"); err != nil {
		t.Fatal(err)
	}
	select {
	case ev := <-ch:
		t.Fatalf("лишнее событие: %v", ev)
	default:
	}
}
