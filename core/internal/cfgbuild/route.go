// SPDX-License-Identifier: GPL-3.0-only

// route.go — route-секция из пресета (порт Routing-части BuildConfigSingBox,
// ConfigBuilder.cpp:660–757) + experimental.cache_file. Порядок правил —
// (обновление bd56088): sniff → dns-hijack → CUSTOM пресета (пины
// имеют приоритет над списками — Riot→proxy выше geosite:riot) → block →
// proxy → direct (domain, затем ip) → процессные правила (SCHEMA §2.1) → tun-bypass
// (cidr/process, whitelist-инверсия ТОЛЬКО tun-списков — поправка ревью №28)
// → built-in блокировки → custom (пресет custom_rules + custom_route_global).
//
// Rule-set (FR-UI-143/266, LNB-31/36): geosite:/geoip: → ЛОКАЛЬНЫЕ .srs из
// подписанного набора поставки (type=local), не remote с изменяемой ветки
// оракула; experimental.cache_file включён всегда — старт без сети работает
// из кэша/поставки.
//
// Отклонения от оракула (deviations.yaml):
//   - rule-set local из поставки вместо remote MetaCubeX (THREAT-MODEL:
//     подмена .srs на зеркале; layouts — geo/<kind>/<name>.srs);
//   - custom-правила в конце (план §3.1.5), оракул ставил их до сгенерированных;
//   - tun-bypass до built-in (план §3.1.5), у оракула после;
//   - авто-байпас путей внешних процессов — W6 (за флагом enable_external_cores).
package cfgbuild

import (
	"fmt"
	"strings"

	"github.com/anatolijlaptev1991-ctrl/lavray-android/core/internal/presets"
)

// ruleSetCollector — дедуплицированный сбор определений route.rule_set из
// гео-ссылок (geosite:/geoip:) dns- и route-правил; порядок определений —
// порядок встречи (dns-правила собираются раньше маршрутных, см. builder.go).
type ruleSetCollector struct {
	geoDir string
	order  []string
	defs   map[string]map[string]any
}

// newRuleSetCollector — каталог поставки .srs (GeoDir; "" → "geo",
// W6 подставит абсолютный путь рабочего каталога процесса).
func newRuleSetCollector(st *BuildSettings) *ruleSetCollector {
	geoDir := strings.TrimSpace(st.GeoDir)
	if geoDir == "" {
		geoDir = "geo"
	}
	return &ruleSetCollector{geoDir: geoDir, defs: map[string]map[string]any{}}
}

// add — гео-ссылка kind:name ("geosite"/"geoip") → тег "kind-name" и
// определение локального binary rule-set из поставки. Повторные ссылки
// дедуплицируются по тегу (как в оракуле).
func (c *ruleSetCollector) add(kind, name string) string {
	if name == "" {
		return ""
	}
	tag := kind + "-" + name
	if _, ok := c.defs[tag]; ok {
		return tag
	}
	c.defs[tag] = map[string]any{
		"type":   "local",
		"tag":    tag,
		"format": "binary",
		// Каталог поставки — layout манифеста geo.json (UPDATE-SYSTEM §8):
		// <GeoDir>/<kind>/<name>.srs; разделитель "/" на всех платформах
		// (детерминизм golden; sing-box принимает на Windows).
		"path": c.geoDir + "/" + kind + "/" + name + ".srs",
	}
	c.order = append(c.order, tag)
	return tag
}

// list — определения rule_set в порядке встречи (nil, если пусто).
func (c *ruleSetCollector) list() []any {
	if len(c.order) == 0 {
		return nil
	}
	out := make([]any, 0, len(c.order))
	for _, tag := range c.order {
		out = append(out, c.defs[tag])
	}
	return out
}

// makeMatchRule — список строк пресета (синтаксис FR-UI-135 / v2ray) →
// матч-поля sing-box route/dns-правила (порт make_rule,
// ConfigBuilder.cpp:481–533). isIP: geoip: → rule_set, прочее → ip_cidr;
// домен: geosite: → rule_set, full: → domain, domain:/без-префикса →
// domain_suffix (v2ray Subdomain ⇒ sing-box suffix), keyword: →
// domain_keyword, regexp: → domain_regex; значения — toLower как в оракуле.
// Пустая map — список ничего не дал (правило не эмитится).
func makeMatchRule(list []string, isIP bool, rs *ruleSetCollector) map[string]any {
	rule := map[string]any{}
	var ruleSet, ipCidr, domainFull, domainSub, domainKw, domainRe []any
	ipIsPrivate := false
	ruleSetSeen := map[string]bool{}
	appendRuleSet := func(tag string) {
		if tag == "" || ruleSetSeen[tag] {
			return // дедуп и в определениях, и внутри правила (deviations.yaml)
		}
		ruleSetSeen[tag] = true
		ruleSet = append(ruleSet, tag)
	}
	for _, item := range list {
		if item == "" {
			continue
		}
		if isIP {
			if rest, ok := strings.CutPrefix(item, "geoip:"); ok {
				// «geoip:private» — встроенное свойство sing-box, а НЕ файл
				// rule-set: апстрим не публикует geoip-private.srs, и попытка
				// открыть его валила старт («invalid rule-set file»).
				if rest == "private" {
					ipIsPrivate = true
					continue
				}
				appendRuleSet(rs.add("geoip", rest))
			} else {
				ipCidr = append(ipCidr, item)
			}
			continue
		}
		switch {
		case strings.HasPrefix(item, "geosite:"):
			appendRuleSet(rs.add("geosite", strings.TrimPrefix(item, "geosite:")))
		case strings.HasPrefix(item, "full:"):
			domainFull = append(domainFull, strings.ToLower(strings.TrimPrefix(item, "full:")))
		case strings.HasPrefix(item, "domain:"):
			domainSub = append(domainSub, strings.ToLower(strings.TrimPrefix(item, "domain:")))
		case strings.HasPrefix(item, "keyword:"):
			domainKw = append(domainKw, strings.ToLower(strings.TrimPrefix(item, "keyword:")))
		case strings.HasPrefix(item, "regexp:"):
			domainRe = append(domainRe, strings.ToLower(strings.TrimPrefix(item, "regexp:")))
		default:
			domainSub = append(domainSub, strings.ToLower(item))
		}
	}
	if len(ruleSet) > 0 {
		rule["rule_set"] = ruleSet
	}
	if isIP {
		if len(ipCidr) > 0 {
			rule["ip_cidr"] = ipCidr
		}
		if ipIsPrivate {
			rule["ip_is_private"] = true
		}
		return rule
	}
	if len(domainFull) > 0 {
		rule["domain"] = domainFull
	}
	if len(domainSub) > 0 {
		rule["domain_suffix"] = domainSub
	}
	if len(domainKw) > 0 {
		rule["domain_keyword"] = domainKw
	}
	if len(domainRe) > 0 {
		rule["domain_regex"] = domainRe
	}
	return rule
}

// routeRules — route.rules в порядке sing-box и источник каждого правила
// (lists[i]: "direct_domain", "process_rules[0]"...). ЕДИНСТВЕННОЕ место, где
// задан порядок правил: его вызывают buildRoute (конфиг) и BuildRouteTable
// (тестер маршрута), поэтому разойтись они не могут.
func routeRules(preset *presets.Preset, st *BuildSettings, forTest bool, rs *ruleSetCollector, warn func(string)) (rules []any, lists []string, _ error) {
	// mark подписывает правила, добавленные с прошлого вызова, списком-источником.
	mark := func(list string, indexed bool) {
		for k := 0; len(lists) < len(rules); k++ {
			name := list
			if indexed {
				name = fmt.Sprintf("%s[%d]", list, k)
			}
			lists = append(lists, name)
		}
	}

	// Sniffing (FR-UI-132): режимы Disable(0)/ForRouting(1); третьего режима
	// некора нет (DROP). Перенесён из полей inbound в route-action (1.13+).
	if preset.Routing.SniffingMode != 0 {
		rules = append(rules, map[string]any{
			"action":  "sniff",
			"inbound": []any{"mixed-in", "tun-in"},
			"timeout": "3s",
		})
	}
	mark("sniff", false)

	// DNS hijack: перехват DNS-запросов из туннеля/слушателя.
	if !forTest {
		rules = append(rules, map[string]any{
			"protocol": "dns",
			"action":   "hijack-dns",
		})
	}
	mark("hijack_dns", false)

	// Custom-правила пресета — ПЕРВЫМИ (обновление пресета bd56088
	// 2026-10-26: пин Riot→proxy обязан иметь приоритет над geosite:riot
	// в direct-списке; соответствует и C++-оракулу, ставившему custom до
	// сгенерированных списков). Custom-правила уже в формате sing-box.
	if !forTest {
		presetRules, err := parseCustomRules(preset.Routing.CustomRules, "preset.routing.custom_rules")
		if err != nil {
			return nil, nil, err
		}
		rules = append(rules, presetRules...)
		mark("custom_rules", true)
		rules = append(rules, globalCustomRules(st, warn)...)
		mark("custom_global", true)
	}

	// Списки пресета (§3.1.5): block → proxy → direct; домены, затем IP.
	// Приоритет proxy перед direct-geosite — семантика google.ru
	// (PRESET-LAV-RU §3.3). Выход proxy — тег цепочки ("proxy").
	addRuleRoute := func(list []string, isIP bool, outbound string) {
		rule := makeMatchRule(list, isIP, rs)
		if len(rule) == 0 {
			return
		}
		rule["outbound"] = outbound
		rules = append(rules, rule)
	}
	if !forTest {
		addRuleRoute(preset.Routing.BlockDomain, false, "block")
		mark("block_domain", false)
		addRuleRoute(preset.Routing.ProxyDomain, false, "proxy")
		mark("proxy_domain", false)
		addRuleRoute(preset.Routing.DirectDomain, false, "bypass")
		mark("direct_domain", false)
		addRuleRoute(preset.Routing.BlockIP, true, "block")
		mark("block_ip", false)
		addRuleRoute(preset.Routing.ProxyIP, true, "proxy")
		mark("proxy_ip", false)
		addRuleRoute(preset.Routing.DirectIP, true, "bypass")
		mark("direct_ip", false)
	}

	// Процессные правила пресета (SCHEMA §2.1): фильтр по платформе сборки;
	// на OpenWrt-сборке опускаются (роутер — без процессов-клиентов).
	if !forTest {
		for _, pr := range effectiveProcessRules(preset, st) {
			rules = append(rules, map[string]any{
				"process_name": stringsToAny(pr.ProcessNames),
				"outbound":     pr.Outbound,
			})
		}
		mark("process_rules", true)
	}

	// TUN-списки (FR-UI-184): bypass-процессы/CIDR мимо туннеля; whitelist
	// инвертирует ТОЛЬКО эти списки (поправка №28), процессные правила
	// пресета не трогает. Авто-байпас внешних ядер — W6.
	if st.Tun.Enabled && !forTest {
		matchOut := "bypass"
		if st.Tun.Whitelist {
			matchOut = "proxy"
		}
		if names := splitLinesSkipSharp(st.Tun.BypassProcess); len(names) > 0 {
			rules = append(rules, map[string]any{
				"process_name": names,
				"outbound":     matchOut,
			})
		}
		if cidrs := splitLinesSkipSharp(st.Tun.BypassCIDR); len(cidrs) > 0 {
			rules = append(rules, map[string]any{
				"ip_cidr":  cidrs,
				"outbound": matchOut,
			})
		}
		mark("tun_bypass", true)
	}

	// Built-in блокировки: NETBIOS/mDNS по UDP и multicast-диапазоны
	// (назначение и источник) — как в оракуле, без forTest-гуарда.
	rules = append(rules,
		map[string]any{
			"network":  []any{"udp"},
			"port":     []any{135, 137, 138, 139, 5353},
			"outbound": "block",
		},
		map[string]any{
			"ip_cidr":  []any{"224.0.0.0/3", "ff00::/8"},
			"outbound": "block",
		},
		map[string]any{
			"source_ip_cidr": []any{"224.0.0.0/3", "ff00::/8"},
			"outbound":       "block",
		},
	)

	mark("builtin_block", true)
	return rules, lists, nil
}

// buildRoute — route{} конфига (§3.4 шаг 6). kill-switch на уровне route —
// см. deviations.yaml: v1 = форс strict_route (inbounds.go) + фаервол
// оболочки (W8);DefOutbound final — только в рабочих конфигах (не forTest).
func buildRoute(preset *presets.Preset, st *BuildSettings, res *BuildResult, forTest, forExport bool, rs *ruleSetCollector) (map[string]any, error) {
	rules, _, err := routeRules(preset, st, forTest, rs, res.warn)
	if err != nil {
		return nil, err
	}
	route := map[string]any{"rules": rules}
	if defs := rs.list(); len(defs) > 0 {
		route["rule_set"] = defs
	}
	// Резолв доменов outbounds через DNS-сервер (1.12+): strategy — только
	// если задана пресетом (канонические golden без шумовых полей).
	resolver := map[string]any{"server": "dns-direct"}
	if s := preset.Routing.OutboundDomainStrategy; s != "" {
		resolver["strategy"] = s
	}
	route["default_domain_resolver"] = resolver
	if st.Tun.Enabled && !forExport {
		route["auto_detect_interface"] = true
	}
	if !forTest {
		route["final"] = preset.Routing.DefOutbound
	}
	return route, nil
}

// effectiveProcessRules — правила пресета, применимые на платформе (обёртка
// presets.EffectiveProcessRules; предупреждения о пропуске — в лог W6).
func effectiveProcessRules(preset *presets.Preset, st *BuildSettings) []presets.ProcessRule {
	rules, _ := presets.EffectiveProcessRules(preset, goosOf(st), st.OpenWRT)
	return rules
}

// buildExperimental — experimental.cache_file (LNB-36/FR-UI-266): включён
// всегда в рабочих конфигах — кэш rule-set даёт старт без сети; path — из
// настроек ("" → путь sing-box по умолчанию). Прочие подсекции experimental
// (clash_api и т.п.) — W6/W7.
func buildExperimental(st *BuildSettings) map[string]any {
	cache := map[string]any{"enabled": true}
	if p := strings.TrimSpace(st.CacheFilePath); p != "" {
		cache["path"] = p
	}
	return map[string]any{"cache_file": cache}
}

// splitLinesSkipSharp — многострочник → строки: пустые/пробельные и
// #-комментарии пропускаются (порт SplitLinesSkipSharp, NekoGui_Utils.cpp:33;
// расширение — trim значений, иначе пробельный хвост давал бы битые CIDR).
func splitLinesSkipSharp(s string) []any {
	var out []any
	for _, line := range strings.FieldsFunc(s, func(r rune) bool { return r == '\n' || r == '\r' }) {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		out = append(out, line)
	}
	return out
}

func stringsToAny(list []string) []any {
	out := make([]any, 0, len(list))
	for _, s := range list {
		out = append(out, s)
	}
	return out
}
