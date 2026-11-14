// SPDX-License-Identifier: GPL-3.0-only

package mobile

// patch_apps.go — списки «Всегда через VPN» и «Всегда напрямую» (docs/ARCHITECTURE.md,
// «Приложения поверх правил»). Списки отдаёт приложение (Platform.AppRoutes) при каждом
// запуске sing-box. Правила встают сразу за sniff и hijack-dns, раньше всех правил пресета
// и настроек: приложение из списка не подчиняется ни спискам доменов и IP, ни блокировке.
//
//   - «через VPN»: соединения в интернет уходят в outbound proxy, DNS-запросы приложения —
//     в dns-remote. Адреса локальной сети (ip_is_private) идут по обычным правилам: сервер
//     их не видит, а соединение туда не уходит к провайдеру.
//   - «напрямую»: все соединения в outbound bypass, DNS — в dns-direct. Обычно такие
//     приложения исключены из туннеля целиком (VpnService) и сюда не попадают: Kotlin
//     передаёт их, только когда включена системная «Блокировать подключения без VPN»,
//     при которой исключённое приложение осталось бы без сети.
//
// Правило по пакету включает поиск владельца соединения (Binder-вызов на соединение), поэтому
// при пустых списках не добавляется ничего и поиск остаётся выключенным.

import (
	"encoding/json"
	"regexp"
	"sort"
)

// appRoutes — списки пакетов. Пакет в обоих списках остаётся только в Proxy: при ошибке
// безопаснее не выпустить трафик мимо туннеля.
type appRoutes struct {
	Proxy  []string `json:"proxy"`
	Direct []string `json:"direct"`
}

// maxAppRoutes — предел имён в одном списке (как AppSelection.MAX_PACKAGES в Kotlin).
const maxAppRoutes = 2000

// Теги из cfgbuild: выход сервера, прямой выход для списков «напрямую» и DNS-серверы.
const (
	tagProxy     = "proxy"
	tagBypass    = "bypass"
	tagDirect    = "direct"
	tagDNSRemote = "dns-remote"
	tagDNSDirect = "dns-direct"
)

var packageName = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_]*(\.[A-Za-z0-9_]+)+$`)

// parseAppRoutes разбирает JSON от приложения. Нечитаемый текст — пустые списки: правило,
// которое нельзя прочитать, не должно ломать подключение.
func parseAppRoutes(raw string) appRoutes {
	var in appRoutes
	if raw == "" || json.Unmarshal([]byte(raw), &in) != nil {
		return appRoutes{}
	}
	proxy := cleanPackages(in.Proxy, nil)
	taken := make(map[string]bool, len(proxy))
	for _, p := range proxy {
		taken[p] = true
	}
	return appRoutes{Proxy: proxy, Direct: cleanPackages(in.Direct, taken)}
}

// cleanPackages оставляет допустимые имена без повторов и без занятых, по алфавиту.
func cleanPackages(list []string, taken map[string]bool) []string {
	seen := make(map[string]bool, len(list))
	out := make([]string, 0, len(list))
	for _, p := range list {
		if seen[p] || taken[p] || !packageName.MatchString(p) {
			continue
		}
		seen[p] = true
		out = append(out, p)
	}
	sort.Strings(out)
	if len(out) > maxAppRoutes {
		out = out[:maxAppRoutes]
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// patchAppRoutes вставляет правила списков в route.rules и dns.rules.
func patchAppRoutes(root map[string]any, a appRoutes, rep *patchReport) {
	if len(a.Proxy) == 0 && len(a.Direct) == 0 {
		return
	}
	var routeRules, dnsRules []any
	if len(a.Proxy) > 0 && hasTag(root, "outbounds", tagProxy) {
		routeRules = append(routeRules, map[string]any{
			"type": "logical",
			"mode": "and",
			"rules": []any{
				map[string]any{"package_name": toAny(a.Proxy)},
				map[string]any{"ip_is_private": true, "invert": true},
			},
			"outbound": tagProxy,
		})
		if hasDNSServer(root, tagDNSRemote) {
			dnsRules = append(dnsRules, packageDNSRule(a.Proxy, tagDNSRemote))
		}
		rep.AppProxy = len(a.Proxy)
	}
	if out := directTag(root); len(a.Direct) > 0 && out != "" {
		routeRules = append(routeRules, map[string]any{"package_name": toAny(a.Direct), "outbound": out})
		if hasDNSServer(root, tagDNSDirect) {
			dnsRules = append(dnsRules, packageDNSRule(a.Direct, tagDNSDirect))
		}
		rep.AppDirect = len(a.Direct)
	}
	if len(routeRules) == 0 {
		return
	}
	rep.NeedsOwnerLookup = true
	route, _ := root["route"].(map[string]any)
	if route == nil {
		route = map[string]any{}
		root["route"] = route
	}
	existing, _ := route["rules"].([]any)
	route["rules"] = insertAt(existing, leadingActions(existing), routeRules)
	if dns, ok := root["dns"].(map[string]any); ok && len(dnsRules) > 0 {
		rules, _ := dns["rules"].([]any)
		dns["rules"] = insertAt(rules, 0, dnsRules)
	}
}

func packageDNSRule(pkgs []string, server string) map[string]any {
	return map[string]any{"package_name": toAny(pkgs), "action": "route", "server": server}
}

// leadingActions — сколько правил в начале списка только готовят соединение (sniff,
// hijack-dns, resolve). Правила списков встают после них: DNS-запрос приложения «через VPN»
// должен по-прежнему перехватываться, а не уходить на сервер сырым UDP.
func leadingActions(rules []any) int {
	n := 0
	for _, item := range rules {
		rule, ok := item.(map[string]any)
		if !ok {
			break
		}
		switch rule["action"] {
		case "sniff", "hijack-dns", "resolve":
			n++
			continue
		}
		break
	}
	return n
}

func insertAt(list []any, at int, items []any) []any {
	out := make([]any, 0, len(list)+len(items))
	out = append(out, list[:at]...)
	out = append(out, items...)
	return append(out, list[at:]...)
}

// directTag — прямой выход: bypass (им cfgbuild ведёт списки «напрямую»), иначе direct.
func directTag(root map[string]any) string {
	for _, tag := range []string{tagBypass, tagDirect} {
		if hasTag(root, "outbounds", tag) {
			return tag
		}
	}
	return ""
}

// hasTag — есть ли в root[section] объект с этим тегом. Выход proxy бывает и в endpoints
// (WireGuard в sing-box 1.11+).
func hasTag(root map[string]any, section, tag string) bool {
	for _, sec := range []string{section, "endpoints"} {
		list, _ := root[sec].([]any)
		for _, item := range list {
			if m, ok := item.(map[string]any); ok && m["tag"] == tag {
				return true
			}
		}
	}
	return false
}

func hasDNSServer(root map[string]any, tag string) bool {
	dns, _ := root["dns"].(map[string]any)
	list, _ := dns["servers"].([]any)
	for _, item := range list {
		if m, ok := item.(map[string]any); ok && m["tag"] == tag {
			return true
		}
	}
	return false
}

func toAny(list []string) []any {
	out := make([]any, len(list))
	for i, s := range list {
		out[i] = s
	}
	return out
}
