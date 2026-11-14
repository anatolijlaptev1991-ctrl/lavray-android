// SPDX-License-Identifier: GPL-3.0-only

package cfgbuild

import (
	"errors"
	"net/netip"
	"reflect"
	"strings"
	"testing"

	"github.com/anatolijlaptev1991-ctrl/lavray-android/core/internal/presets"
)

// T-30: тестер и конфиг берут правила из одного routeRules — для каждого
// встроенного пресета (с TUN и без) таблица тестера равна route.rules конфига.
func TestRouteTableIsTheConfigRules(t *testing.T) {
	list, err := presets.BuiltinPresets()
	if err != nil {
		t.Fatal(err)
	}
	for _, tun := range []bool{false, true} {
		for _, p := range list {
			st := DefaultSettings()
			st.Tun.Enabled = tun
			st.Tun.BypassProcess = "game.exe"
			res, err := Build(BuildRequest{Profile: socksProfile(1, "1.1.1.1", 1080), Settings: st, Preset: p})
			if err != nil {
				t.Fatalf("%s: Build: %v", p.ID, err)
			}
			route := res.Config["route"].(map[string]any)
			table, err := BuildRouteTable(p, st)
			if err != nil {
				t.Fatalf("%s: BuildRouteTable: %v", p.ID, err)
			}
			if !reflect.DeepEqual(route["rules"], table.Rules) {
				t.Errorf("%s (tun=%v): правила тестера расходятся с route.rules конфига", p.ID, tun)
			}
			if route["final"] != table.Final || len(table.Lists) != len(table.Rules) {
				t.Errorf("%s: final=%v/%v, lists=%d rules=%d", p.ID, route["final"], table.Final, len(table.Lists), len(table.Rules))
			}
		}
	}
}

func TestParseRouteQuery(t *testing.T) {
	for _, c := range []struct {
		in      string
		host    string
		ip      string
		port    int
		process string
	}{
		{"Example.ORG.", "example.org", "", 0, ""},
		{"https://store.steampowered.com/app/1?x=1", "store.steampowered.com", "", 443, ""},
		{"http://user:pw@lori.ru:8080/p", "lori.ru", "", 8080, ""},
		{"192.168.1.10", "", "192.168.1.10", 0, ""},
		{"192.168.1.10:53", "", "192.168.1.10", 53, ""},
		{"[2001:db8::1]:443", "", "2001:db8::1", 443, ""},
		{"2001:db8::1", "", "2001:db8::1", 0, ""},
		{"::ffff:10.0.0.1", "", "10.0.0.1", 0, ""},
		{"", "", "", 0, " Cursor.exe "},
	} {
		q := ParseRouteQuery(c.in, c.process)
		gotIP := ""
		if q.IP.IsValid() {
			gotIP = q.IP.String()
		}
		if q.Host != c.host || gotIP != c.ip || q.Port != c.port || q.Network != "tcp" || q.Process != strings.TrimSpace(c.process) {
			t.Errorf("%q: %+v", c.in, q)
		}
	}
}

type fakeSet struct {
	domains []string
	ips     []string
}

func (f fakeSet) MatchDomain(h string) bool {
	for _, d := range f.domains {
		if h == d || strings.HasSuffix(h, "."+d) {
			return true
		}
	}
	return false
}

func (f fakeSet) MatchIP(ip netip.Addr) bool {
	for _, s := range f.ips {
		if netip.MustParsePrefix(s).Contains(ip) {
			return true
		}
	}
	return false
}

func TestEvaluateFollowsSingBoxSemantics(t *testing.T) {
	table := &RouteTable{
		Final: "proxy",
		Rules: []any{
			map[string]any{"action": "sniff"},
			map[string]any{"action": "hijack-dns", "protocol": "dns"},
			map[string]any{"process_name": []any{"Zed.exe"}, "outbound": "bypass"},
			map[string]any{"rule_set": []any{"geosite-ads"}, "outbound": "block"},
			// domain_suffix + rule_set в одном правиле — «ИЛИ» (группа адреса назначения)
			map[string]any{"domain_suffix": []any{"exitgames.com", ".lan"}, "rule_set": []any{"geosite-ru"}, "outbound": "bypass"},
			map[string]any{"ip_is_private": true, "rule_set": []any{"geoip-ru"}, "outbound": "bypass"},
			map[string]any{"domain": []any{"only.example"}, "domain_keyword": []any{"casino"}, "outbound": "block"},
			map[string]any{"network": []any{"udp"}, "port": []any{135, 5353}, "outbound": "block"},
			map[string]any{"source_ip_cidr": []any{"224.0.0.0/3"}, "outbound": "block"},
			map[string]any{"domain_suffix": []any{"broken.example"}, "rule_set": []any{"geosite-missing"}, "clash_mode": "global", "outbound": "block"},
		},
		Lists: []string{"sniff", "hijack_dns", "process_rules[0]", "block_domain", "direct_domain", "direct_ip", "custom_rules[0]", "builtin_block", "builtin_block", "custom_rules[1]"},
	}
	sets := map[string]fakeSet{
		"geosite-ads": {domains: []string{"doubleclick.net"}},
		"geosite-ru":  {domains: []string{"ru"}},
		"geoip-ru":    {ips: []string{"77.88.0.0/16"}},
	}
	look := func(tag string) (RuleSetMatcher, error) {
		if s, ok := sets[tag]; ok {
			return s, nil
		}
		return nil, errors.New("нет файла")
	}
	for _, c := range []struct {
		q       RouteQuery
		out     string
		list    string
		ruleHas string
	}{
		{ParseRouteQuery("lori.ru", ""), "bypass", "direct_domain", "rule_set geosite-ru"},
		{ParseRouteQuery("sub.exitgames.com", ""), "bypass", "direct_domain", "domain_suffix"},
		{ParseRouteQuery("x.lan", ""), "bypass", "direct_domain", "domain_suffix"},
		{ParseRouteQuery("lan", ""), "proxy", "default", ""}, // «.lan» — только поддомены
		{ParseRouteQuery("ads.doubleclick.net", ""), "block", "block_domain", "rule_set geosite-ads"},
		{ParseRouteQuery("192.168.1.10", ""), "bypass", "direct_ip", "ip_is_private"},
		{ParseRouteQuery("77.88.8.8", ""), "bypass", "direct_ip", "rule_set geoip-ru"},
		{ParseRouteQuery("8.8.8.8", ""), "proxy", "default", ""},
		{ParseRouteQuery("only.example", ""), "block", "custom_rules[0]", "domain"},
		{ParseRouteQuery("best-casino.io", ""), "block", "custom_rules[0]", "domain_keyword"},
		{ParseRouteQuery("", "zed.EXE"), "bypass", "process_rules[0]", "process_name"},
		{ParseRouteQuery("", "other.exe"), "proxy", "default", ""},
		// Домен не срабатывает на ip_is_private: без resolve sing-box IP домена не знает.
		{ParseRouteQuery("localhost", ""), "proxy", "default", ""},
	} {
		v := table.Evaluate(c.q, look)
		if v.Outbound != c.out || v.List != c.list || !strings.Contains(v.Rule, c.ruleHas) {
			t.Errorf("%+v → %+v; ожидалось %s/%s/%q", c.q, v, c.out, c.list, c.ruleHas)
		}
	}

	// UDP :5353 — встроенная блокировка; TCP :5353 — нет.
	udp := RouteQuery{Host: "mdns.example", Port: 5353, Network: "udp"}
	if v := table.Evaluate(udp, look); v.Outbound != "block" || v.List != "builtin_block" {
		t.Errorf("udp:5353 → %+v", v)
	}
	tcp := RouteQuery{Host: "mdns.example", Port: 5353, Network: "tcp"}
	if v := table.Evaluate(tcp, look); v.Outbound != "proxy" {
		t.Errorf("tcp:5353 → %+v", v)
	}
	// Неподдерживаемое условие и нечитаемый rule_set не срабатывают молча: они названы.
	v := table.Evaluate(ParseRouteQuery("broken.example", ""), look)
	if v.Outbound != "proxy" || len(v.Unresolved) == 0 {
		t.Errorf("broken.example → %+v; ожидался proxy с перечнем неоценённого", v)
	}
}
