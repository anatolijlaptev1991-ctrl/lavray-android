// SPDX-License-Identifier: GPL-3.0-only

// routetable.go — тестер маршрута (T-30): правила активного пресета из того же
// routeRules, что строит конфиг, и их оценка по семантике sing-box 1.14
// (route/rule/rule_abstract.go). Работает без запущенного профиля.
//
// Что вычисляется: process_name, network, port (все — «И»), группа «адрес
// назначения» (ИЛИ): domain, domain_suffix, domain_keyword, domain_regex,
// ip_cidr, ip_is_private и содержимое rule_set. Для домена IP-правила не
// срабатывают: без действия resolve sing-box не резолвит домен ради ip_cidr.
// Правила с другими условиями (inbound, source_*, protocol...) в оценке не
// участвуют; такие ключи попадают в RouteVerdict.Skipped.
package cfgbuild

import (
	"net/netip"
	"regexp"
	"strconv"
	"strings"

	"github.com/anatolijlaptev1991-ctrl/lavray-android/core/internal/presets"
)

// RuleSetRef — гео-набор, на который ссылается rule_set правила.
type RuleSetRef struct {
	Path string // <GeoDir>/<kind>/<name>.srs — как в route.rule_set конфига
	Rel  string // <kind>/<name>.srs — путь внутри каталога поставки
}

// RouteTable — упорядоченные правила маршрута активного пресета.
type RouteTable struct {
	Rules    []any // route.rules (карты sing-box), порядок как в конфиге
	Lists    []string
	Final    string // route.final
	RuleSets map[string]RuleSetRef
}

// BuildRouteTable собирает таблицу тем же routeRules, что и buildRoute.
// preset == nil — нейтральный набор (как при сборке конфига).
func BuildRouteTable(preset *presets.Preset, st *BuildSettings) (*RouteTable, error) {
	if preset == nil {
		preset = NeutralPreset()
	}
	rs := newRuleSetCollector(st)
	rules, lists, err := routeRules(preset, st, false, rs, nil)
	if err != nil {
		return nil, err
	}
	t := &RouteTable{Rules: rules, Lists: lists, Final: preset.Routing.DefOutbound, RuleSets: map[string]RuleSetRef{}}
	for tag, def := range rs.defs {
		path, _ := def["path"].(string)
		t.RuleSets[tag] = RuleSetRef{Path: path, Rel: strings.TrimPrefix(path, rs.geoDir+"/")}
	}
	return t, nil
}

// RouteQuery — что проверяется: домен или IP назначения, порт, процесс.
type RouteQuery struct {
	Host    string     // домен в нижнем регистре без точки на конце; пусто, если задан IP
	IP      netip.Addr // невалидный, если задан домен
	Port    int        // 0 — неизвестен
	Network string     // "tcp" (по умолчанию) | "udp"
	Process string     // имя процесса; пусто — не задано
}

// ParseRouteQuery разбирает ввод тестера: домен, IP, IP:порт или URL.
func ParseRouteQuery(addr, process string) RouteQuery {
	q := RouteQuery{Network: "tcp", Process: strings.TrimSpace(process)}
	rest := strings.TrimSpace(addr)
	if i := strings.Index(rest, "://"); i >= 0 {
		switch strings.ToLower(rest[:i]) {
		case "http", "ws":
			q.Port = 80
		case "https", "wss":
			q.Port = 443
		}
		rest = rest[i+3:]
	}
	if i := strings.IndexAny(rest, "/?#"); i >= 0 {
		rest = rest[:i]
	}
	if i := strings.LastIndex(rest, "@"); i >= 0 {
		rest = rest[i+1:]
	}
	host, port := rest, ""
	switch {
	case strings.HasPrefix(rest, "["): // [ipv6]:port
		if end := strings.Index(rest, "]"); end > 0 {
			host, port = rest[1:end], strings.TrimPrefix(rest[end+1:], ":")
		}
	case strings.Count(rest, ":") == 1: // host:port
		host, port, _ = strings.Cut(rest, ":")
	}
	if n, err := strconv.Atoi(port); err == nil && n > 0 && n < 65536 {
		q.Port = n
	}
	host = strings.ToLower(strings.TrimSuffix(host, "."))
	if ip, err := netip.ParseAddr(host); err == nil {
		q.IP = ip.Unmap()
	} else {
		q.Host = host
	}
	return q
}

// RuleSetMatcher — содержимое гео-набора (реализация — пакет geo).
type RuleSetMatcher interface {
	MatchDomain(host string) bool
	MatchIP(ip netip.Addr) bool
}

// RuleSetLookup возвращает набор по тегу rule_set (ошибка — набор не читается).
type RuleSetLookup func(tag string) (RuleSetMatcher, error)

// RouteVerdict — исход проверки.
type RouteVerdict struct {
	Outbound   string   // proxy | direct | bypass | block | тег custom-правила
	Rule       string   // сработавшее условие: "rule_set geosite-category-ru"
	List       string   // источник правила ("direct_domain", "process_rules[0]") или "default"
	Index      int      // индекс в RouteTable.Rules; -1 — сработал route.final
	Unresolved []string // rule_set, которые не удалось прочитать, и неподдерживаемые ключи
}

// supportedKeys — ключи правила, которые умеет оценивать тестер.
var supportedKeys = map[string]bool{
	"outbound": true, "action": true, "process_name": true, "network": true, "port": true,
	"domain": true, "domain_suffix": true, "domain_keyword": true, "domain_regex": true,
	"ip_cidr": true, "ip_is_private": true, "rule_set": true,
}

// Evaluate прогоняет запрос через правила по порядку; первое сработавшее
// правило с outbound решает исход, иначе действует route.final.
func (t *RouteTable) Evaluate(q RouteQuery, look RuleSetLookup) RouteVerdict {
	v := RouteVerdict{Outbound: t.Final, List: "default", Index: -1}
	sets := map[string]RuleSetMatcher{}
	for i, raw := range t.Rules {
		rule, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		out, _ := rule["outbound"].(string)
		if out == "" {
			if a, _ := rule["action"].(string); a != "reject" {
				continue // sniff / hijack-dns / predefined не выбирают исход
			}
			out = "block" // action reject — то же, что исход block
		}
		hit, why := matchRule(rule, q, look, sets, &v.Unresolved)
		if hit {
			v.Outbound, v.Rule, v.List, v.Index = out, why, t.Lists[i], i
			return v
		}
	}
	return v
}

func matchRule(rule map[string]any, q RouteQuery, look RuleSetLookup, sets map[string]RuleSetMatcher, skipped *[]string) (bool, string) {
	for k := range rule {
		if strings.HasPrefix(k, "source_") {
			return false, "" // источник соединения тестеру неизвестен
		}
		if !supportedKeys[k] {
			*skipped = append(*skipped, "key:"+k)
			return false, ""
		}
	}
	// Условия «И».
	if names := strList(rule["process_name"]); len(names) > 0 && !containsFold(names, q.Process) {
		return false, ""
	}
	if nets := strList(rule["network"]); len(nets) > 0 && !containsFold(nets, q.Network) {
		return false, ""
	}
	if ports := intList(rule["port"]); len(ports) > 0 && !containsInt(ports, q.Port) {
		return false, ""
	}
	// Группа «адрес назначения» — ИЛИ по всем предикатам и rule_set.
	required, matched, why := false, false, ""
	hit := func(ok bool, cond string) {
		required = true
		if ok && !matched {
			matched, why = true, cond
		}
	}
	if l := strList(rule["domain"]); len(l) > 0 {
		hit(q.Host != "" && containsFold(l, q.Host), "domain")
	}
	if l := strList(rule["domain_suffix"]); len(l) > 0 {
		hit(q.Host != "" && anySuffix(l, q.Host), "domain_suffix")
	}
	if l := strList(rule["domain_keyword"]); len(l) > 0 {
		hit(q.Host != "" && anyContains(l, q.Host), "domain_keyword")
	}
	if l := strList(rule["domain_regex"]); len(l) > 0 {
		hit(q.Host != "" && anyRegexp(l, q.Host), "domain_regex")
	}
	if l := strList(rule["ip_cidr"]); len(l) > 0 {
		hit(q.IP.IsValid() && anyPrefix(l, q.IP), "ip_cidr")
	}
	if b, _ := rule["ip_is_private"].(bool); b {
		hit(q.IP.IsValid() && !isPublicAddr(q.IP), "ip_is_private")
	}
	if tags := strList(rule["rule_set"]); len(tags) > 0 {
		required = true
		for _, tag := range tags {
			m, ok := sets[tag]
			if !ok {
				var err error
				if m, err = look(tag); err != nil {
					*skipped = append(*skipped, "rule_set:"+tag)
					continue
				}
				sets[tag] = m
			}
			if (q.Host != "" && m.MatchDomain(q.Host)) || (q.IP.IsValid() && m.MatchIP(q.IP)) {
				hit(true, "rule_set "+tag)
				break
			}
		}
	}
	if required && !matched {
		return false, ""
	}
	if !required { // только процесс/сеть/порт (или пустое правило)
		why = firstKey(rule, "process_name", "port", "network")
	}
	return true, why
}

func firstKey(rule map[string]any, keys ...string) string {
	for _, k := range keys {
		if _, ok := rule[k]; ok {
			return k
		}
	}
	return "all"
}

// strList — []string из значения правила ([]any после JSON либо []string).
func strList(v any) []string {
	switch l := v.(type) {
	case []string:
		return l
	case []any:
		out := make([]string, 0, len(l))
		for _, x := range l {
			if s, ok := x.(string); ok {
				out = append(out, s)
			}
		}
		return out
	case string:
		return []string{l}
	}
	return nil
}

func intList(v any) []int {
	l, _ := v.([]any)
	out := make([]int, 0, len(l))
	for _, x := range l {
		switch n := x.(type) {
		case int:
			out = append(out, n)
		case float64:
			out = append(out, int(n))
		}
	}
	return out
}

func containsFold(list []string, s string) bool {
	for _, x := range list {
		if strings.EqualFold(x, s) {
			return true
		}
	}
	return false
}

func containsInt(list []int, n int) bool {
	for _, x := range list {
		if x == n {
			return true
		}
	}
	return false
}

// anySuffix — семантика domain_suffix sing-box: «x» — сам домен и поддомены,
// «.x» — только поддомены.
func anySuffix(list []string, host string) bool {
	for _, s := range list {
		s = strings.ToLower(s)
		if strings.HasPrefix(s, ".") {
			if strings.HasSuffix(host, s) {
				return true
			}
		} else if host == s || strings.HasSuffix(host, "."+s) {
			return true
		}
	}
	return false
}

func anyContains(list []string, host string) bool {
	for _, s := range list {
		if s != "" && strings.Contains(host, strings.ToLower(s)) {
			return true
		}
	}
	return false
}

func anyRegexp(list []string, host string) bool {
	for _, expr := range list {
		if re, err := regexp.Compile(expr); err == nil && re.MatchString(host) {
			return true
		}
	}
	return false
}

func anyPrefix(list []string, ip netip.Addr) bool {
	for _, s := range list {
		if p, err := netip.ParsePrefix(s); err == nil && p.Contains(ip) {
			return true
		}
		if a, err := netip.ParseAddr(s); err == nil && a == ip {
			return true
		}
	}
	return false
}

// isPublicAddr — как sing common/network.IsPublicAddr (ip_is_private = !public).
func isPublicAddr(a netip.Addr) bool {
	return !(a.IsPrivate() || a.IsLoopback() || a.IsMulticast() ||
		a.IsLinkLocalUnicast() || a.IsInterfaceLocalMulticast() || a.IsUnspecified())
}
