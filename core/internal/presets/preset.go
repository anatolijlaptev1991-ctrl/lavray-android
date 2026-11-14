// SPDX-License-Identifier: GPL-3.0-only

// Package presets — маршрутные пресеты LAVRay (W3): формат preset.schema.v1
// (SCHEMA.md рядом), загрузка встроенных наборов (go:embed), валидация при
// загрузке (§4 SCHEMA) и конвертер из NekoRay 4.x (RoutingBean, §6 SCHEMA).
package presets

import (
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
)

// Поддерживаемая версия схемы (минорные назад — SCHEMA §1).
const SchemaVersion = 1

// Localized — map<lang,string> с каноническим порядком ключей при маршале
// (ru, zh, en, es, de — как в поставке presets/*.json; прочие языки —
// алфавитно после). Даёт байт-в-байт стабильный golden.
type Localized map[string]string

// canonicalLangs — порядок поставки (SCHEMA: бренды не переводятся, говорящие
// имена — 5 языков).
var canonicalLangs = []string{"ru", "zh", "en", "es", "de"}

// MarshalJSON пишет известные языки в каноническом порядке, затем прочие
// по алфавиту (детерминизм golden).
func (l Localized) MarshalJSON() ([]byte, error) {
	if len(l) == 0 {
		return []byte("{}"), nil
	}
	used := map[string]bool{}
	var sb strings.Builder
	sb.WriteByte('{')
	first := true
	emit := func(lang string) error {
		v, ok := l[lang]
		if !ok {
			return nil
		}
		if !first {
			sb.WriteByte(',')
		}
		first = false
		k, err := json.Marshal(lang)
		if err != nil {
			return err
		}
		val, err := json.Marshal(v)
		if err != nil {
			return err
		}
		sb.Write(k)
		sb.WriteByte(':')
		sb.Write(val)
		used[lang] = true
		return nil
	}
	for _, lang := range canonicalLangs {
		if err := emit(lang); err != nil {
			return nil, err
		}
	}
	var rest []string
	for lang := range l {
		if !used[lang] {
			rest = append(rest, lang)
		}
	}
	sort.Strings(rest)
	for _, lang := range rest {
		if err := emit(lang); err != nil {
			return nil, err
		}
	}
	sb.WriteByte('}')
	return []byte(sb.String()), nil
}

// Preset — файл пресета (SCHEMA §1).
type Preset struct {
	Schema          string    `json:"$schema"`
	Version         int       `json:"version"`
	Revision        int       `json:"revision"`
	ID              string    `json:"id"`
	Builtin         bool      `json:"builtin"`
	Name            Localized `json:"name"`
	Description     Localized `json:"description"`
	MaintainerNotes string    `json:"maintainer_notes,omitempty"`
	Routing         Routing   `json:"routing"`
	DNS             DNSBlock  `json:"dns"`
	// SourceID/SourceRevision — встроенный набор, с которого снята копия (T-80;
	// у файлов встроенных и у наборов «с нуля» пусто, omitempty их не пишет).
	SourceID       string `json:"source_id,omitempty"`
	SourceRevision int    `json:"source_revision,omitempty"`
}

// Routing — блок routing (SCHEMA §2).
type Routing struct {
	DefOutbound            string        `json:"def_outbound"`
	OutboundDomainStrategy string        `json:"outbound_domain_strategy"`
	SniffingMode           int           `json:"sniffing_mode"`
	ProxyDomain            []string      `json:"proxy_domain"`
	DirectDomain           []string      `json:"direct_domain"`
	BlockDomain            []string      `json:"block_domain"`
	ProxyIP                []string      `json:"proxy_ip"`
	DirectIP               []string      `json:"direct_ip"`
	BlockIP                []string      `json:"block_ip"`
	ProcessRules           []ProcessRule `json:"process_rules"`
	CustomRules            string        `json:"custom_rules,omitempty"` // JSON sing-box rules
}

// ProcessRule — platform-scoped процессное правило (SCHEMA §2.1).
type ProcessRule struct {
	Platforms    []string `json:"platforms,omitempty"`
	Outbound     string   `json:"outbound"`
	ProcessNames []string `json:"process_names"`
}

// DNSBlock — блок dns (SCHEMA §3).
type DNSBlock struct {
	Mode                string `json:"mode"` // simple | advanced
	RemoteDNS           string `json:"remote_dns"`
	RemoteQueryStrategy string `json:"remote_query_strategy"`
	DirectDNS           string `json:"direct_dns"`
	RoutingEnabled      bool   `json:"routing_enabled"`
	UnderlyingDNS       string `json:"underlying_dns,omitempty"`
	AdvancedJSON        string `json:"advanced_json,omitempty"` // mode=advanced (FR-UI-149)
}

// Имена платформ процессных правил (SCHEMA §2.1). openwrt допустим в файле
// (правило для роутера), но на ПК не действует — см. EffectiveProcessRules.
var knownPlatforms = map[string]bool{"windows": true, "linux": true, "macos": true, "openwrt": true}

// Известные префиксы строк списков (FR-UI-135); geoip — только ip-поля,
// geosite — только domain-поля (валидация §4 SCHEMA).
var knownPrefixes = []string{"geosite:", "geoip:", "domain:", "full:", "keyword:", "regexp:"}

var domainStrategies = map[string]bool{
	"": true, "ipv4_only": true, "ipv6_only": true, "prefer_ipv4": true, "prefer_ipv6": true,
}

// Validate проверяет пресет по SCHEMA §4 (ошибки — с путём поля).
func (p *Preset) Validate() error {
	if p.Version <= 0 || p.Version > SchemaVersion {
		return fmt.Errorf("version: схема %d не поддерживается (поддерживается ≤ %d)", p.Version, SchemaVersion)
	}
	if p.ID == "" {
		return errors.New("id: пуст")
	}
	if p.Revision < 0 || p.Revision > MaxRevision {
		return fmt.Errorf("revision: %d вне 0…%d", p.Revision, MaxRevision)
	}
	if len(p.Name) == 0 {
		return errors.New("name: пуст")
	}
	r := &p.Routing
	switch r.DefOutbound {
	case "proxy", "direct", "block":
	default:
		return fmt.Errorf("routing.def_outbound: %q (допустимо proxy/direct/block)", r.DefOutbound)
	}
	if !domainStrategies[r.OutboundDomainStrategy] {
		return fmt.Errorf("routing.outbound_domain_strategy: %q", r.OutboundDomainStrategy)
	}
	switch r.SniffingMode {
	case 0, 1: // 2 (все режимы) — DROP аудита; конвертер даёт warning
	default:
		return fmt.Errorf("routing.sniffing_mode: %d (допустимо 0/1; режим 2 некора не переносится)", r.SniffingMode)
	}
	for _, f := range []struct {
		path  string
		items []string
		isIP  bool
	}{
		{"proxy_domain", r.ProxyDomain, false},
		{"direct_domain", r.DirectDomain, false},
		{"block_domain", r.BlockDomain, false},
		{"proxy_ip", r.ProxyIP, true},
		{"direct_ip", r.DirectIP, true},
		{"block_ip", r.BlockIP, true},
	} {
		for _, s := range f.items {
			if err := validateListEntry(f.path, s, f.isIP); err != nil {
				return err
			}
		}
	}
	for i, rule := range r.ProcessRules {
		path := fmt.Sprintf("routing.process_rules[%d]", i)
		switch rule.Outbound {
		case "proxy", "direct", "block":
		default:
			return fmt.Errorf("%s.outbound: %q", path, rule.Outbound)
		}
		for _, plat := range rule.Platforms {
			if !knownPlatforms[plat] {
				return fmt.Errorf("%s.platforms: неизвестная платформа %q (windows/linux/macos/openwrt)", path, plat)
			}
		}
		for _, name := range rule.ProcessNames {
			// §4.3: только имена — без путей, «..», слэшей (анти-инъекция).
			if name == "" || strings.ContainsAny(name, "/\\") || strings.Contains(name, "..") {
				return fmt.Errorf("%s.process_names: %q — ожидалось имя процесса без пути", path, name)
			}
		}
		if len(rule.ProcessNames) == 0 {
			return fmt.Errorf("%s.process_names: пуст", path)
		}
	}
	if r.CustomRules != "" && !json.Valid([]byte(r.CustomRules)) {
		return errors.New("routing.custom_rules: не является корректным JSON")
	}
	d := &p.DNS
	switch d.Mode {
	case "simple", "advanced":
	default:
		return fmt.Errorf("dns.mode: %q (simple/advanced)", d.Mode)
	}
	if !domainStrategies[d.RemoteQueryStrategy] {
		return fmt.Errorf("dns.remote_query_strategy: %q", d.RemoteQueryStrategy)
	}
	if d.Mode == "advanced" && !json.Valid([]byte(d.AdvancedJSON)) {
		return errors.New("dns.advanced_json: mode=advanced требует валидный JSON объекта dns")
	}
	return nil
}

// validateListEntry — непустая строка, известный префикс, geoip только в ip,
// geosite только в domain (§4.2 SCHEMA).
func validateListEntry(path, s string, isIP bool) error {
	if s == "" {
		return fmt.Errorf("%s: пустая строка", path)
	}
	if strings.ContainsAny(s, " \t") || strings.ContainsRune(s, '#') {
		return fmt.Errorf("%s: %q — пробелы/«#» недопустимы в элементе списка", path, s)
	}
	pfx := ""
	for _, p := range knownPrefixes {
		if strings.HasPrefix(s, p) {
			pfx = p
			break
		}
	}
	if pfx == "" {
		if isIP && !isCIDROrIP(s) {
			return fmt.Errorf("%s: %q — geoip:/CIDR ожидаются в ip-полях", path, s)
		}
		return nil // суффикс-домен в domain-поле (FR-UI-135)
	}
	switch pfx {
	case "geoip:":
		if !isIP {
			return fmt.Errorf("%s: %q — geoip допустим только в ip-полях", path, s)
		}
	case "geosite:":
		if isIP {
			return fmt.Errorf("%s: %q — geosite допустим только в domain-полях", path, s)
		}
	case "domain:", "full:", "keyword:", "regexp:":
		if isIP {
			return fmt.Errorf("%s: %q — доменные префиксы недопустимы в ip-полях", path, s)
		}
		if len(s) == len(pfx) {
			return fmt.Errorf("%s: %q — пустое значение после префикса", path, s)
		}
	}
	return nil
}

func isCIDROrIP(s string) bool {
	if strings.Contains(s, "/") {
		return validCIDR(s)
	}
	return validIPish(s)
}

func validCIDR(s string) bool {
	i := strings.LastIndex(s, "/")
	if i <= 0 {
		return false
	}
	return validIPish(s[:i])
}

func validIPish(s string) bool {
	// достаточно поверхностной проверки (полная — на этапе конфигбилдера W4/W5)
	if s == "" {
		return false
	}
	for _, c := range s {
		if !(c >= '0' && c <= '9' || c == '.' || c == ':' || c >= 'a' && c <= 'f') {
			return false
		}
	}
	return true
}

// LoadPreset разбирает и валидирует пресет из JSON.
func LoadPreset(data []byte) (*Preset, error) {
	var p Preset
	if err := json.Unmarshal(data, &p); err != nil {
		return nil, fmt.Errorf("пресет: некорректный JSON: %w", err)
	}
	if err := p.Validate(); err != nil {
		return nil, fmt.Errorf("пресет %q: %w", p.ID, err)
	}
	return &p, nil
}

// BuiltinFiles — имена 5 builtin-наборов (PRESET-LAV-RU §3a; «Выборочный» —
// пустой пользовательский набор, не файл).
var BuiltinFiles = []string{
	"lav-ru-linux.json", "lav-ru.json", "lav-cn.json", "global.json", "all-proxy.json", "all-direct.json",
}

// BuiltinPresets загружает все встроенные пресеты (builtin/*.json).
func BuiltinPresets() ([]*Preset, error) {
	out := make([]*Preset, 0, len(BuiltinFiles))
	for _, name := range BuiltinFiles {
		data, err := builtinFS.ReadFile("builtin/" + name)
		if err != nil {
			return nil, fmt.Errorf("builtin: %w", err)
		}
		p, err := LoadPreset(data)
		if err != nil {
			return nil, fmt.Errorf("builtin %s: %w", name, err)
		}
		if !p.Builtin {
			return nil, fmt.Errorf("builtin %s: builtin=false", name)
		}
		out = append(out, p)
	}
	return out, nil
}
