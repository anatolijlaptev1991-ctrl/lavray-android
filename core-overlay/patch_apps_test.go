// SPDX-License-Identifier: GPL-3.0-only

package mobile

import (
	"encoding/json"
	"reflect"
	"testing"
)

// appsCfg — конфиг в форме cfgbuild (G28): sniff, hijack-dns, правила пресета, DNS со списками.
const appsCfg = `{
 "inbounds":[{"type":"tun","tag":"tun-in","address":["172.19.0.1/30"]}],
 "outbounds":[{"type":"vless","tag":"proxy"},{"type":"direct","tag":"direct"},{"type":"direct","tag":"bypass"},{"type":"block","tag":"block"}],
 "route":{"final":"proxy","rules":[
  {"action":"sniff","inbound":["mixed-in","tun-in"],"timeout":"3s"},
  {"action":"hijack-dns","protocol":"dns"},
  {"outbound":"block","rule_set":["geosite-category-ads-all"]},
  {"domain_suffix":["ru"],"outbound":"bypass"},
  {"process_name":["a.exe"],"outbound":"proxy"}
 ]},
 "dns":{"servers":[{"tag":"dns-remote","type":"https"},{"tag":"dns-direct","type":"local"},{"tag":"dns-local","type":"local"}],
  "rules":[{"action":"route","domain_suffix":["google.com"],"server":"dns-remote"},{"action":"route","domain_suffix":["ru"],"server":"dns-direct"}]}
}`

func patchApps(t *testing.T, cfg string, a appRoutes) (map[string]any, patchReport) {
	t.Helper()
	out, rep, err := patchConfig([]byte(cfg), tunParams{Apps: a})
	if err != nil {
		t.Fatal(err)
	}
	var root map[string]any
	if err := json.Unmarshal(out, &root); err != nil {
		t.Fatal(err)
	}
	return root, rep
}

func rulesOf(root map[string]any, section string) []any {
	m, _ := root[section].(map[string]any)
	list, _ := m["rules"].([]any)
	return list
}

func jsonOf(t *testing.T, v any) string {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestAppRoutesEmptyListsChangeNothing(t *testing.T) {
	root, rep := patchApps(t, appsCfg, appRoutes{})
	if rep.NeedsOwnerLookup || rep.AppProxy != 0 || rep.AppDirect != 0 {
		t.Fatalf("пустые списки включили поиск владельца: %+v", rep)
	}
	// process_name снят и правило удалено, как и без списков.
	if got := len(rulesOf(root, "route")); got != 4 {
		t.Fatalf("правил маршрута %d, ждали 4", got)
	}
	if got := len(rulesOf(root, "dns")); got != 2 {
		t.Fatalf("правил DNS %d, ждали 2", got)
	}
}

func TestAppRoutesGoRightAfterSniffAndHijack(t *testing.T) {
	root, rep := patchApps(t, appsCfg, appRoutes{Proxy: []string{"org.telegram.messenger"}, Direct: []string{"com.bank.app", "com.game.x"}})
	if !rep.NeedsOwnerLookup || rep.AppProxy != 1 || rep.AppDirect != 2 {
		t.Fatalf("отчёт: %+v", rep)
	}
	rules := rulesOf(root, "route")
	if len(rules) != 6 {
		t.Fatalf("правил маршрута %d, ждали 6: %s", len(rules), jsonOf(t, rules))
	}
	if a, _ := rules[0].(map[string]any)["action"]; a != "sniff" {
		t.Fatalf("первым должен остаться sniff: %s", jsonOf(t, rules[0]))
	}
	if a, _ := rules[1].(map[string]any)["action"]; a != "hijack-dns" {
		t.Fatalf("вторым должен остаться hijack-dns: %s", jsonOf(t, rules[1]))
	}
	wantProxy := `{"mode":"and","outbound":"proxy","rules":[{"package_name":["org.telegram.messenger"]},{"invert":true,"ip_is_private":true}],"type":"logical"}`
	if got := jsonOf(t, rules[2]); got != wantProxy {
		t.Fatalf("правило «через VPN»:\n got %s\nwant %s", got, wantProxy)
	}
	wantDirect := `{"outbound":"bypass","package_name":["com.bank.app","com.game.x"]}`
	if got := jsonOf(t, rules[3]); got != wantDirect {
		t.Fatalf("правило «напрямую»:\n got %s\nwant %s", got, wantDirect)
	}
	// Блокировка рекламы пресета идёт после списков: они важнее любых правил пресета.
	if o, _ := rules[4].(map[string]any)["outbound"]; o != "block" {
		t.Fatalf("после списков ждали правило пресета: %s", jsonOf(t, rules[4]))
	}
	dns := rulesOf(root, "dns")
	if len(dns) != 4 {
		t.Fatalf("правил DNS %d, ждали 4: %s", len(dns), jsonOf(t, dns))
	}
	if got := jsonOf(t, dns[0]); got != `{"action":"route","package_name":["org.telegram.messenger"],"server":"dns-remote"}` {
		t.Fatalf("DNS «через VPN»: %s", got)
	}
	if got := jsonOf(t, dns[1]); got != `{"action":"route","package_name":["com.bank.app","com.game.x"],"server":"dns-direct"}` {
		t.Fatalf("DNS «напрямую»: %s", got)
	}
}

// Без sniff (отключён в пресете) правила встают сразу за hijack-dns.
func TestAppRoutesWithoutSniff(t *testing.T) {
	cfg := `{"inbounds":[{"type":"tun"}],"outbounds":[{"tag":"proxy"},{"tag":"bypass","type":"direct"}],
	 "route":{"rules":[{"action":"hijack-dns","protocol":"dns"},{"ip_cidr":["10.0.0.0/8"],"outbound":"bypass"}]}}`
	root, _ := patchApps(t, cfg, appRoutes{Proxy: []string{"a.b"}})
	rules := rulesOf(root, "route")
	if len(rules) != 3 || rules[1].(map[string]any)["outbound"] != "proxy" {
		t.Fatalf("правила: %s", jsonOf(t, rules))
	}
	// DNS-серверов нет: правило маршрута есть, правил DNS не появилось.
	if dns := rulesOf(root, "dns"); len(dns) != 0 {
		t.Fatalf("появились правила DNS: %s", jsonOf(t, dns))
	}
}

// WireGuard-выход в endpoints тоже считается выходом proxy; без прямого выхода правило «напрямую» не ставится.
func TestAppRoutesOutboundLookup(t *testing.T) {
	cfg := `{"inbounds":[{"type":"tun"}],"endpoints":[{"type":"wireguard","tag":"proxy"}],"outbounds":[{"tag":"block","type":"block"}],"route":{"rules":[]}}`
	root, rep := patchApps(t, cfg, appRoutes{Proxy: []string{"a.b"}, Direct: []string{"c.d"}})
	rules := rulesOf(root, "route")
	if len(rules) != 1 || rep.AppProxy != 1 || rep.AppDirect != 0 {
		t.Fatalf("правила %s, отчёт %+v", jsonOf(t, rules), rep)
	}
	// Ни proxy, ни прямого выхода: правил нет и поиск владельца не нужен.
	cfg = `{"inbounds":[{"type":"tun"}],"outbounds":[{"tag":"block","type":"block"}],"route":{"rules":[]}}`
	_, rep = patchApps(t, cfg, appRoutes{Proxy: []string{"a.b"}, Direct: []string{"c.d"}})
	if rep.NeedsOwnerLookup {
		t.Fatalf("правил нет, а поиск владельца включён: %+v", rep)
	}
}

func TestParseAppRoutes(t *testing.T) {
	tests := []struct {
		name string
		raw  string
		want appRoutes
	}{
		{"пусто", ``, appRoutes{}},
		{"мусор", `{"proxy":`, appRoutes{}},
		{"не объект", `[1,2]`, appRoutes{}},
		{"по алфавиту без повторов", `{"proxy":["z.z","a.a","z.z"],"direct":["b.b"]}`,
			appRoutes{Proxy: []string{"a.a", "z.z"}, Direct: []string{"b.b"}}},
		{"пакет в обоих списках остаётся в proxy", `{"proxy":["a.a"],"direct":["a.a","b.b"]}`,
			appRoutes{Proxy: []string{"a.a"}, Direct: []string{"b.b"}}},
		{"недопустимые имена отброшены", `{"proxy":["nodot","1a.b","a.b; rm","ok.app"],"direct":[""]}`,
			appRoutes{Proxy: []string{"ok.app"}}},
		{"неизвестные поля не мешают", `{"proxy":["a.b"],"extra":1}`, appRoutes{Proxy: []string{"a.b"}}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := parseAppRoutes(tc.raw); !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("got %+v, want %+v", got, tc.want)
			}
		})
	}
}

func TestParseAppRoutesBounded(t *testing.T) {
	list := make([]string, maxAppRoutes+10)
	for i := range list {
		list[i] = "p.a" + string(rune('a'+i%26)) + "x" + itoa(i)
	}
	raw, _ := json.Marshal(appRoutes{Proxy: list})
	if got := len(parseAppRoutes(string(raw)).Proxy); got != maxAppRoutes {
		t.Fatalf("имён %d, предел %d", got, maxAppRoutes)
	}
}

func itoa(i int) string {
	b, _ := json.Marshal(i)
	return string(b)
}

// Кэш пакетов за uid: второй вызов не идёт в Kotlin, пустой ответ не запоминается, новый запуск всё забывает.
func TestPackagesForCachesPerRun(t *testing.T) {
	plat := &countingPlatform{answers: map[int32]string{10001: "a.b, c.d", 10002: "", 1073: ""}}
	h := newHost(plat)
	for i := 0; i < 3; i++ {
		if got := h.packagesFor(10001); !reflect.DeepEqual(got, []string{"a.b", "c.d"}) {
			t.Fatalf("пакеты %v", got)
		}
		h.packagesFor(10002)
		h.packagesFor(1073)
	}
	// Системный uid без пакетов спрашивается один раз за запуск.
	if plat.calls[1073] != 1 {
		t.Fatalf("системный uid спрошен %d раз, ждали 1", plat.calls[1073])
	}
	if plat.calls[10001] != 1 {
		t.Fatalf("10001 спрошен %d раз, ждали 1", plat.calls[10001])
	}
	if plat.calls[10002] != 3 {
		t.Fatalf("пустой ответ запомнился: %d вызовов вместо 3", plat.calls[10002])
	}
	h.forgetPackages()
	h.packagesFor(10001)
	if plat.calls[10001] != 2 {
		t.Fatalf("после forgetPackages 10001 спрошен %d раз, ждали 2", plat.calls[10001])
	}
}

type countingPlatform struct {
	fakePlatform
	answers map[int32]string
	calls   map[int32]int
}

func (p *countingPlatform) PackagesForUid(uid int32) string {
	if p.calls == nil {
		p.calls = map[int32]int{}
	}
	p.calls[uid]++
	return p.answers[uid]
}
