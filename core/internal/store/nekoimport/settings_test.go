// SPDX-License-Identifier: GPL-3.0-only

package nekoimport

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/anatolijlaptev1991-ctrl/lavray-android/core/internal/model"
)

func settingsOf(t *testing.T, raw string) ([]PlanSetting, string, []string) {
	t.Helper()
	var top map[string]json.RawMessage
	if err := json.Unmarshal([]byte(raw), &top); err != nil {
		t.Fatal(err)
	}
	return MapSettings(top)
}

func settingMap(list []PlanSetting) map[string]string {
	out := map[string]string{}
	for _, s := range list {
		out[s.Key] = s.JSON
	}
	return out
}

// Таблица соответствия: типы и преобразования значений (H).
func TestSettingsMappingTable(t *testing.T) {
	list, active, warns := settingsOf(t, `{
		"inbound_address":"127.0.0.1","inbound_socks_port":2080,
		"inbound_auth":{"user":"u","pass":"p"},
		"sub_use_proxy":true,"mux_concurrency":-1,"vpn_strict_route":1,
		"spmode2":["system_proxy","tun"],"hk_mw":"Ctrl+Alt+M",
		"core_box_clash_api":-9090,
		"remember_id":3,"current_group":1,"active_routing":"Мой маршрут"}`)
	got := settingMap(list)
	want := map[string]string{
		"inbound.address": `"127.0.0.1"`, "inbound.port": `2080`,
		"inbound.auth.user": `"u"`, "inbound.auth.pass": `"p"`,
		"sub.use_proxy": `true`, "mux.concurrency": `-1`, "tun.strict_route": `true`,
		"remember.modes": `["system_proxy","tun"]`, "hotkeys.main_window": `"Ctrl+Alt+M"`,
		"core.clash_api.enabled": `false`,
		"remember.id":            `3`, "ui.current_group": `1`,
	}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("%s = %q, ожидалось %q", k, got[k], v)
		}
	}
	if active != "Мой маршрут" {
		t.Errorf("активный маршрут %q", active)
	}
	if len(warns) != 0 {
		t.Errorf("предупреждения: %v", warns)
	}
}

// Каждая строка таблицы ведёт на существующий ключ LAVRay нужного типа.
func TestSettingRulesMatchDescriptors(t *testing.T) {
	desc := map[string]model.SettingDescriptor{}
	for _, d := range model.SettingDescriptors() {
		desc[d.Key] = d
	}
	probe := map[model.SettingType]string{
		model.SettingString: `"x"`, model.SettingBool: `true`, model.SettingInt: `5`,
		model.SettingStringList: `["a"]`,
	}
	if r := settingRules; len(r) == 0 {
		t.Fatal("пустая таблица")
	}
	for _, r := range settingRules {
		if r.lavray == "" {
			continue
		}
		d, ok := desc[r.lavray]
		if !ok {
			t.Errorf("%s: ключа %s нет в дескрипторах", r.neko, r.lavray)
			continue
		}
		if d.Internal {
			t.Errorf("%s: ключ %s служебный", r.neko, r.lavray)
		}
		switch r.neko {
		case "core_box_clash_api", "theme", "language":
			continue // значение выводится из другого типа или из набора допустимых
		}
		in, out := probe[d.Type], ""
		if got, ok := r.conv(json.RawMessage(in)); ok {
			out = got
		}
		if in != "" && out != in {
			t.Errorf("%s → %s (%s): значение %s дало %q", r.neko, r.lavray, d.Type, in, out)
		}
	}
}

// Неизвестные ключи — одно предупреждение со списком; неверный тип — отдельное (I).
func TestUnknownSettingsWarned(t *testing.T) {
	list, _, warns := settingsOf(t, `{"inbound_socks_port":"abc","zzz_future":1,"aaa_other":2,"conn_stat":true,"extraCore":{"core_map":"{}"},"theme":"Fusion","language":2,"mw_size":"AAAA"}`)
	if len(list) != 0 {
		t.Errorf("перенесено лишнее: %v", list)
	}
	joined := strings.Join(warns, "\n")
	for _, want := range []string{"inbound_socks_port не перенесена", "2 (aaa_other, zzz_future)", "conn_stat не переносится", "extraCore не переносится", "theme не перенесена", "language не перенесена", "mw_size не переносится"} {
		if !strings.Contains(joined, want) {
			t.Errorf("нет предупреждения %q в\n%s", want, joined)
		}
	}
}

func TestUnknownWarningIsCapped(t *testing.T) {
	keys := make([]string, 40)
	for i := range keys {
		keys[i] = "k" + string(rune('a'+i%26)) + string(rune('a'+i/26))
	}
	if w := unknownWarning(keys); !strings.Contains(w, "и ещё 10") {
		t.Errorf("предупреждение не обрезано: %s", w)
	}
}

// Ссылки на id NekoBox заменяются локальными; без переноса записи — настройка отброшена.
func TestRemapSettingRefs(t *testing.T) {
	plan, err := Scan(fixtureConfig)
	if err != nil {
		t.Fatal(err)
	}
	st, _ := LoadState(openStore(t))
	res := Remap(plan, st)
	got := settingMap(res.Settings)
	if got["remember.id"] != "3" || got["ui.current_group"] != "1" {
		t.Errorf("ссылки: %v", got)
	}
	plan.Settings = append(plan.Settings, PlanSetting{Key: "remember.id", JSON: "99", Ref: RefProfile, RefID: 99})
	res = Remap(plan, st)
	if !hasWarning(res.Report.Warnings, "setting remember.id not transferred") {
		t.Errorf("нет предупреждения об отброшенной ссылке: %v", res.Report.Warnings)
	}
}

// Настройки фикстуры доходят до плана целиком.
func TestFixtureSettingsScanned(t *testing.T) {
	plan, err := Scan(fixtureConfig)
	if err != nil {
		t.Fatal(err)
	}
	got := settingMap(plan.Settings)
	if got["inbound.port"] != "2080" || got["inbound.auth.user"] != `"demo"` || got["hotkeys.main_window"] != `"Ctrl+Alt+M"` {
		t.Errorf("настройки плана: %v", got)
	}
	if !hasWarning(plan.Report.Warnings, "future_option_x") {
		t.Errorf("неизвестные ключи не названы: %v", plan.Report.Warnings)
	}
}
