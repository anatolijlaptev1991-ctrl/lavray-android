// SPDX-License-Identifier: GPL-3.0-only

package logs

import (
	"regexp"
	"strings"
)

// reMAC — кандидат в MAC-адрес: шесть байтов по две hex-цифры через «:» или «-».
var reMAC = regexp.MustCompile(`(?i)[0-9a-f]{2}(?:[:-][0-9a-f]{2}){5}`)

// MaskMACs скрывает в строке журнала MAC-адреса, оставляя первые два байта:
//
//	gateway_mac 50:88:11:22:33:44  ->  gateway_mac 50:88:**:**:**:**
//
// После T-33 в журнал ядра попадают строки sing-box вида «network: updated
// network environment: gateway 192.168.31.1, gateway_mac 50:88:…». MAC шлюза
// и устройств сети идентифицирует оборудование пользователя, а журнал
// выгружают в отчёты об ошибках, поэтому он не должен храниться и отдаваться
// целиком. Остальной текст не меняется: адрес принимается, только если это ровно
// шесть байтов с одним типом разделителя и вокруг нет других hex-цифр,
// «:» или «-» (иначе это кусок IPv6-адреса, EUI-64, идентификатора). Повторный
// вызов ничего не меняет.
func MaskMACs(s string) string {
	if len(s) < 17 || !strings.ContainsAny(s, ":-") {
		return s
	}
	locs := reMAC.FindAllStringIndex(s, -1)
	if locs == nil {
		return s
	}
	var b strings.Builder
	last := 0
	for _, loc := range locs {
		start, end := loc[0], loc[1]
		mac := s[start:end]
		sep := mac[2]
		if strings.Count(mac, string(sep)) != 5 || !macBoundary(s, start, end) {
			continue
		}
		b.WriteString(s[last:start])
		b.WriteString(mac[:5])
		for i := 0; i < 4; i++ {
			b.WriteByte(sep)
			b.WriteString("**")
		}
		last = end
	}
	if last == 0 {
		return s
	}
	b.WriteString(s[last:])
	return b.String()
}

// macBoundary — перед и после кандидата нет hex-цифры, «:» или «-»: он не часть
// более длинной последовательности (IPv6, EUI-64, UUID).
func macBoundary(s string, start, end int) bool {
	return (start == 0 || !macContinues(s[start-1])) && (end == len(s) || !macContinues(s[end]))
}

func macContinues(c byte) bool {
	return c == ':' || c == '-' || (c >= '0' && c <= '9') || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F')
}
