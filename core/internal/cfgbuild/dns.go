// SPDX-License-Identifier: GPL-3.0-only

// dns.go — DNS-секция из пресета (порт makeDnsServer + DNS-часть
// BuildConfigSingBox, ConfigBuilder.cpp:533–658): 3 режима —
//   - simple: строки remote/direct (+underlying) → типизированные серверы
//     sing-box 1.12+/1.14 (FR-UI-155), remote через туннель detour=proxy;
//   - advanced: полный JSON dns из пресета «как есть» с валидацией (LNB-08);
//   - dns_routing: правила remote/direct-списков (FR-UI-150), включая
//     full:-адреса серверов цепочки (res.ServerDomains).
//
// Built-in правила (query_type 32/33 SVCB/HTTPS RR и .lan — predefined
// NOERROR) и dns-fake (только TUN) — как в оракуле.
//
// Отклонения от оракула: remote_query_strategy наследуется (FR-UI-148);
// direct_dns_strategy/dns_final_out/domain_strategy — DROP (не переносим).
package cfgbuild

import (
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/anatolijlaptev1991-ctrl/lavray-android/core/internal/presets"
)

// buildDNS — dns{} конфига. Порядок сайд-эффектов: DNS-правила собирают
// rule-set-определения (geosite:) ДО маршрутных правил — как в оракуле.
func buildDNS(preset *presets.Preset, st *BuildSettings, res *BuildResult, forTest bool, rs *ruleSetCollector) (map[string]any, error) {
	// mode=advanced (use_dns_object некора, FR-UI-149): объект «как есть».
	if preset.DNS.Mode == "advanced" {
		return advancedDNS(preset)
	}

	// DNS-списки (FR-UI-150): при dns_routing proxy-список → dns-remote,
	// direct-список → dns-direct; адреса серверов цепочки — всегда
	// dns-direct (BuildChainInternal оракула), добавлены ПЕРВЫМИ.
	var dnsRemoteList []string
	dnsDirectList := append([]string(nil), res.ServerDomains...)
	if preset.DNS.RoutingEnabled && !forTest {
		dnsRemoteList = append(dnsRemoteList, preset.Routing.ProxyDomain...)
		dnsDirectList = append(dnsDirectList, preset.Routing.DirectDomain...)
	}

	var servers []any
	var rules []any

	// Remote: через туннель (detour=proxy), резолв своего домена — dns-local.
	// Карточка «DoH» (DNSRemoteDirect) — напрямую, без detour.
	if !forTest {
		detour := "proxy"
		if st.DNSRemoteDirect {
			detour = ""
		}
		srv, err := makeDnsServer("dns-remote", preset.DNS.RemoteDNS, detour, true)
		if err != nil {
			return nil, err
		}
		servers = append(servers, srv)
	}

	// Direct: домены outbounds резолвятся через route.default_domain_resolver.
	srv, err := makeDnsServer("dns-direct", preset.DNS.DirectDNS, "", true)
	if err != nil {
		return nil, err
	}
	servers = append(servers, srv)

	// FakeDNS (FR-UI-151): только с TUN.
	fakeDNS := st.Tun.FakeDNS && st.Tun.Enabled && !forTest
	if fakeDNS {
		servers = append(servers, map[string]any{
			"tag":         "dns-fake",
			"type":        "fakeip",
			"inet4_range": "198.18.0.0/15",
			"inet6_range": "fc00::/18",
		})
	}

	// Underlying 100% Working DNS (dns-local).
	underlying := strings.TrimSpace(st.UnderlyingDNS)
	if underlying == "" {
		underlying = strings.TrimSpace(preset.DNS.UnderlyingDNS)
	}
	if underlying == "" {
		underlying = "local"
	}
	srv, err = makeDnsServer("dns-local", underlying, "", false)
	if err != nil {
		return nil, err
	}
	servers = append(servers, srv)

	// Правила списков (geosite: внутри → rule_set, общий коллектор с route).
	addRuleDNS := func(list []string, server string) {
		rule := makeMatchRule(list, false, rs)
		if len(rule) == 0 {
			return
		}
		rule["action"] = "route"
		rule["server"] = server
		rules = append(rules, rule)
	}
	addRuleDNS(dnsRemoteList, "dns-remote")
	addRuleDNS(dnsDirectList, "dns-direct")

	// Built-in: legacy rcode://success → predefined NOERROR (query_type
	// 32=SVCB, 33=HTTPS; суффикс .lan).
	if !forTest {
		rules = append(rules,
			map[string]any{
				"query_type": []any{32, 33},
				"action":     "predefined",
				"rcode":      0,
			},
			map[string]any{
				"domain_suffix": []any{".lan"},
				"action":        "predefined",
				"rcode":         0,
			},
		)
	}

	// FakeDNS-правило: запросы из tun-in → dns-fake.
	if fakeDNS {
		rules = append(rules, map[string]any{
			"inbound": "tun-in",
			"action":  "route",
			"server":  "dns-fake",
		})
	}

	dns := map[string]any{
		"servers": servers,
		"rules":   rules,
	}
	if s := preset.DNS.RemoteQueryStrategy; s != "" {
		dns["strategy"] = s
	}
	return dns, nil
}

// advancedDNS — режим advanced (FR-UI-149): JSON dns из пресета как есть;
// валидация LNB-08: объект + наличие сервера "dns-direct" (на него ссылается
// route.default_domain_resolver; его отсутствие ломало бы старт).
func advancedDNS(preset *presets.Preset) (map[string]any, error) {
	raw := strings.TrimSpace(preset.DNS.AdvancedJSON)
	if raw == "" {
		return nil, invalidArgument("preset.dns.advanced_json", "dns.mode=advanced requires advanced_json")
	}
	var dns map[string]any
	if err := json.Unmarshal([]byte(raw), &dns); err != nil {
		return nil, invalidArgument("preset.dns.advanced_json", "invalid JSON: %v", err)
	}
	hasDirect := false
	if servers, ok := dns["servers"].([]any); ok {
		for _, s := range servers {
			if m, ok := s.(map[string]any); ok {
				if tag, _ := m["tag"].(string); tag == "dns-direct" {
					hasDirect = true
					break
				}
			}
		}
	}
	if !hasDirect {
		return nil, invalidArgument("preset.dns.advanced_json",
			"advanced dns object must define a server tagged \"dns-direct\" (route.default_domain_resolver, LNB-08)")
	}
	return dns, nil
}

// ValidateDNSAddress проверяет адрес DNS-сервера, введённый пользователем:
// "local" либо [схема://]хост[:порт][/путь] со схемой udp, tcp, tls, https,
// h3, quic или dhcp и непустым хостом.
func ValidateDNSAddress(addr string) error {
	addr = strings.TrimSpace(addr)
	if addr == "" {
		return errors.New("адрес пуст")
	}
	if addr == "local" {
		return nil
	}
	if i := strings.Index(addr, "://"); i >= 0 {
		switch strings.ToLower(addr[:i]) {
		case "udp", "tcp", "tls", "https", "h3", "quic", "dhcp":
		default:
			return fmt.Errorf("схема %q не поддерживается (udp, tcp, tls, https, h3, quic, dhcp)", addr[:i])
		}
	}
	if strings.ContainsAny(addr, " 	") {
		return errors.New("адрес не должен содержать пробелов")
	}
	if _, err := makeDnsServer("dns-remote", addr, "", false); err != nil {
		return errors.New("в адресе нет хоста")
	}
	return nil
}

// makeDnsServer — legacy-строка адреса → типизированный сервер sing-box
// 1.12+/1.14 (порт makeDnsServer, ConfigBuilder.cpp:540–598; FR-UI-155).
// Формы: "local", "fakeip", "[udp://|tls://|https://|h3://|quic://|tcp://|
// dhcp://]хост[:порт][/path]" (для https/h3 путь сохраняется).
func makeDnsServer(tag, address, detour string, useResolver bool) (map[string]any, error) {
	address = strings.TrimSpace(address)
	if address == "" {
		address = "local"
	}
	o := map[string]any{"tag": tag}
	if address == "local" {
		o["type"] = "local"
		if detour != "" {
			o["detour"] = detour
		}
		return o, nil
	}
	if address == "fakeip" {
		o["type"] = "fakeip"
		o["inet4_range"] = "198.18.0.0/15"
		o["inet6_range"] = "fc00::/18"
		return o, nil
	}

	typ := "udp"
	server := address
	if schemeEnd := strings.Index(server, "://"); schemeEnd > 0 {
		scheme := strings.ToLower(server[:schemeEnd])
		server = server[schemeEnd+3:]
		switch scheme {
		case "tls":
			typ = "tls"
		case "https":
			typ = "https"
		case "h3":
			typ = "h3"
		case "quic":
			typ = "quic"
		case "tcp":
			typ = "tcp"
		case "dhcp":
			typ = "dhcp"
		}
	}
	if slash := strings.Index(server, "/"); slash >= 0 {
		if typ == "https" || typ == "h3" {
			o["path"] = server[slash:]
		}
		server = server[:slash]
	}
	serverPort := 0
	if strings.HasPrefix(server, "[") { // [ipv6]:port
		if close := strings.Index(server, "]"); close >= 0 {
			rest := server[close+1:]
			if strings.HasPrefix(rest, ":") {
				if v, err := strconv.Atoi(rest[1:]); err == nil {
					serverPort = v
				}
			}
			server = server[1:close]
		}
	} else if colon := strings.LastIndex(server, ":"); colon > 0 {
		if v, err := strconv.Atoi(server[colon+1:]); err == nil && v > 0 {
			serverPort = v
			server = server[:colon]
		}
	}
	if server == "" {
		return nil, invalidArgument("preset.dns", "dns server address %q: empty host", address)
	}
	o["type"] = typ
	o["server"] = server
	if serverPort > 0 {
		o["server_port"] = serverPort
	}
	// detour на пустой/прямой outbound — ошибка sing-box 1.14; не эмитим
	// (как в оракуле).
	if detour != "" && detour != "direct" {
		o["detour"] = detour
	}
	if useResolver {
		o["domain_resolver"] = "dns-local"
	}
	return o, nil
}
