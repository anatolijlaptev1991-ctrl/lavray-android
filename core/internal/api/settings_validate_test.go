// SPDX-License-Identifier: GPL-3.0-only

package api

import (
	"context"
	"testing"

	"connectrpc.com/connect"

	lavrayv1 "github.com/anatolijlaptev1991-ctrl/lavray-android/core/internal/gen/lavrayv1"
)

func getSettings(t *testing.T, s *Server, keys ...string) map[string]*lavrayv1.SettingValue {
	t.Helper()
	r, err := s.GetSettings(context.Background(), connect.NewRequest(&lavrayv1.GetSettingsRequest{Keys: keys}))
	if err != nil {
		t.Fatal(err)
	}
	return r.Msg.Values
}

func boolSetting(v bool) *lavrayv1.SettingValue {
	return &lavrayv1.SettingValue{Value: &lavrayv1.SettingValue_BoolValue{BoolValue: v}}
}

// T-34 (B2-13): свежая установка отдаёт значения по умолчанию, а не пустоту.
func TestGetSettingsReturnsDefaults(t *testing.T) {
	s := newSvcFixture(t, nil).srv
	v := getSettings(t, s)
	if v["inbound.port"].GetIntValue() != 2080 || v["inbound.address"].GetStringValue() != "127.0.0.1" ||
		v["tun.mtu"].GetIntValue() != 9000 || !v["sub.use_proxy"].GetBoolValue() || v["mux.protocol"].GetStringValue() != "h2mux" {
		t.Errorf("значения по умолчанию не отданы: port=%v addr=%v mtu=%v use_proxy=%v mux=%v",
			v["inbound.port"], v["inbound.address"], v["tun.mtu"], v["sub.use_proxy"], v["mux.protocol"])
	}
	if only := getSettings(t, s, "inbound.port"); len(only) != 1 {
		t.Errorf("фильтр по ключам вернул %d значений", len(only))
	}
	// Записанное значение важнее значения по умолчанию.
	if err := setSetting(t, s, "inbound.port", intSetting(3000)); err != nil {
		t.Fatal(err)
	}
	if got := getSettings(t, s, "inbound.port")["inbound.port"].GetIntValue(); got != 3000 {
		t.Errorf("порт после записи = %d", got)
	}
}

// T-34 (B2-13, A1-16): невалидное отвергается целиком и ничего не записывает.
func TestSetSettingsRejectsInvalidValues(t *testing.T) {
	s := newSvcFixture(t, nil).srv
	bad := []struct {
		key string
		val *lavrayv1.SettingValue
	}{
		{"inbound.port", intSetting(0)}, // то, во что UI превращал пустое поле
		{"inbound.port", intSetting(99999)},
		{"inbound.port", intSetting(-1)},
		{"inbound.port", strSetting("80")}, // тип не тот
		{"inbound.address", strSetting("")},
		{"inbound.address", strSetting("not-an-ip")},
		{"tun.mtu", intSetting(-5)},
		{"tun.mtu", intSetting(70000)},
		{"tun.impl", intSetting(9)},
		{"mux.protocol", strSetting("")},
		{"mux.protocol", strSetting("quic")},
		{"tun.fake_dns", intSetting(1)},
		{"routing.custom_global", &lavrayv1.SettingValue{Value: &lavrayv1.SettingValue_JsonValue{JsonValue: "this is not json"}}},
	}
	for _, c := range bad {
		if err := setSetting(t, s, c.key, c.val); connect.CodeOf(err) != connect.CodeInvalidArgument {
			t.Errorf("%s=%v: ожидался INVALID_ARGUMENT, получено %v", c.key, c.val, err)
		}
	}
	// Пакет с одним плохим значением не применяется частично.
	_, err := s.SetSettings(context.Background(), connect.NewRequest(&lavrayv1.SetSettingsRequest{
		Values: map[string]*lavrayv1.SettingValue{"inbound.port": intSetting(3000), "tun.mtu": intSetting(1)}}))
	if connect.CodeOf(err) != connect.CodeInvalidArgument {
		t.Fatalf("пакет: %v", err)
	}
	if got := getSettings(t, s, "inbound.port")["inbound.port"].GetIntValue(); got != 2080 {
		t.Errorf("отвергнутый пакет изменил порт: %d", got)
	}
	// Корректные граничные значения принимаются.
	for _, ok := range []int64{1, 65535} {
		if err := setSetting(t, s, "inbound.port", intSetting(ok)); err != nil {
			t.Errorf("порт %d отвергнут: %v", ok, err)
		}
	}
}

// Сценарий аудита: пустой порт → 0 → mixed-слушатель исчез → системный прокси на :1.
func TestEmptyPortCanNotRemoveTheListener(t *testing.T) {
	f := newSvcFixture(t, nil)
	s := f.srv
	if err := setSetting(t, s, "inbound.port", intSetting(0)); err == nil {
		t.Fatal("порт 0 принят")
	}
	if p := s.coreStatus().Inbound.Port; p != 2080 {
		t.Errorf("CoreStatus.inbound.port = %d, ожидался 2080", p)
	}
	res, err := s.dryRunBuild(f.trojan(t), lavrayv1.ConnectMode_CONNECT_MODE_SYSTEM_PROXY)
	if err != nil {
		t.Fatal(err)
	}
	if ins, _ := res.Config["inbounds"].([]any); len(ins) == 0 {
		t.Error("в конфиге нет mixed-слушателя")
	}
}

func TestSetSettingsReportsRestartFlagsAndKeepsLists(t *testing.T) {
	f := newSvcFixture(t, nil)
	s, ctx := f.srv, context.Background()
	set := func(key string, v *lavrayv1.SettingValue) *lavrayv1.SetSettingsResponse {
		r, err := s.SetSettings(ctx, connect.NewRequest(&lavrayv1.SetSettingsRequest{Values: map[string]*lavrayv1.SettingValue{key: v}}))
		if err != nil {
			t.Fatal(err)
		}
		return r.Msg
	}
	if set("inbound.port", intSetting(2081)).NeedsProfileRestart {
		t.Error("без подключения перезапуск профиля не нужен")
	}
	p := f.trojan(t)
	if _, err := s.Connect(ctx, connect.NewRequest(&lavrayv1.ConnectRequest{ProfileId: &p.ID})); err != nil {
		t.Fatal(err)
	}
	if !set("inbound.port", intSetting(2082)).NeedsProfileRestart {
		t.Error("порт при активном подключении требует перезапуска профиля")
	}
	if set("ui.theme", strSetting("dark")).NeedsProfileRestart {
		t.Error("тема не требует перезапуска")
	}
	// Список строк переживает запись и чтение.
	list := &lavrayv1.SettingValue{Value: &lavrayv1.SettingValue_StringListValue{StringListValue: &lavrayv1.StringList{Items: []string{"tun", "system_proxy"}}}}
	set("remember.modes", list)
	if got := getSettings(t, s, "remember.modes")["remember.modes"].GetStringListValue().GetItems(); len(got) != 2 || got[0] != "tun" {
		t.Errorf("remember.modes = %v", got)
	}
	d, err := s.DescribeSettings(ctx, connect.NewRequest(&lavrayv1.DescribeSettingsRequest{}))
	if err != nil {
		t.Fatal(err)
	}
	for _, x := range d.Msg.Descriptors {
		if x.Key == "inbound.port" && (!x.RequiresProfileRestart || x.GetDefaultValueJson() != "2080" || x.GroupId != "inbound") {
			t.Errorf("дескриптор inbound.port неполный: %v", x)
		}
	}
}

// T-75: update.channel принимает только известные каналы; всё остальное —
// INVALID_ARGUMENT, значение не записывается (иначе чужой канал молча сводился бы к stable).
func TestSetSettingsUpdateChannelOnlyKnown(t *testing.T) {
	s := newSvcFixture(t, nil).srv
	for _, bad := range []string{"", "nightly", "Stable", "BETA", "stable ", "../beta", "beta" + string(rune(10))} {
		if err := setSetting(t, s, "update.channel", strSetting(bad)); connect.CodeOf(err) != connect.CodeInvalidArgument {
			t.Errorf("update.channel=%q: ожидался INVALID_ARGUMENT, получено %v", bad, err)
		}
	}
	if err := setSetting(t, s, "update.channel", intSetting(1)); connect.CodeOf(err) != connect.CodeInvalidArgument {
		t.Errorf("update.channel=1: %v", err)
	}
	if got := getSettings(t, s, "update.channel")["update.channel"].GetStringValue(); got != "stable" {
		t.Fatalf("отвергнутые значения изменили канал: %q", got)
	}
	for _, ok := range []string{"beta", "stable"} {
		if err := setSetting(t, s, "update.channel", strSetting(ok)); err != nil {
			t.Errorf("update.channel=%q отвергнут: %v", ok, err)
		}
		if got := getSettings(t, s, "update.channel")["update.channel"].GetStringValue(); got != ok {
			t.Errorf("канал после записи %q = %q", ok, got)
		}
	}
}
