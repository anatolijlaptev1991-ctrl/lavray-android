// SPDX-License-Identifier: GPL-3.0-only

package mobile

// patch_sniff.go — потолок тайм-аута sniff-действия маршрута на телефоне.
//
// cfgbuild ядра пишет в route.rules {"action":"sniff","timeout":"3s"}. sing-box
// (common/sniff.PeekStream) ждёт первую порцию данных от приложения до этого тайм-аута,
// а потом маршрутизирует соединение. Клиентские протоколы (TLS, HTTP) присылают её за
// миллисекунды, поэтому тайм-аут их не задерживает. Серверные (баннер сначала: FTP, MySQL,
// SMTP на нестандартном порту, многие игровые рукопожатия) ничего не шлют, и каждое такое
// соединение стояло бы 3 с до начала маршрутизации. Первое чтение локальное (приложение →
// tun → gVisor), сети в нём нет, ждать дольше 0,5 с нечего.
//
// 500 мс вдвое больше умолчания sing-box (C.ReadPayloadTimeout, 300 мс): запас для
// приложения, которое пишет первый байт не сразу после connect. Меньше 300 мс брать не
// стоит: это граница, на которой sing-box сам считает ожидание разумным. Значения не
// больше потолка, пустые и непонятные не трогаются: правило не удлиняется и не
// скрывает опечатку пользователя (sing-box отвергнет её сам).

import "time"

// maxSniffTimeout — наибольший тайм-аут sniff-действия на телефоне.
const maxSniffTimeout = 500 * time.Millisecond

// patchSniff снижает тайм-аут верхнеуровневых правил route с action=sniff до
// maxSniffTimeout. Вложенные логические правила действий не несут, их не обходим.
func patchSniff(root map[string]any) {
	route, ok := root["route"].(map[string]any)
	if !ok {
		return
	}
	rules, ok := route["rules"].([]any)
	if !ok {
		return
	}
	for _, item := range rules {
		rule, ok := item.(map[string]any)
		if !ok {
			continue
		}
		if action, _ := rule["action"].(string); action != "sniff" {
			continue
		}
		raw, ok := rule["timeout"].(string)
		if !ok {
			continue
		}
		d, err := time.ParseDuration(raw)
		if err != nil || d <= maxSniffTimeout {
			continue
		}
		rule["timeout"] = maxSniffTimeout.String()
	}
}
