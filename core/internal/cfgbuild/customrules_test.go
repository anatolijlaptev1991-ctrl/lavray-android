// SPDX-License-Identifier: GPL-3.0-only

package cfgbuild

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/anatolijlaptev1991-ctrl/lavray-android/core/internal/presets"
)

// buildWithGlobal собирает конфиг при заданной устаревшей настройке
// routing.custom_global и возвращает результат и JSON секции route.
func buildWithGlobal(t *testing.T, global string) (*BuildResult, string) {
	t.Helper()
	st := DefaultSettings()
	st.CustomRouteGlobalJSON = global
	res, err := Build(BuildRequest{Profile: vlessWithStream(nil), Settings: st})
	if err != nil {
		t.Fatalf("Build с routing.custom_global=%.40q: %v", global, err)
	}
	route, err := json.Marshal(res.Config["route"])
	if err != nil {
		t.Fatal(err)
	}
	return res, string(route)
}

// Негодное значение устаревшей настройки не ломает сборку: правила пропущены,
// причина — в BuildResult.Warnings (старые установки продолжают запускаться).
func TestInvalidCustomGlobalIsIgnoredWithWarning(t *testing.T) {
	over := `{"rules":[{"domain":["` + strings.Repeat("a", presets.MaxCustomRulesBytes) + `"]}]}`
	cases := map[string]string{
		"объект без rules":       `{"foo":1}`,
		"не объект в rules":      `{"rules":[1,"x"]}`,
		"rules не массив":        `{"rules":{"a":1}}`,
		"не JSON":                `this is not json`,
		"больше 256 КиБ":         over,
		"массив не-объектов":     `[1,2]`,
		"скаляр":                 `42`,
		"обрезанный JSON-объект": `{"rules":[{"domain":`,
	}
	for name, raw := range cases {
		res, route := buildWithGlobal(t, raw)
		if len(res.Warnings) != 1 || !strings.Contains(res.Warnings[0], "routing.custom_global") {
			t.Errorf("%s: замечания сборки = %v", name, res.Warnings)
		}
		if strings.Contains(route, "aaaa") {
			t.Errorf("%s: правила негодной настройки попали в конфиг", name)
		}
	}
}

// Годное значение применяется без замечаний — в обоих видах записи.
func TestValidCustomGlobalAppliesWithoutWarning(t *testing.T) {
	for _, raw := range []string{
		`{"rules":[{"domain":["global-pin.example"],"outbound":"proxy"}]}`,
		`[{"domain":["global-pin.example"],"outbound":"proxy"}]`,
		"", "{}",
	} {
		res, route := buildWithGlobal(t, raw)
		if len(res.Warnings) != 0 {
			t.Errorf("%q: замечания %v", raw, res.Warnings)
		}
		if has := strings.Contains(route, "global-pin.example"); has != (raw != "" && raw != "{}") {
			t.Errorf("%q: правило в конфиге = %v", raw, has)
		}
	}
}

// Негодные custom-правила НАБОРА по-прежнему останавливают сборку (там
// пользователь видит причину, а молча пропущенный пин был бы утечкой).
func TestInvalidPresetCustomRulesStillFailBuild(t *testing.T) {
	p := NeutralPreset()
	p.Routing.CustomRules = `[1]`
	if _, err := Build(BuildRequest{Profile: vlessWithStream(nil), Preset: p}); err == nil {
		t.Fatal("негодные custom-правила набора приняты")
	}
}
