// SPDX-License-Identifier: GPL-3.0-only

package presets

// Конвертер маршрутов NekoRay 4.x → формат LAVRay (SCHEMA §6; источник —
// файлы config/routes_box/<name> RoutingBean; каталог routes/ старых форков
// принимается тоже). Эталон конвертации — presets/lav-ru.source.json →
// presets/lav-ru.json (golden-тест TestGoldenLavRU, byte-for-byte; G28).
//
// DROP-поля некора (SCHEMA §3): domain_strategy, direct_dns_strategy,
// dns_final_out — игнорируются с warning; sniffing_mode 2 → warning + 1.
//
// FuzzConvertNekoRayRoute: инвариант — нет паник, схема результата валидна.

import (
	"encoding/json"
	"fmt"
	"strings"
)

// NekoRayRoute — RoutingBean NekoRay 4.x (main/NekoGui_DataStore.hpp Routing).
type NekoRayRoute struct {
	DirectIP     string `json:"direct_ip"`
	DirectDomain string `json:"direct_domain"`
	ProxyIP      string `json:"proxy_ip"`
	ProxyDomain  string `json:"proxy_domain"`
	BlockIP      string `json:"block_ip"`
	BlockDomain  string `json:"block_domain"`
	DefOutbound  string `json:"def_outbound"`
	Custom       string `json:"custom"`

	RemoteDNS         string `json:"remote_dns"`
	RemoteDNSStrategy string `json:"remote_dns_strategy"`
	DirectDNS         string `json:"direct_dns"`
	DirectDNSStrategy string `json:"direct_dns_strategy"`
	DNSRouting        bool   `json:"dns_routing"`
	UseDNSObject      bool   `json:"use_dns_object"`
	DNSObject         string `json:"dns_object"`
	DNSFinalOut       string `json:"dns_final_out"`

	DomainStrategy         string `json:"domain_strategy"`
	OutboundDomainStrategy string `json:"outbound_domain_strategy"`
	SniffingMode           int    `json:"sniffing_mode"`

	// UnderlyingDNS — Core Option некора (не в RoutingBean); при непустом
	// значении (перенос из Core Options) попадает в dns.underlying_dns.
	UnderlyingDNS string `json:"underlying_dns,omitempty"`
}

// nekoCustomRules — содержимое поля custom.
type nekoCustomRules struct {
	Rules []map[string]json.RawMessage `json:"rules"`
}

// ConvertOptions — метаданные пресета-приёмника (не выводятся из RoutingBean)
// и правила конвертации builtin.
type ConvertOptions struct {
	Meta PresetMeta
	// AddAdsBlocklist — §3b PRESET-LAV-RU: все builtin содержат
	// geosite:category-ads-all в block_domain (в пользовательской копии
	// можно удалить явно).
	AddAdsBlocklist bool
	// DefaultUnderlyingDNS — дефолт dns.underlying_dns ("local", настройка
	// core.underlying_dns); RoutingBean его не хранит.
	DefaultUnderlyingDNS string
}

// PresetMeta — обложка пресета (id/ревизия/имена/описания).
type PresetMeta struct {
	Schema          string    `json:"$schema"`
	Version         int       `json:"version"`
	Revision        int       `json:"revision"`
	ID              string    `json:"id"`
	Builtin         bool      `json:"builtin"`
	Name            Localized `json:"name"`
	Description     Localized `json:"description"`
	MaintainerNotes string    `json:"maintainer_notes,omitempty"`
}

// ConvertNekoRayRoute конвертирует RoutingBean в пресет LAVRay.
// Возвращает пресет и список предупреждений (DROP-поля и пр.); ошибка —
// только для структурно битого входа.
func ConvertNekoRayRoute(src *NekoRayRoute, opts ConvertOptions) (*Preset, []string, error) {
	var warnings []string
	warnf := func(format string, args ...any) {
		warnings = append(warnings, fmt.Sprintf(format, args...))
	}

	out := &Preset{
		Schema:          opts.Meta.Schema,
		Version:         opts.Meta.Version,
		Revision:        opts.Meta.Revision,
		ID:              opts.Meta.ID,
		Builtin:         opts.Meta.Builtin,
		Name:            opts.Meta.Name,
		Description:     opts.Meta.Description,
		MaintainerNotes: opts.Meta.MaintainerNotes,
	}

	r := &out.Routing

	// def_outbound (дефолт некора — "proxy")
	r.DefOutbound = src.DefOutbound
	if r.DefOutbound == "" {
		r.DefOutbound = "proxy"
	}
	switch r.DefOutbound {
	case "proxy", "direct", "block":
	default:
		return nil, warnings, fmt.Errorf("def_outbound: %q (допустимо proxy/direct/block)", r.DefOutbound)
	}

	// Стратегии домена: AsIs→"", UseIPv4→ipv4_only и т.д. (enum v2ray).
	r.OutboundDomainStrategy = mapStrategy(src.OutboundDomainStrategy, warnf, "outbound_domain_strategy")
	if src.DomainStrategy != "" && src.DomainStrategy != "AsIs" {
		warnf("domain_strategy = %q — DROP (FR-UI-131: мёртвое поле некора; §3 SCHEMA)", src.DomainStrategy)
	}

	// sniffing: 2 (все режимы) некора — DROP аудита → warning + ForRouting.
	r.SniffingMode = src.SniffingMode
	if r.SniffingMode == 2 {
		warnf("sniffing_mode = 2 не переносится (FR-UI-132: третий режим DROP) — установлен 1 (ForRouting)")
		r.SniffingMode = 1
	}

	// Списки: строки с \n → массивы построчно, «#»-комментарии вырезаются.
	r.ProxyDomain = splitLines(src.ProxyDomain)
	r.DirectDomain = splitLines(src.DirectDomain)
	r.ProxyIP = splitLines(src.ProxyIP)
	r.DirectIP = splitLines(src.DirectIP)
	r.BlockDomain = splitLines(src.BlockDomain)
	r.BlockIP = splitLines(src.BlockIP)
	if opts.AddAdsBlocklist && !contains(r.BlockDomain, "geosite:category-ads-all") {
		r.BlockDomain = append(r.BlockDomain, "geosite:category-ads-all") // §3b
	}

	// custom → процессные правила / custom_rules.
	customRules, customRest, customWarns, err := extractCustomRules(src.Custom)
	warnings = append(warnings, customWarns...)
	if err != nil {
		return nil, warnings, fmt.Errorf("custom: %w", err)
	}
	r.ProcessRules = customRules
	r.CustomRules = customRest

	// DNS.
	d := &out.DNS
	if src.UseDNSObject {
		d.Mode = "advanced"
		d.AdvancedJSON = src.DNSObject
		if d.AdvancedJSON == "" {
			d.AdvancedJSON = "{}"
		}
		warnf("use_dns_object=true → dns.mode=advanced (валидация объекта — конфигбилдер, LNB-08)")
	} else {
		d.Mode = "simple"
	}
	d.RemoteDNS = src.RemoteDNS
	if d.RemoteDNS == "" {
		d.RemoteDNS = "https://dns.google/dns-query" // дефолт RoutingBean
	}
	d.DirectDNS = src.DirectDNS
	if d.DirectDNS == "" {
		d.DirectDNS = "local"
	}
	d.RemoteQueryStrategy = mapStrategy(src.RemoteDNSStrategy, warnf, "remote_dns_strategy")
	d.RoutingEnabled = src.DNSRouting
	d.UnderlyingDNS = src.UnderlyingDNS
	if d.UnderlyingDNS == "" {
		d.UnderlyingDNS = opts.DefaultUnderlyingDNS
	}
	if d.UnderlyingDNS == "" {
		d.UnderlyingDNS = "local"
	}
	if src.DirectDNSStrategy != "" && src.DirectDNSStrategy != "AsIs" {
		warnf("direct_dns_strategy = %q — DROP (FR-UI-148: не влияло на конфиг)", src.DirectDNSStrategy)
	}
	if src.DNSFinalOut != "" {
		warnf("dns_final_out = %q — DROP (FR-UI-148: не влияло на конфиг)", src.DNSFinalOut)
	}

	if err := out.Validate(); err != nil {
		return nil, warnings, fmt.Errorf("результат конвертации невалиден: %w", err)
	}
	return out, warnings, nil
}

// mapStrategy — enum v2ray → enum LAVRay; AsIs/"" → "".
func mapStrategy(v string, warnf func(string, ...any), field string) string {
	switch v {
	case "", "AsIs":
		return ""
	case "UseIPv4":
		return "ipv4_only"
	case "UseIPv6":
		return "ipv6_only"
	case "PreferIPv4":
		return "prefer_ipv4"
	case "PreferIPv6":
		return "prefer_ipv6"
	default:
		warnf("%s = %q — неизвестное значение, сброшено в \"\"", field, v)
		return ""
	}
}

// splitLines — построчно; «#»-комментарии (целые строки и хвосты) вырезаются;
// пустые строки отбрасываются (SCHEMA §6).
func splitLines(s string) []string {
	if s == "" {
		return []string{}
	}
	var out []string
	for _, line := range strings.Split(strings.ReplaceAll(s, "\r\n", "\n"), "\n") {
		if i := strings.Index(line, "#"); i >= 0 {
			line = line[:i]
		}
		line = strings.TrimSpace(line)
		if line != "" {
			out = append(out, line)
		}
	}
	if out == nil {
		return []string{}
	}
	return out
}

func contains(list []string, v string) bool {
	for _, s := range list {
		if s == v {
			return true
		}
	}
	return false
}

// extractCustomRules делит custom: правила с process_name → routing.
// process_rules (outbound proxy/bypass/block → proxy/direct/block, *.exe —
// windows-only §2.1; POSIX-имена — отдельной записью linux+macos), правила
// БЕЗ процессных полей остаются в routing.custom_rules (строкой JSON).
func extractCustomRules(custom string) ([]ProcessRule, string, []string, error) {
	var noWarns []string
	if strings.TrimSpace(custom) == "" {
		return []ProcessRule{}, "", noWarns, nil
	}
	var parsed nekoCustomRules
	if err := json.Unmarshal([]byte(custom), &parsed); err != nil {
		return nil, "", noWarns, fmt.Errorf("не является корректным JSON: %w", err)
	}
	var processRules []ProcessRule
	var restRules []map[string]json.RawMessage
	for _, rule := range parsed.Rules {
		var names []string
		if raw, ok := rule["process_name"]; ok {
			if err := json.Unmarshal(raw, &names); err != nil {
				// одиночная строка тоже допустима
				var one string
				if err2 := json.Unmarshal(raw, &one); err2 != nil {
					return nil, "", noWarns, fmt.Errorf("process_name: %w", err)
				}
				names = []string{one}
			}
		}
		if len(names) == 0 {
			restRules = append(restRules, rule)
			continue
		}
		var outbound string
		var warn string
		if raw, ok := rule["outbound"]; ok {
			if err := json.Unmarshal(raw, &outbound); err != nil {
				// W3-7 (ревью): молчаливый fallback в proxy — приватность;
				// теперь предупреждение в отчёт конвертации.
				warn = fmt.Sprintf("custom: правило с process_name имеет нечитаемый outbound (%v) — принято proxy", err)
			}
		}
		mapped := map[string]string{"proxy": "proxy", "bypass": "direct", "block": "block"}[outbound]
		if mapped == "" {
			if warn == "" && outbound != "" {
				warn = fmt.Sprintf("custom: правило с process_name: неизвестный outbound %q — принято proxy", outbound)
			}
			mapped = "proxy"
		}
		if warn != "" {
			noWarns = append(noWarns, warn)
		}
		// §2.1: *.exe — windows-only; остальные — POSIX (linux+macos).
		var win, posix []string
		for _, n := range names {
			if strings.HasSuffix(strings.ToLower(n), ".exe") {
				win = append(win, n)
			} else {
				posix = append(posix, n)
			}
		}
		if len(win) > 0 {
			processRules = append(processRules, ProcessRule{
				Platforms: []string{"windows"}, Outbound: mapped, ProcessNames: win,
			})
		}
		if len(posix) > 0 {
			processRules = append(processRules, ProcessRule{
				Platforms: []string{"linux", "macos"}, Outbound: mapped, ProcessNames: posix,
			})
		}
	}
	rest := ""
	if len(restRules) > 0 {
		b, err := json.Marshal(map[string]any{"rules": restRules})
		if err != nil {
			return nil, "", noWarns, fmt.Errorf("custom_rules: %w", err)
		}
		rest = string(b)
	}
	return processRules, rest, noWarns, nil
}
