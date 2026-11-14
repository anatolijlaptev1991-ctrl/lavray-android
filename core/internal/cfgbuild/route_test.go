// SPDX-License-Identifier: GPL-3.0-only

package cfgbuild

// route_test.go — юниты route-слоя (W5): makeMatchRule (префиксы FR-UI-135,
// дедупликация гео-ссылок, toLower), ruleSetCollector (порядок/пути поставки),
// splitLinesSkipSharp. Порядок правил/интеграционные случаи — golden G20–G35.

import (
	"reflect"
	"testing"
)

func TestMakeMatchRuleDomains(t *testing.T) {
	rs := newRuleSetCollector(DefaultSettings())
	rule := makeMatchRule([]string{
		"geosite:google",
		"full:Example.COM",
		"domain:Example.org",
		"keyword:Shop",
		"regexp:^ads-[0-9]+",
		"plain.example",
		"", // пустые — пропускаются
	}, false, rs)
	want := map[string]any{
		"rule_set":       []any{"geosite-google"},
		"domain":         []any{"example.com"},
		"domain_suffix":  []any{"example.org", "plain.example"},
		"domain_keyword": []any{"shop"},
		"domain_regex":   []any{"^ads-[0-9]+"},
	}
	if !reflect.DeepEqual(want, rule) {
		t.Fatalf("makeMatchRule(domains) =\n%v\nwant\n%v", rule, want)
	}
}

func TestMakeMatchRuleIPs(t *testing.T) {
	rs := newRuleSetCollector(DefaultSettings())
	rule := makeMatchRule([]string{
		"geoip:ru",
		"geoip:ru", // дедуп
		"geoip:private",
		"10.0.0.0/8",
	}, true, rs)
	// geoip:private — встроенное ip_is_private (файла geoip-private.srs
	// в апстриме нет; попытка открыть его валила старт sing-box).
	want := map[string]any{
		"rule_set":      []any{"geoip-ru"},
		"ip_cidr":       []any{"10.0.0.0/8"},
		"ip_is_private": true,
	}
	if !reflect.DeepEqual(want, rule) {
		t.Fatalf("makeMatchRule(ips) =\n%v\nwant\n%v", rule, want)
	}
	// Пустой список — пустое правило (не эмитится вызывающим кодом).
	if rule := makeMatchRule(nil, false, rs); len(rule) != 0 {
		t.Fatalf("makeMatchRule(nil) = %v, want пусто", rule)
	}
	// Только гео-ссылки в ip-списке — ip_cidr не эмитится.
	rule = makeMatchRule([]string{"geoip:cn"}, true, rs)
	if _, ok := rule["ip_cidr"]; ok {
		t.Fatalf("ip_cidr не должен эмититься для чисто гео-списка: %v", rule)
	}
}

func TestRuleSetCollector(t *testing.T) {
	// Дефолтный каталог поставки — "geo"; layout <geo>/<kind>/<name>.srs.
	rs := newRuleSetCollector(DefaultSettings())
	rs.add("geosite", "google")
	rs.add("geosite", "google") // дедуп по тегу
	rs.add("geoip", "ru")
	got := rs.list()
	want := []any{
		map[string]any{"type": "local", "tag": "geosite-google", "format": "binary",
			"path": "geo/geosite/google.srs"},
		map[string]any{"type": "local", "tag": "geoip-ru", "format": "binary",
			"path": "geo/geoip/ru.srs"},
	}
	if !reflect.DeepEqual(want, got) {
		t.Fatalf("ruleSetCollector.list() =\n%v\nwant\n%v", got, want)
	}

	// Явный GeoDir из настроек (W6 подставит абсолютный путь).
	st := DefaultSettings()
	st.GeoDir = `/opt/lavray/geo`
	rs = newRuleSetCollector(st)
	if tag := rs.add("geosite", ""); tag != "" {
		t.Fatalf("пустое имя гео-набора: %q", tag)
	}
	if defs := rs.list(); len(defs) != 0 {
		t.Fatalf("пустое имя не должно давать определение: %v", defs)
	}
	rs.add("geoip", "private")
	for _, d := range rs.list() {
		if d.(map[string]any)["path"] != "/opt/lavray/geo/geoip/private.srs" {
			t.Fatalf("path = %v", d)
		}
	}
}

func TestSplitLinesSkipSharp(t *testing.T) {
	got := splitLinesSkipSharp("a.exe\n\r\n# comment\n  # тоже комментарий\nb.exe\r\nc.exe")
	want := []any{"a.exe", "b.exe", "c.exe"}
	if !reflect.DeepEqual(want, got) {
		t.Fatalf("splitLinesSkipSharp = %v, want %v", got, want)
	}
	if got := splitLinesSkipSharp("  \n#\n"); got != nil {
		t.Fatalf("только комментарии/пустые строки: %v", got)
	}
}

func TestParseCustomRules(t *testing.T) {
	rules, err := parseCustomRules(`{"rules":[{"port":53,"outbound":"block"}]}`, "k")
	if err != nil || len(rules) != 1 {
		t.Fatalf("parseCustomRules: %v %v", rules, err)
	}
	if rules, err = parseCustomRules("", "k"); err != nil || rules != nil {
		t.Fatalf("пустая строка: %v %v", rules, err)
	}
	if rules, err = parseCustomRules("{}", "k"); err != nil || rules != nil {
		t.Fatalf("{}: %v %v", rules, err)
	}
	if _, err = parseCustomRules("[1,2]", "k"); err == nil {
		t.Fatal("невалидный формат должен давать ошибку")
	}
}

// T-80: пользовательские наборы хранят custom-правила массивом (как в proto);
// сборка конфига читает оба вида записи одинаково.
func TestParseCustomRulesArrayForm(t *testing.T) {
	arr, err := parseCustomRules(`[{"port":53,"outbound":"block"}]`, "k")
	if err != nil || len(arr) != 1 {
		t.Fatalf("массив: %v %v", arr, err)
	}
	obj, _ := parseCustomRules(`{"rules":[{"port":53,"outbound":"block"}]}`, "k")
	if len(obj) != 1 || obj[0].(map[string]any)["port"] != arr[0].(map[string]any)["port"] {
		t.Fatalf("виды записи дают разные правила: %v / %v", arr, obj)
	}
}
