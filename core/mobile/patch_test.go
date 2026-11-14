// SPDX-License-Identifier: GPL-3.0-only

package mobile

import (
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"
)

func mustParse(t *testing.T, data []byte) map[string]any {
	t.Helper()
	m, err := decodeObject(data)
	if err != nil {
		t.Fatalf("разбор результата: %v", err)
	}
	return m
}

// get идёт по пути из ключей и индексов.
func get(t *testing.T, v any, path ...any) any {
	t.Helper()
	for _, p := range path {
		switch k := p.(type) {
		case string:
			m, ok := v.(map[string]any)
			if !ok {
				t.Fatalf("ожидался объект перед %q", k)
			}
			v = m[k]
		case int:
			l, ok := v.([]any)
			if !ok || k >= len(l) {
				t.Fatalf("нет элемента %d", k)
			}
			v = l[k]
		}
	}
	return v
}

const tunIn = `{"tag":"tun-in","type":"tun","interface_name":"lavray-tun","mtu":9000,"stack":"gvisor","auto_route":true}`

func TestPatchInbounds(t *testing.T) {
	tests := []struct {
		name        string
		inbounds    string
		wantTags    []string
		wantRemoved []string
		wantErr     error
	}{
		{"mixed-in удалён", `[{"tag":"mixed-in","type":"mixed","listen":"127.0.0.1","listen_port":2080},` + tunIn + `]`,
			[]string{"tun-in"}, []string{"mixed-in"}, nil},
		{"слушатель на localhost", `[{"tag":"x","type":"socks","listen":"localhost","listen_port":1}, ` + tunIn + `]`,
			[]string{"tun-in"}, []string{"x"}, nil},
		{"слушатель на ::1", `[{"tag":"x","type":"http","listen":"::1","listen_port":1}, ` + tunIn + `]`,
			[]string{"tun-in"}, []string{"x"}, nil},
		{"слушатель без адреса", `[{"tag":"x","type":"http","listen_port":1}, ` + tunIn + `]`,
			[]string{"tun-in"}, []string{"x"}, nil},
		{"не-loopback слушатель остаётся", `[{"tag":"lan","type":"socks","listen":"192.168.1.2","listen_port":1}, ` + tunIn + `]`,
			[]string{"lan", "tun-in"}, nil, nil},
		{"только tun", `[` + tunIn + `]`, []string{"tun-in"}, nil, nil},
		{"нет tun", `[{"tag":"mixed-in","type":"mixed","listen":"127.0.0.1","listen_port":2080}]`, nil, nil, errNoTun},
		{"нет inbounds", `[]`, nil, nil, errNoTun},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			out, rep, err := patchConfig([]byte(`{"inbounds":`+tc.inbounds+`}`), tunParams{})
			if tc.wantErr != nil {
				if !errors.Is(err, tc.wantErr) {
					t.Fatalf("ошибка = %v, ждали %v", err, tc.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			var tags []string
			for _, in := range get(t, mustParse(t, out), "inbounds").([]any) {
				tags = append(tags, in.(map[string]any)["tag"].(string))
			}
			if !reflect.DeepEqual(tags, tc.wantTags) {
				t.Errorf("теги = %v, ждали %v", tags, tc.wantTags)
			}
			if !reflect.DeepEqual(rep.RemovedInbounds, tc.wantRemoved) {
				t.Errorf("удалено = %v, ждали %v", rep.RemovedInbounds, tc.wantRemoved)
			}
		})
	}
}

func TestPatchTun(t *testing.T) {
	tests := []struct {
		name       string
		in         string
		p          tunParams
		wantMTU    string
		wantForced string
	}{
		{"из конфига", tunIn, tunParams{}, "9000", ""},
		{"stack из настроек не действует, mtu действует", tunIn, tunParams{Stack: "system", MTU: 1500}, "1500", ""},
		{"только stack", tunIn, tunParams{Stack: "mixed"}, "9000", ""},
		{"только mtu", tunIn, tunParams{MTU: 1400}, "1400", ""},
		{"system из конфига заменён", strings.Replace(tunIn, `"gvisor"`, `"system"`, 1), tunParams{}, "9000", "system"},
		{"mixed из конфига заменён", strings.Replace(tunIn, `"gvisor"`, `"mixed"`, 1), tunParams{}, "9000", "mixed"},
		{"stack не задан", strings.Replace(tunIn, `"stack":"gvisor",`, ``, 1), tunParams{}, "9000", ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			out, rep, err := patchConfig([]byte(`{"inbounds":[`+tc.in+`]}`), tc.p)
			if err != nil {
				t.Fatal(err)
			}
			tun := get(t, mustParse(t, out), "inbounds", 0).(map[string]any)
			if _, has := tun["interface_name"]; has {
				t.Error("interface_name не удалён")
			}
			if tun["stack"] != "gvisor" {
				t.Errorf("stack = %v, ждали gvisor", tun["stack"])
			}
			if rep.StackForced != tc.wantForced {
				t.Errorf("StackForced = %q, ждали %q", rep.StackForced, tc.wantForced)
			}
			if got := tun["mtu"].(json.Number).String(); got != tc.wantMTU {
				t.Errorf("mtu = %s, ждали %s", got, tc.wantMTU)
			}
			if tun["auto_route"] != true || tun["tag"] != "tun-in" {
				t.Error("лишние изменения tun-inbound")
			}
		})
	}
}

func TestPatchRules(t *testing.T) {
	tests := []struct {
		name         string
		rules        string
		want         string
		wantDropped  int
		wantStripped int
		wantOwner    bool
	}{
		{"только process_name: правило удалено",
			`[{"process_name":["a.exe"],"outbound":"proxy"}]`, `[]`, 1, 1, false},
		{"process_name и домен: условие снято, правило цело",
			`[{"process_name":["a.exe"],"domain":["x.com"],"outbound":"proxy"}]`,
			`[{"domain":["x.com"],"outbound":"proxy"}]`, 0, 1, false},
		{"process_path и process_path_regex",
			`[{"process_path":["C:\\a"],"process_path_regex":["^x"],"outbound":"p"}]`, `[]`, 1, 2, false},
		{"другие правила не тронуты",
			`[{"domain":["a"],"outbound":"p"},{"process_name":["z"],"outbound":"q"},{"ip_cidr":["1.1.1.0/24"],"outbound":"d"}]`,
			`[{"domain":["a"],"outbound":"p"},{"ip_cidr":["1.1.1.0/24"],"outbound":"d"}]`, 1, 1, false},
		{"правило без process_*, но без условий, не удаляется",
			`[{"action":"sniff"}]`, `[{"action":"sniff"}]`, 0, 0, false},
		{"действие без условий после снятия удаляется",
			`[{"process_name":["a"],"action":"sniff"}]`, `[]`, 1, 1, false},
		{"логическое правило: подправило удалено, остаток цел",
			`[{"type":"logical","mode":"or","rules":[{"process_name":["a"]},{"domain":["x"]}],"outbound":"p"}]`,
			`[{"type":"logical","mode":"or","rules":[{"domain":["x"]}],"outbound":"p"}]`, 1, 1, false},
		{"логическое правило целиком из process_name удаляется",
			`[{"type":"logical","mode":"and","rules":[{"process_name":["a"]}],"outbound":"p"}]`, `[]`, 2, 1, false},
		{"package_name требует поиска владельца",
			`[{"package_name":["com.a"],"outbound":"p"}]`, `[{"package_name":["com.a"],"outbound":"p"}]`, 0, 0, true},
		{"user_id требует поиска владельца",
			`[{"user_id":[1000],"outbound":"p"}]`, `[{"user_id":[1000],"outbound":"p"}]`, 0, 0, true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			in := `{"inbounds":[` + tunIn + `],"route":{"rules":` + tc.rules + `}}`
			out, rep, err := patchConfig([]byte(in), tunParams{})
			if err != nil {
				t.Fatal(err)
			}
			got := get(t, mustParse(t, out), "route", "rules")
			want := mustParse(t, []byte(`{"v":`+tc.want+`}`))["v"]
			if !reflect.DeepEqual(got, want) {
				gj, _ := json.Marshal(got)
				t.Errorf("rules = %s, ждали %s", gj, tc.want)
			}
			if rep.DroppedRules != tc.wantDropped || rep.StrippedConds != tc.wantStripped || rep.NeedsOwnerLookup != tc.wantOwner {
				t.Errorf("отчёт = %+v", rep)
			}
		})
	}
}

func TestPatchDNSRulesAndRuleSets(t *testing.T) {
	in := `{"inbounds":[` + tunIn + `],
	 "dns":{"rules":[{"process_name":["a"],"server":"s"},{"domain":["x"],"server":"s"}]},
	 "route":{"rule_set":[{"type":"inline","tag":"i","rules":[{"process_name":["a"],"domain":["d"]}]},{"type":"remote","tag":"r"}]}}`
	out, _, err := patchConfig([]byte(in), tunParams{})
	if err != nil {
		t.Fatal(err)
	}
	m := mustParse(t, out)
	if n := len(get(t, m, "dns", "rules").([]any)); n != 1 {
		t.Errorf("dns.rules: %d правил, ждали 1", n)
	}
	inline := get(t, m, "route", "rule_set", 0, "rules", 0).(map[string]any)
	if _, has := inline["process_name"]; has || inline["domain"] == nil {
		t.Errorf("inline rule_set: %v", inline)
	}
}

func TestPatchRouteAndExperimental(t *testing.T) {
	in := `{"inbounds":[` + tunIn + `],
	 "route":{"auto_detect_interface":true,"default_interface":"Ethernet","default_mark":7,"final":"proxy","find_process":true},
	 "experimental":{"cache_file":{"enabled":true},"clash_api":{"external_controller":"127.0.0.1:9090"},"v2ray_api":{"listen":"127.0.0.1:8080"}}}`
	out, rep, err := patchConfig([]byte(in), tunParams{})
	if err != nil {
		t.Fatal(err)
	}
	m := mustParse(t, out)
	route := get(t, m, "route").(map[string]any)
	for _, key := range []string{"default_interface", "default_mark"} {
		if _, has := route[key]; has {
			t.Errorf("route.%s не удалён", key)
		}
	}
	if route["auto_detect_interface"] != true || route["final"] != "proxy" {
		t.Errorf("route изменён лишнего: %v", route)
	}
	exp := get(t, m, "experimental").(map[string]any)
	if exp["cache_file"] == nil || exp["clash_api"] != nil || exp["v2ray_api"] != nil {
		t.Errorf("experimental = %v", exp)
	}
	if !rep.NeedsOwnerLookup || len(rep.RemovedExperiment) != 2 {
		t.Errorf("отчёт = %+v", rep)
	}
}

func TestPatchKeepsNumbersAndInputUntouched(t *testing.T) {
	in := []byte(`{"inbounds":[` + tunIn + `],"log":{"level":"info"},"outbounds":[{"type":"vless","server_port":443,"big":12345678901234567890}]}`)
	orig := string(in)
	out, _, err := patchConfig(in, tunParams{})
	if err != nil {
		t.Fatal(err)
	}
	if string(in) != orig {
		t.Error("входной срез изменён")
	}
	ob := get(t, mustParse(t, out), "outbounds", 0).(map[string]any)
	if ob["server_port"].(json.Number).String() != "443" || ob["big"].(json.Number).String() != "12345678901234567890" {
		t.Errorf("числа искажены: %v", ob)
	}
}

func TestPatchInvalidInput(t *testing.T) {
	for _, in := range []string{``, `[]`, `null`, `{`, `"x"`} {
		if _, _, err := patchConfig([]byte(in), tunParams{}); err == nil {
			t.Errorf("patchConfig(%q): ждали ошибку", in)
		}
	}
}
