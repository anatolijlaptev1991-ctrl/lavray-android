// SPDX-License-Identifier: GPL-3.0-only

package mobile

import "testing"

const tunV4Only = `{"tag":"tun-in","type":"tun","address":["172.19.0.1/30"]}`
const tunDual = `{"tag":"tun-in","type":"tun","address":["172.19.0.1/30","fd4c:4156:5259::1/126"]}`

func TestPatchIPv6Off(t *testing.T) {
	tests := []struct {
		name         string
		cfg          string
		wantStrategy any // dns.strategy после правки
		wantResolver any // route.default_domain_resolver.strategy
		wantFlag     bool
	}{
		{"без IPv6: ipv4_only", `{"inbounds":[` + tunV4Only + `],"dns":{"servers":[]},"route":{"default_domain_resolver":{"server":"dns-direct"}}}`,
			"ipv4_only", "prefer_ipv4", true},
		{"IPv6 включён: не трогаем", `{"inbounds":[` + tunDual + `],"dns":{"servers":[]},"route":{"default_domain_resolver":{"server":"dns-direct"}}}`,
			nil, nil, false},
		{"стратегия резолвера пресета остаётся", `{"inbounds":[` + tunV4Only + `],"dns":{},"route":{"default_domain_resolver":{"server":"dns-direct","strategy":"prefer_ipv6"}}}`,
			"ipv4_only", "prefer_ipv6", true},
		{"резолвер строкой", `{"inbounds":[` + tunV4Only + `],"dns":{},"route":{"default_domain_resolver":"dns-direct"}}`,
			"ipv4_only", "prefer_ipv4", true},
		{"нет раздела dns создаётся", `{"inbounds":[` + tunV4Only + `],"route":{"default_domain_resolver":{"server":"dns-direct"}}}`,
			"ipv4_only", "prefer_ipv4", true},
		{"адрес строкой с v6", `{"inbounds":[{"tag":"tun-in","type":"tun","address":"fd00::1/64"}],"dns":{},"route":{"default_domain_resolver":"x"}}`,
			nil, nil, false},
		// L3: чужой выбор не перезаписывается.
		{"заданная dns.strategy остаётся", `{"inbounds":[` + tunV4Only + `],"dns":{"strategy":"prefer_ipv6"},"route":{"default_domain_resolver":{"server":"dns-direct"}}}`,
			"prefer_ipv6", nil, false},
		// L3: без default_domain_resolver sing-box сам выбирает резолвер доменов серверов, и ipv4_only
		// сделал бы сервер только с IPv6 недоступным.
		{"нет default_domain_resolver: не ставим", `{"inbounds":[` + tunV4Only + `],"dns":{},"route":{}}`,
			nil, nil, false},
		{"нет route: не ставим", `{"inbounds":[` + tunV4Only + `],"dns":{}}`,
			nil, nil, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			out, rep, err := patchConfig([]byte(tc.cfg), tunParams{})
			if err != nil {
				t.Fatal(err)
			}
			root := mustParse(t, out)
			dns, _ := root["dns"].(map[string]any)
			var got any
			if dns != nil {
				got = dns["strategy"]
			}
			if got != tc.wantStrategy {
				t.Errorf("dns.strategy = %v, ждали %v", got, tc.wantStrategy)
			}
			var res any
			if route, ok := root["route"].(map[string]any); ok {
				if r, ok := route["default_domain_resolver"].(map[string]any); ok {
					res = r["strategy"]
				}
			}
			if res != tc.wantResolver {
				t.Errorf("resolver.strategy = %v, ждали %v", res, tc.wantResolver)
			}
			if rep.IPv4OnlyDNS != tc.wantFlag {
				t.Errorf("rep.IPv4OnlyDNS = %v", rep.IPv4OnlyDNS)
			}
		})
	}
}

func TestPatchIPv6OffReportsWhyItDidNothing(t *testing.T) {
	_, rep, err := patchConfig([]byte(`{"inbounds":[`+tunV4Only+`],"dns":{"strategy":"prefer_ipv6"},"route":{"default_domain_resolver":"x"}}`), tunParams{})
	if err != nil || rep.IPv4OnlyDNSKept != "prefer_ipv6" {
		t.Errorf("Kept = %q, err = %v", rep.IPv4OnlyDNSKept, err)
	}
	_, rep, err = patchConfig([]byte(`{"inbounds":[`+tunV4Only+`],"dns":{},"route":{}}`), tunParams{})
	if err != nil || !rep.IPv4OnlyDNSSkipped {
		t.Errorf("Skipped = %v, err = %v", rep.IPv4OnlyDNSSkipped, err)
	}
}

// L3: у sing-box пустая стратегия резолвера наследует dns.strategy, поэтому каждый резолвер доменов
// серверов (outbound, endpoint, DNS-сервер), у которого своей стратегии нет, получает prefer_ipv4.
func TestPatchIPv6OffProtectsEveryServerResolver(t *testing.T) {
	cfg := `{
	 "inbounds":[` + tunV4Only + `],
	 "dns":{"servers":[
	   {"tag":"dns-doh","type":"https","server":"dns.example","domain_resolver":"dns-local"},
	   {"tag":"dns-own","type":"https","server":"x.example","domain_resolver":{"server":"dns-local","strategy":"prefer_ipv6"}},
	   {"tag":"dns-plain","type":"udp","server":"1.1.1.1"}]},
	 "route":{"default_domain_resolver":{"server":"dns-direct"}},
	 "outbounds":[
	   {"tag":"a","type":"vless","server":"v6only.example","domain_resolver":"dns-local"},
	   {"tag":"b","type":"vless","server":"b.example","domain_resolver":{"server":"dns-local"}},
	   {"tag":"c","type":"vless","server":"c.example"},
	   {"tag":"d","type":"direct"}],
	 "endpoints":[{"tag":"wg","type":"wireguard","domain_resolver":"dns-local"}]}`
	out, _, err := patchConfig([]byte(cfg), tunParams{})
	if err != nil {
		t.Fatal(err)
	}
	root := mustParse(t, out)
	checks := []struct {
		path []any
		want any
	}{
		{[]any{"dns", "servers", 0, "domain_resolver", "strategy"}, "prefer_ipv4"},
		{[]any{"dns", "servers", 0, "domain_resolver", "server"}, "dns-local"},
		{[]any{"dns", "servers", 1, "domain_resolver", "strategy"}, "prefer_ipv6"}, // чужая стратегия остаётся
		{[]any{"outbounds", 0, "domain_resolver", "strategy"}, "prefer_ipv4"},
		{[]any{"outbounds", 1, "domain_resolver", "strategy"}, "prefer_ipv4"},
		{[]any{"endpoints", 0, "domain_resolver", "strategy"}, "prefer_ipv4"},
		{[]any{"route", "default_domain_resolver", "strategy"}, "prefer_ipv4"},
	}
	for _, c := range checks {
		if got := get(t, root, c.path...); got != c.want {
			t.Errorf("%v = %v, ждали %v", c.path, got, c.want)
		}
	}
	// Элементы без своего domain_resolver не получают его: их резолвер — default_domain_resolver.
	if _, has := get(t, root, "outbounds", 2).(map[string]any)["domain_resolver"]; has {
		t.Error("у outbound c появился domain_resolver")
	}
	if _, has := get(t, root, "dns", "servers", 2).(map[string]any)["domain_resolver"]; has {
		t.Error("у DNS-сервера dns-plain появился domain_resolver")
	}
}
