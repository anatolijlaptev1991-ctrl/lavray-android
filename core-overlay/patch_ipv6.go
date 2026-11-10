// SPDX-License-Identifier: GPL-3.0-only

package mobile

// patch_ipv6.go — DNS без IPv6, когда в туннеле нет адреса IPv6.
//
// VpnService.Builder блокирует семейство адресов, для которого не добавлен ни адрес,
// ни маршрут (allowFamily не вызывается), поэтому IPv6 приложений в туннеле заблокирован.
// Если при этом DNS туннеля отвечает записями AAAA, приложения сначала пробуют IPv6 и
// падают на запасной путь с задержкой. Стратегия ipv4_only убирает AAAA из ответов
// туннельного DNS. Домены серверов профиля резолвит само приложение вне туннеля, где
// IPv6 может работать: их резолвер получает prefer_ipv4 (если пресет не задал своё),
// чтобы сервер только с IPv6 остался доступен.

import "strings"

const (
	strategyIPv4Only   = "ipv4_only"
	strategyPreferIPv4 = "prefer_ipv4"
)

// tunHasIPv6 — есть ли у какого-нибудь tun-inbound адрес IPv6.
func tunHasIPv6(root map[string]any) bool {
	list, _ := root["inbounds"].([]any)
	for _, item := range list {
		in, ok := item.(map[string]any)
		if !ok {
			continue
		}
		if typ, _ := in["type"].(string); typ != "tun" {
			continue
		}
		switch addr := in["address"].(type) {
		case string:
			if strings.Contains(addr, ":") {
				return true
			}
		case []any:
			for _, a := range addr {
				if s, _ := a.(string); strings.Contains(s, ":") {
					return true
				}
			}
		}
	}
	return false
}

// patchIPv6Off ставит IPv4-стратегию DNS, если IPv6 в туннеле выключен.
func patchIPv6Off(root map[string]any, rep *patchReport) {
	if tunHasIPv6(root) {
		return
	}
	dns, ok := root["dns"].(map[string]any)
	if !ok {
		dns = map[string]any{}
		root["dns"] = dns
	}
	dns["strategy"] = strategyIPv4Only
	rep.IPv4OnlyDNS = true
	route, ok := root["route"].(map[string]any)
	if !ok {
		return
	}
	switch r := route["default_domain_resolver"].(type) {
	case string:
		route["default_domain_resolver"] = map[string]any{"server": r, "strategy": strategyPreferIPv4}
	case map[string]any:
		if _, has := r["strategy"]; !has {
			r["strategy"] = strategyPreferIPv4
		}
	}
}
