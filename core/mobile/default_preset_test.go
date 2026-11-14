// SPDX-License-Identifier: GPL-3.0-only

package mobile

import (
	"encoding/json"
	"reflect"
	"testing"

	"github.com/anatolijlaptev1991-ctrl/lavray-android/core/internal/presets"
	"github.com/anatolijlaptev1991-ctrl/lavray-android/core/internal/store"
)

func builtinByID(t *testing.T) map[string]*presets.Preset {
	t.Helper()
	list, err := presets.BuiltinPresets()
	if err != nil {
		t.Fatal(err)
	}
	out := make(map[string]*presets.Preset, len(list))
	for _, p := range list {
		out[p.ID] = p
	}
	return out
}

// LAV Preset RU Linux — ровно LAV Preset RU без правил для программ: если lav-ru изменится, тест
// упадёт и напомнит пересоздать файл (node tools/presets/lav-ru-linux.mjs).
func TestLinuxPresetIsLavRuWithoutWindowsApps(t *testing.T) {
	all := builtinByID(t)
	linux, ru := all[androidDefaultPreset], all[desktopDefaultPreset]
	if linux == nil || ru == nil {
		t.Fatalf("нет встроенных наборов %q и %q (internal/presets/builtin и BuiltinFiles)", androidDefaultPreset, desktopDefaultPreset)
	}
	if !linux.Builtin {
		t.Fatal("LAV Preset RU Linux не помечен встроенным")
	}
	if len(linux.Routing.ProcessRules) != 0 {
		t.Fatalf("в LAV Preset RU Linux остались правила программ: %+v", linux.Routing.ProcessRules)
	}
	if len(ru.Routing.ProcessRules) == 0 {
		t.Fatal("в LAV Preset RU нет правил программ: сверка потеряла смысл, пересмотрите набор")
	}
	want := ru.Routing
	want.ProcessRules = []presets.ProcessRule{}
	if !reflect.DeepEqual(linux.Routing, want) {
		t.Fatal("маршруты LAV Preset RU Linux разошлись с LAV Preset RU: node tools/presets/lav-ru-linux.mjs")
	}
	if !reflect.DeepEqual(linux.DNS, ru.DNS) {
		t.Fatalf("DNS разошёлся: %+v против %+v", linux.DNS, ru.DNS)
	}
	for lang, name := range linux.Name {
		if name != "LAV Preset RU Linux" {
			t.Fatalf("имя %s = %q", lang, name)
		}
	}
}

func TestDefaultPresetOnFreshInstall(t *testing.T) {
	st := openStore(t)
	if err := applyAndroidDefaults(st); err != nil {
		t.Fatal(err)
	}
	if v, ok, _ := st.GetSetting(settingActivePreset); !ok || v != `"lav-ru-linux"` {
		t.Fatalf("набор = %q, задан=%v", v, ok)
	}
	// Повторный запуск ничего не меняет.
	if err := applyAndroidDefaults(st); err != nil {
		t.Fatal(err)
	}
	if v, _, _ := st.GetSetting(settingActivePreset); v != `"lav-ru-linux"` {
		t.Fatalf("после второго запуска набор = %q", v)
	}
}

// Явный выбор, LAV Preset RU тоже, остаётся.
func TestDefaultPresetKeepsExplicitChoice(t *testing.T) {
	for _, chosen := range []string{`"lav-ru"`, `"global"`, `"custom:mine"`} {
		st := openStore(t)
		if err := st.SetSetting(settingActivePreset, chosen); err != nil {
			t.Fatal(err)
		}
		if err := applyAndroidDefaults(st); err != nil {
			t.Fatal(err)
		}
		if v, _, _ := st.GetSetting(settingActivePreset); v != chosen {
			t.Fatalf("выбор %s заменён на %s", chosen, v)
		}
	}
}

// Выбор DNS для LAV Preset RU переходит к новому набору; свой выбор нового набора не перетирается.
func TestDefaultPresetCarriesDNSChoice(t *testing.T) {
	st := openStore(t)
	if err := st.SetMeta(metaDNSOverride, `{"lav-ru":{"remote":"https://1.1.1.1/dns-query"},"lav-cn":{"remote":"x"}}`); err != nil {
		t.Fatal(err)
	}
	if err := applyAndroidDefaults(st); err != nil {
		t.Fatal(err)
	}
	raw, _, _ := st.GetMeta(metaDNSOverride)
	var got map[string]map[string]string
	if err := json.Unmarshal([]byte(raw), &got); err != nil {
		t.Fatal(err)
	}
	if got["lav-ru-linux"]["remote"] != "https://1.1.1.1/dns-query" || got["lav-ru"]["remote"] != "https://1.1.1.1/dns-query" || got["lav-cn"]["remote"] != "x" {
		t.Fatalf("выбор DNS: %s", raw)
	}

	st = openStore(t)
	own := `{"lav-ru":{"remote":"a"},"lav-ru-linux":{"remote":"b"}}`
	if err := st.SetMeta(metaDNSOverride, own); err != nil {
		t.Fatal(err)
	}
	if err := applyAndroidDefaults(st); err != nil {
		t.Fatal(err)
	}
	if raw, _, _ := st.GetMeta(metaDNSOverride); raw != own {
		t.Fatalf("свой выбор нового набора перетёрт: %s", raw)
	}

	st = openStore(t)
	if err := st.SetMeta(metaDNSOverride, `not json`); err != nil {
		t.Fatal(err)
	}
	if err := applyAndroidDefaults(st); err != nil {
		t.Fatalf("нечитаемая запись DNS сорвала запуск: %v", err)
	}
}

func openStore(t *testing.T) *store.Store {
	t.Helper()
	st, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	return st
}
