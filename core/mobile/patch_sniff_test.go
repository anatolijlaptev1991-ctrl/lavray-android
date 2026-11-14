// SPDX-License-Identifier: GPL-3.0-only

package mobile

import "testing"

func TestPatchSniffTimeout(t *testing.T) {
	tests := []struct {
		name string
		rule string // правило route.rules[0]
		want any    // ожидаемое значение timeout; nil — ключа нет
	}{
		{"3s снижается", `{"action":"sniff","inbound":["tun-in"],"timeout":"3s"}`, "500ms"},
		{"1m снижается", `{"action":"sniff","timeout":"1m"}`, "500ms"},
		{"501ms снижается", `{"action":"sniff","timeout":"501ms"}`, "500ms"},
		{"ровно 500ms без изменений", `{"action":"sniff","timeout":"500ms"}`, "500ms"},
		{"300ms не удлиняется", `{"action":"sniff","timeout":"300ms"}`, "300ms"},
		{"без тайм-аута остаётся без него", `{"action":"sniff","inbound":["tun-in"]}`, nil},
		{"непонятное значение не трогается", `{"action":"sniff","timeout":"3 секунды"}`, "3 секунды"},
		{"не строка не трогается", `{"action":"sniff","timeout":3}`, float64(3)},
		{"другое действие не трогается", `{"action":"route","outbound":"proxy","timeout":"3s"}`, "3s"},
		{"правило без действия не трогается", `{"domain":["a.example"],"timeout":"3s"}`, "3s"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			in := `{"inbounds":[` + tunIn + `],"route":{"rules":[` + tc.rule + `]}}`
			out, _, err := patchConfig([]byte(in), tunParams{})
			if err != nil {
				t.Fatalf("patchConfig: %v", err)
			}
			rule, _ := get(t, mustParse(t, out), "route", "rules", 0).(map[string]any)
			got, has := rule["timeout"]
			if tc.want == nil {
				if has {
					t.Fatalf("timeout появился: %v", got)
				}
				return
			}
			if f, isNum := tc.want.(float64); isNum {
				if n, _ := got.(interface{ Float64() (float64, error) }); n != nil {
					if v, _ := n.Float64(); v != f {
						t.Fatalf("timeout = %v, ожидалось %v", got, tc.want)
					}
					return
				}
			}
			if got != tc.want {
				t.Fatalf("timeout = %v, ожидалось %v", got, tc.want)
			}
		})
	}
}

func TestPatchSniffOnlyTouchesSniffRules(t *testing.T) {
	in := `{"inbounds":[` + tunIn + `],"route":{"rules":[
		{"action":"sniff","inbound":["tun-in"],"timeout":"3s"},
		{"protocol":"dns","action":"hijack-dns"},
		{"action":"sniff","timeout":"10s","sniffer":["tls"]},
		{"domain":["a.example"],"action":"route","outbound":"direct"}]}}`
	out, _, err := patchConfig([]byte(in), tunParams{})
	if err != nil {
		t.Fatalf("patchConfig: %v", err)
	}
	root := mustParse(t, out)
	for i, want := range map[int]string{0: "500ms", 2: "500ms"} {
		if got := get(t, root, "route", "rules", i, "timeout"); got != want {
			t.Errorf("rules[%d].timeout = %v, ожидалось %s", i, got, want)
		}
	}
	if sn := get(t, root, "route", "rules", 2, "sniffer"); sn == nil {
		t.Error("sniffer потерян")
	}
	for _, i := range []int{1, 3} {
		rule, _ := get(t, root, "route", "rules", i).(map[string]any)
		if _, has := rule["timeout"]; has {
			t.Errorf("rules[%d]: появился timeout", i)
		}
	}
}

func TestPatchSniffNoRoute(t *testing.T) {
	// Конфиг без route и route без rules не должен падать.
	for _, in := range []string{
		`{"inbounds":[` + tunIn + `]}`,
		`{"inbounds":[` + tunIn + `],"route":{}}`,
		`{"inbounds":[` + tunIn + `],"route":{"rules":[]}}`,
	} {
		if _, _, err := patchConfig([]byte(in), tunParams{}); err != nil {
			t.Errorf("%s: %v", in, err)
		}
	}
}
