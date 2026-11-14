// SPDX-License-Identifier: GPL-3.0-only

package mobile

// patch_ipv6.go — DNS без IPv6, когда в туннеле нет адреса IPv6.
//
// VpnService.Builder блокирует семейство адресов, для которого не добавлен ни адрес,
// ни маршрут (allowFamily не вызывается), поэтому IPv6 приложений в туннеле заблокирован.
// Если при этом DNS туннеля отвечает записями AAAA, приложения сначала пробуют IPv6 и
// падают на запасной путь с задержкой. Стратегия dns.strategy=ipv4_only убирает AAAA из
// ответов туннельного DNS.
//
// Как sing-box (v1.14, dns/router.go) применяет стратегию: явная стратегия запроса
// (domain_resolver.strategy, strategy в правиле DNS) сильнее dns.strategy, а пустая
// берёт dns.strategy. Поэтому dns.strategy=ipv4_only достаёт и домены серверов профиля:
// и default_domain_resolver, и domain_resolver у outbound/endpoint, и у DNS-серверов, если у них
// нет своей стратегии (строка "dns-local" — это resolver без стратегии). Домены серверов резолвит
// само приложение вне туннеля, где IPv6 может работать, так что серверу только с IPv6 нужна
// стратегия prefer_ipv4: она проставляется каждому резолверу без своей.
//
// Условия, при которых правка не делается (IPv6 остаётся «как есть», блокируется туннелем, а запасной
// путь приложений быстрый):
//   - в туннеле есть адрес IPv6;
//   - dns.strategy уже задана (пресетом или пользователем): чужой выбор не перезаписывается;
//   - нет route.default_domain_resolver: резолвер доменов серверов в этом случае выбирается
//     самим sing-box без нашей стратегии, и ipv4_only сделал бы сервер только с IPv6 недоступным.

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

// hasDefaultResolver — задан ли route.default_domain_resolver (строкой или объектом).
func hasDefaultResolver(root map[string]any) bool {
	route, ok := root["route"].(map[string]any)
	if !ok {
		return false
	}
	switch r := route["default_domain_resolver"].(type) {
	case string:
		return r != ""
	case map[string]any:
		return true
	}
	return false
}

// patchIPv6Off ставит IPv4-стратегию DNS, если IPv6 в туннеле выключен и это безопасно.
func patchIPv6Off(root map[string]any, rep *patchReport) {
	if tunHasIPv6(root) {
		return
	}
	dns, hasDNS := root["dns"].(map[string]any)
	if hasDNS {
		if s, _ := dns["strategy"].(string); s != "" {
			rep.IPv4OnlyDNSKept = s
			return
		}
	}
	if !hasDefaultResolver(root) {
		rep.IPv4OnlyDNSSkipped = true
		return
	}
	if !hasDNS {
		dns = map[string]any{}
		root["dns"] = dns
	}
	dns["strategy"] = strategyIPv4Only
	rep.IPv4OnlyDNS = true

	route, _ := root["route"].(map[string]any)
	route["default_domain_resolver"] = withStrategy(route["default_domain_resolver"])
	for _, key := range []string{"outbounds", "endpoints"} {
		resolversOf(root[key])
	}
	resolversOf(dns["servers"])
}

// resolversOf проставляет prefer_ipv4 domain_resolver'ам списка (outbounds, endpoints, DNS-серверов), у которых
// нет своей стратегии. Элементы без domain_resolver не трогаются: их резолвер — default_domain_resolver.
func resolversOf(list any) {
	items, _ := list.([]any)
	for _, item := range items {
		m, ok := item.(map[string]any)
		if !ok {
			continue
		}
		if r, has := m["domain_resolver"]; has {
			m["domain_resolver"] = withStrategy(r)
		}
	}
}

// withStrategy возвращает resolver с prefer_ipv4, если своей стратегии у него нет.
func withStrategy(resolver any) any {
	switch r := resolver.(type) {
	case string:
		if r == "" {
			return resolver
		}
		return map[string]any{"server": r, "strategy": strategyPreferIPv4}
	case map[string]any:
		if s, _ := r["strategy"].(string); s == "" {
			r["strategy"] = strategyPreferIPv4
		}
		return r
	}
	return resolver
}
