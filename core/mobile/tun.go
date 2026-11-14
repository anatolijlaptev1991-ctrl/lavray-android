// SPDX-License-Identifier: GPL-3.0-only

package mobile

// tun.go — параметры TUN для Platform.OpenTun (JSON строкой: итераторы
// gomobile не нужны).

import (
	"encoding/json"
	"fmt"
	"net/netip"

	tun "github.com/sagernet/sing-tun"
)

// tunPayload — то, что Kotlin превращает в VpnService.Builder. Route4 и Route6
// считает sing-box (auto_route с учётом route_address и route_exclude_address);
// Kotlin, не знающий этих полей, добавляет 0.0.0.0/0 и ::/0.
type tunPayload struct {
	MTU             int      `json:"mtu"`
	Inet4           []string `json:"inet4"`
	Inet6           []string `json:"inet6"`
	DNS             []string `json:"dns"`
	IncludePackages []string `json:"includePackages"`
	ExcludePackages []string `json:"excludePackages"`
	Route4          []string `json:"route4"`
	Route6          []string `json:"route6"`
}

func buildTunPayload(o *tun.Options) (string, error) {
	ranges, err := o.BuildAutoRouteRanges(true)
	if err != nil {
		return "", fmt.Errorf("mobile: маршруты TUN: %w", err)
	}
	dns, err := o.DNSServerAddress()
	if err != nil {
		return "", fmt.Errorf("mobile: DNS TUN: %w", err)
	}
	p := tunPayload{
		MTU:             int(o.MTU),
		Inet4:           prefixStrings(o.Inet4Address),
		Inet6:           prefixStrings(o.Inet6Address),
		DNS:             make([]string, 0, len(dns)),
		IncludePackages: nonNil(o.IncludePackage),
		ExcludePackages: nonNil(o.ExcludePackage),
	}
	for _, a := range dns {
		p.DNS = append(p.DNS, a.String())
	}
	for _, r := range ranges {
		if r.Addr().Is4() {
			p.Route4 = append(p.Route4, r.String())
		} else {
			p.Route6 = append(p.Route6, r.String())
		}
	}
	p.Route4, p.Route6 = nonNil(p.Route4), nonNil(p.Route6)
	out, err := json.Marshal(p)
	if err != nil {
		return "", fmt.Errorf("mobile: JSON TUN: %w", err)
	}
	return string(out), nil
}

func prefixStrings(list []netip.Prefix) []string {
	out := make([]string, 0, len(list))
	for _, p := range list {
		out = append(out, p.String())
	}
	return out
}

func nonNil(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}
