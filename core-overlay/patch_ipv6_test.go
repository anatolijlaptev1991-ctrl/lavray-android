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
		{"стратегия пресета заменяется", `{"inbounds":[` + tunV4Only + `],"dns":{"strategy":"prefer_ipv6"},"route":{}}`,
			"ipv4_only", nil, true},
		{"стратегия резолвера пресета остаётся", `{"inbounds":[` + tunV4Only + `],"dns":{},"route":{"default_domain_resolver":{"server":"dns-direct","strategy":"prefer_ipv6"}}}`,
			"ipv4_only", "prefer_ipv6", true},
		{"резолвер строкой", `{"inbounds":[` + tunV4Only + `],"dns":{},"route":{"default_domain_resolver":"dns-direct"}}`,
			"ipv4_only", "prefer_ipv4", true},
		{"нет раздела dns создаётся", `{"inbounds":[` + tunV4Only + `]}`,
			"ipv4_only", nil, true},
		{"адрес строкой с v6", `{"inbounds":[{"tag":"tun-in","type":"tun","address":"fd00::1/64"}],"dns":{}}`,
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
