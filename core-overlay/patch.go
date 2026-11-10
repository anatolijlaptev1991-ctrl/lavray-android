// SPDX-License-Identifier: GPL-3.0-only

package mobile

// patch.go — приведение конфига sing-box к правилам Android (docs/ARCHITECTURE.md,
// «Подмешивание платформы»). Чистая функция над JSON: ни сети, ни платформы,
// поэтому разбирается табличными тестами без запуска sing-box.
//
// Что делает patchConfig:
//   - удаляет inbound «mixed-in» и любой слушатель на localhost (другие
//     приложения телефона не должны видеть локальный прокси); слушатели на
//     не-loopback адресе (их cfgbuild допускает только с авторизацией)
//     остаются: их задал пользователь явно;
//   - удаляет experimental.clash_api и experimental.v2ray_api: это тоже
//     локальные слушатели;
//   - убирает process_name, process_path, process_path_regex из правил
//     маршрутизации и DNS (на телефоне они ничего не выбирают); правило,
//     в котором после этого не осталось условий, удаляется целиком, остальные
//     правила не затрагиваются;
//   - у inbound типа tun убирает interface_name (имя берётся из
//     дескриптора платформы), ставит stack gvisor (см. androidStack) и
//     подставляет mtu из настроек приложения;
//   - убирает route.default_interface и route.default_mark: имя интерфейса
//     рабочего стола и метка SO_MARK на телефоне недействительны;
//   - не трогает route.auto_detect_interface (см. platform.go: с платформой он
//     включает вызов Protect, а не привязку к интерфейсу).
//
// Конфиг без tun-inbound после правок означает режим «системный прокси», а
// на телефоне он невозможен: возвращается errNoTun.

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"strings"
)

// errNoTun — после правок в конфиге не осталось tun-inbound.
var errNoTun = errors.New("android: в конфиге нет tun-inbound (подключайтесь в режиме TUN)")

// tunParams — значения из настроек приложения; нулевое значение поля означает
// «оставить из конфига».
type tunParams struct {
	Stack string
	MTU   int
}

// patchReport — что именно сделал patchConfig (для журнала и тестов).
type patchReport struct {
	RemovedInbounds   []string // теги или типы удалённых inbound
	DroppedRules      int      // правила, опустевшие после снятия process_*
	StrippedConds     int      // снятые условия process_*
	NeedsOwnerLookup  bool     // остались правила package_name, user, user_id или find_process
	TunPatched        bool     // у tun-inbound заменены stack/mtu
	StackForced       string   // стек из конфига, заменённый на gvisor ("" — замены не было или стек не задан)
	RemovedExperiment []string // удалённые слушатели experimental
}

// processKeys — условия, привязанные к процессу рабочего стола.
var processKeys = []string{"process_name", "process_path", "process_path_regex"}

// ownerKeys — условия, ради которых sing-box ищет владельца соединения.
var ownerKeys = []string{"package_name", "package_name_regex", "user", "user_id"}

// optionKeys — ключи правила, которые не являются условиями: действие и его
// параметры. Правило без других ключей «пустое» и sing-box отвергает его.
var optionKeys = map[string]bool{
	"action": true, "outbound": true, "server": true, "method": true, "rcode": true,
	"answer": true, "ns": true, "extra": true, "strategy": true, "disable_cache": true,
	"disable_optimistic_cache": true, "rewrite_ttl": true, "client_subnet": true,
	"no_drop": true, "timeout": true, "sniffer": true, "override_address": true,
	"override_port": true, "network_strategy": true, "network_type": true,
	"fallback_network_type": true, "fallback_delay": true, "udp_disable_domain_unmapping": true,
	"udp_connect": true, "udp_timeout": true, "tls_fragment": true,
	"tls_fragment_fallback_delay": true, "tls_record_fragment": true, "invert": true,
	"type": true, "mode": true,
}

// patchConfig возвращает исправленную копию конфига. Входной срез не меняется.
func patchConfig(cfg []byte, p tunParams) ([]byte, patchReport, error) {
	var rep patchReport
	root, err := decodeObject(cfg)
	if err != nil {
		return nil, rep, fmt.Errorf("android: разбор конфига: %w", err)
	}
	if err := patchInbounds(root, p, &rep); err != nil {
		return nil, rep, err
	}
	patchRoute(root, &rep)
	patchSniff(root) // patch_sniff.go: потолок тайм-аута sniff
	patchDNS(root, &rep)
	patchExperimental(root, &rep)
	out, err := encodeObject(root)
	if err != nil {
		return nil, rep, fmt.Errorf("android: запись конфига: %w", err)
	}
	return out, rep, nil
}

// decodeObject разбирает JSON-объект, сохраняя числа как есть (json.Number):
// порты и тайм-ауты не превращаются в float64 с потерей вида записи.
func decodeObject(data []byte) (map[string]any, error) {
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber()
	var root map[string]any
	if err := dec.Decode(&root); err != nil {
		return nil, err
	}
	if root == nil {
		return nil, errors.New("корень конфига не объект")
	}
	return root, nil
}

func encodeObject(root map[string]any) ([]byte, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(root); err != nil {
		return nil, err
	}
	return bytes.TrimRight(buf.Bytes(), "\n"), nil
}

// patchInbounds убирает локальные слушатели и приводит tun-inbound.
func patchInbounds(root map[string]any, p tunParams, rep *patchReport) error {
	list, _ := root["inbounds"].([]any)
	kept := make([]any, 0, len(list))
	hasTun := false
	for _, item := range list {
		in, ok := item.(map[string]any)
		if !ok {
			kept = append(kept, item)
			continue
		}
		if isLocalListener(in) {
			rep.RemovedInbounds = append(rep.RemovedInbounds, inboundLabel(in))
			continue
		}
		if typ, _ := in["type"].(string); typ == "tun" {
			hasTun = true
			patchTun(in, p, rep)
		}
		kept = append(kept, in)
	}
	if !hasTun {
		return errNoTun
	}
	root["inbounds"] = kept
	return nil
}

// isLocalListener — inbound «mixed-in» либо слушатель, чей адрес пуст или
// loopback. Inbound без полей listen и listen_port (tun) слушателем не считается.
func isLocalListener(in map[string]any) bool {
	if tag, _ := in["tag"].(string); tag == "mixed-in" {
		return true
	}
	listen, hasListen := in["listen"].(string)
	_, hasPort := in["listen_port"]
	if !hasListen && !hasPort {
		return false
	}
	return listen == "" || isLoopback(listen)
}

func isLoopback(host string) bool {
	host = strings.TrimSpace(host)
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(strings.Trim(host, "[]"))
	return ip != nil && ip.IsLoopback()
}

func inboundLabel(in map[string]any) string {
	if tag, _ := in["tag"].(string); tag != "" {
		return tag
	}
	typ, _ := in["type"].(string)
	return typ
}

// androidStack — единственный стек туннеля, который работает на телефоне. Приложение
// исключено из своего VPN (addDisallowedApplication), поэтому ответы системного
// TCP-слушателя стеков system и mixed уходят мимо tun0 и соединения висят до тайм-аута
// (docs/MEASUREMENTS.md, 3.5). Стек из конфига или настроек (например, из резервной
// копии рабочего стола) заменяется, иначе туннель поднимается, но не передаёт данные.
const androidStack = "gvisor"

func patchTun(in map[string]any, p tunParams, rep *patchReport) {
	delete(in, "interface_name")
	if stack, _ := in["stack"].(string); stack != androidStack {
		in["stack"] = androidStack
		rep.TunPatched = true
		rep.StackForced = stack
	}
	if p.MTU > 0 {
		in["mtu"] = json.Number(fmt.Sprint(p.MTU))
		rep.TunPatched = true
	}
}

// patchRoute правит route: правила, rule_set с inline-правилами, интерфейс.
func patchRoute(root map[string]any, rep *patchReport) {
	route, ok := root["route"].(map[string]any)
	if !ok {
		return
	}
	delete(route, "default_interface")
	delete(route, "default_mark")
	if v, _ := route["find_process"].(bool); v {
		rep.NeedsOwnerLookup = true
	}
	if rules, ok := route["rules"].([]any); ok {
		route["rules"] = stripRules(rules, rep)
	}
	if sets, ok := route["rule_set"].([]any); ok {
		for _, s := range sets {
			set, ok := s.(map[string]any)
			if !ok {
				continue
			}
			if rules, ok := set["rules"].([]any); ok {
				set["rules"] = stripRules(rules, rep)
			}
		}
	}
}

func patchDNS(root map[string]any, rep *patchReport) {
	dns, ok := root["dns"].(map[string]any)
	if !ok {
		return
	}
	if rules, ok := dns["rules"].([]any); ok {
		dns["rules"] = stripRules(rules, rep)
	}
}

// patchExperimental убирает API с локальными слушателями.
func patchExperimental(root map[string]any, rep *patchReport) {
	exp, ok := root["experimental"].(map[string]any)
	if !ok {
		return
	}
	for _, key := range []string{"clash_api", "v2ray_api"} {
		if _, has := exp[key]; has {
			delete(exp, key)
			rep.RemovedExperiment = append(rep.RemovedExperiment, key)
		}
	}
}

// stripRules снимает process_* и возвращает новый список без опустевших правил.
func stripRules(rules []any, rep *patchReport) []any {
	out := make([]any, 0, len(rules))
	for _, item := range rules {
		rule, ok := item.(map[string]any)
		if !ok {
			out = append(out, item)
			continue
		}
		if stripRule(rule, rep) {
			rep.DroppedRules++
			continue
		}
		out = append(out, rule)
	}
	return out
}

// stripRule снимает process_* с одного правила (вложенные логические правила
// рекурсивно). true — правило опустело именно из-за этого и подлежит удалению.
func stripRule(rule map[string]any, rep *patchReport) bool {
	removed := 0
	for _, key := range processKeys {
		if _, has := rule[key]; has {
			delete(rule, key)
			removed++
		}
	}
	rep.StrippedConds += removed
	for _, key := range ownerKeys {
		if _, has := rule[key]; has {
			rep.NeedsOwnerLookup = true
		}
	}
	if typ, _ := rule["type"].(string); typ == "logical" {
		return stripLogical(rule, rep, removed)
	}
	return removed > 0 && !hasCondition(rule)
}

// stripLogical обрабатывает правило типа logical: опустевшие подправила
// выбрасываются, логическое правило без подправил удаляется, если что-то
// из-за нас в нём уже было снято.
func stripLogical(rule map[string]any, rep *patchReport, removed int) bool {
	sub, ok := rule["rules"].([]any)
	if !ok {
		return false
	}
	before := len(sub)
	rule["rules"] = stripRules(sub, rep)
	left, _ := rule["rules"].([]any)
	changed := removed > 0 || len(left) != before
	return changed && len(left) == 0
}

// hasCondition — осталось ли в правиле хотя бы одно условие (ключ, не
// относящийся к действию).
func hasCondition(rule map[string]any) bool {
	for key := range rule {
		if !optionKeys[key] {
			return true
		}
	}
	return false
}
