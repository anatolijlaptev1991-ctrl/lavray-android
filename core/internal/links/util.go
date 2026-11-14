// SPDX-License-Identifier: GPL-3.0-only

package links

import (
	"encoding/base64"
	"fmt"
	"net"
	"net/url"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

// sanitizeString приводит %-декодированные байты к корректному UTF-8
// (недопустимые последовательности → U+FFFD) — как QString::fromUtf8 в Qt;
// стабилизирует длину при повторной JSON-сериализации bean.
func sanitizeString(s string) string {
	if utf8.ValidString(s) {
		return s
	}
	return strings.ToValidUTF8(s, "�")
}

// fragName — имя профиля из fragment ссылки.
func fragName(u *url.URL) string { return sanitizeString(u.Fragment) }

// userInfo — логин/пароль из userinfo (password в URL-кодировке уже
// декодирован net/url).
func userInfo(u *url.URL) (user, pass string, hasPass bool) {
	user = sanitizeString(u.User.Username())
	if u.User != nil {
		pass, hasPass = u.User.Password()
		pass = sanitizeString(pass)
	}
	return
}

// Лимиты входа (LNB-12/17 — защита от гигантских значений ещё до парсинга).
const (
	maxLinkLength   = 64 << 10 // 64 КиБ на всю ссылку
	maxFieldLength  = 1024     // пароли, логины, uuid, sni, метод, отпечаток…
	maxRemarkLength = 4096     // имя/замечание профиля
	maxPluginLength = 2048     // plugin-строка ss
)

// Белые списки значений (Preset.hpp; LNB-17 — ошибка импорта вместо ошибки
// старта ядра).
var (
	ssMethods = map[string]bool{
		"2022-blake3-aes-128-gcm":       true,
		"2022-blake3-aes-256-gcm":       true,
		"2022-blake3-chacha20-poly1305": true,
		"none":                          true,
		"aes-128-gcm":                   true,
		"aes-192-gcm":                   true,
		"aes-256-gcm":                   true,
		"chacha20-ietf-poly1305":        true,
		"xchacha20-ietf-poly1305":       true,
		"aes-128-ctr":                   true,
		"aes-192-ctr":                   true,
		"aes-256-ctr":                   true,
		"aes-128-cfb":                   true,
		"aes-192-cfb":                   true,
		"aes-256-cfb":                   true,
		"rc4-md5":                       true,
		"chacha20-ietf":                 true,
		"xchacha20":                     true,
	}
	vmessSecurities = map[string]bool{
		"auto": true, "none": true, "zero": true,
		"aes-128-gcm": true, "chacha20-poly1305": true,
	}
	vlessFlows = map[string]bool{"xtls-rprx-vision": true}
	utlsFPs    = map[string]bool{
		"": true, "chrome": true, "firefox": true, "edge": true, "safari": true,
		"360": true, "qq": true, "ios": true, "android": true,
		"random": true, "randomized": true,
	}
	tuicCongestions = map[string]bool{"": true, "bbr": true, "cubic": true, "new_reno": true}
	tuicRelayModes  = map[string]bool{"": true, "native": true, "quic": true}
)

// isHexDigit нужен ручной проверке hex-компонентов.
func isHexDigit(c byte) bool {
	return (c >= '0' && c <= '9') || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F')
}

// percentEncodeAll экранирует всё, кроме unreserved (RFC 3986 §2.3:
// ALPHA / DIGIT / "-" / "." / "_" / "~"). Аналог Qt QUrl::toPercentEncoding —
// в частности, пробел кодируется как %20, "+" не используется.
func percentEncodeAll(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' ||
			c == '-' || c == '.' || c == '_' || c == '~' {
			b.WriteByte(c)
		} else {
			b.WriteByte('%')
			const hex = "0123456789ABCDEF"
			b.WriteByte(hex[c>>4])
			b.WriteByte(hex[c&0xF])
		}
	}
	return b.String()
}

// queryPair — параметр query в исходном порядке.
type queryPair struct{ key, value string }

// parseQuery разбирает RawQuery вручную: %XX декодируется (url.PathUnescape —
// без «+»→пробел, как QUrlQuery), порядок сохраняется, первое вхождение
// ключа выигрывает (queryItemValue Qt).
func parseQuery(rawQuery string) (pairs []queryPair, get func(key string) (string, bool)) {
	seen := map[string]string{}
	if rawQuery != "" {
		for _, part := range strings.Split(rawQuery, "&") {
			if part == "" {
				continue
			}
			k, v, _ := strings.Cut(part, "=")
			ku, err := url.PathUnescape(k)
			if err != nil {
				ku = k
			}
			vu, err := url.PathUnescape(v)
			if err != nil {
				vu = v // битый escape в значении — оставляем как есть
			}
			vu = sanitizeString(vu)
			pairs = append(pairs, queryPair{ku, vu})
			if _, dup := seen[ku]; !dup {
				seen[ku] = vu
			}
		}
	}
	return pairs, func(key string) (string, bool) {
		v, ok := seen[key]
		return v, ok
	}
}

// decodeB64Strict — строгий padded base64 (std или url-алфавит). Qt
// AbortOnBase64DecodingErrors-семантика; непаддированный вход отвергается
// (deviations.yaml: «некорректный padding — LAVRay отвергает»).
func decodeB64Strict(s string) ([]byte, bool) {
	if strings.ContainsAny(s, "\r\n") || s == "" {
		return nil, false
	}
	if b, err := base64.StdEncoding.DecodeString(s); err == nil {
		return b, true
	}
	if b, err := base64.URLEncoding.DecodeString(s); err == nil {
		return b, true
	}
	return nil, false
}

// decodeB64Any — padded или raw base64 (std/url). Для neko-ссылок, где
// сторонние Go-клиенты нередко не пишут padding.
func decodeB64Any(s string) ([]byte, bool) {
	if b, ok := decodeB64Strict(s); ok {
		return b, true
	}
	if b, err := base64.RawStdEncoding.DecodeString(s); err == nil {
		return b, true
	}
	if b, err := base64.RawURLEncoding.DecodeString(s); err == nil {
		return b, true
	}
	return nil, false
}

// canonicalHost канонизирует хост: IPv6-литерал (возможно, в квадратных
// скобках — поле add в vmess-JSON) — в форму net.IP.String() (нижний регистр,
// сжатые нули; LNB-15/17-канонизация), домен — как есть (регистр хранится в
// модели, FR-UI-083 вырезает только пробелы).
func canonicalHost(h string) string {
	inner := h
	if strings.HasPrefix(h, "[") && strings.HasSuffix(h, "]") {
		inner = h[1 : len(h)-1]
	}
	if ip := net.ParseIP(inner); ip != nil {
		return ip.String()
	}
	return h
}

// isIPAddr — хост является IP-литералом (v4/v6).
func isIPAddr(h string) bool {
	return net.ParseIP(h) != nil
}

// validPort — порт в диапазоне 1..65535 (LNB-15).
func validPort(p int) bool {
	return p >= 1 && p <= 65535
}

// parsePortStr разбирает порт из строки URL; пусто → 0, ok (вызовет дефолт).
func parsePortStr(s string) (int, error) {
	if s == "" {
		return 0, nil
	}
	n, err := strconv.Atoi(s)
	if err != nil {
		return 0, fmt.Errorf("порт %q не является числом", s)
	}
	return n, nil
}

// normalizeALPN — тримминг элементов через запятую, пустые отбрасываются
// (канонизация §4.1).
func normalizeALPN(s string) string {
	parts := strings.Split(s, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if t := strings.TrimSpace(p); t != "" {
			out = append(out, t)
		}
	}
	return strings.Join(out, ",")
}

// normalizeSecurityNone разрешает security-значения транспорта socks/http:
// none/0/false/пусто → "", 1/true/tls → "tls", иначе ошибка (§4.1).
func normalizeSecurityNone(v string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "", "none", "0", "false":
		return "", nil
	case "1", "true", "tls":
		return "tls", nil
	default:
		return "", fmt.Errorf("неподдерживаемое значение security %q (допустимо: tls/none)", v)
	}
}

// normalizeSecurityTLS разрешает security-значения vless/trojan/vmess:
// none/0/false/пусто → "", 1/true/tls → "tls", reality → "reality"
// (канонизируется в реальность через pbk на уровне модели), иначе ошибка.
func normalizeSecurityTLS(v string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "", "none", "0", "false":
		return "", nil
	case "1", "true", "tls":
		return "tls", nil
	case "reality":
		return "reality", nil
	default:
		return "", fmt.Errorf("неподдерживаемое значение security %q (допустимо: tls/reality/none)", v)
	}
}

// parseBoolParam — bool-параметр ссылки (allowInsecure/insecure/
// allow_insecure): строго 1/true → true; 0/false/пусто и любое другое
// значение (yes, мусор) → false. Строчнее oracle-«непусто→true» (L2-2
// ревью: поблажка включала небезопасный режим там, где oracle не включил
// бы; задокументировано в doc.go).
func parseBoolParam(v string) bool {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "1", "true":
		return true
	default:
		return false
	}
}

// validateDuration — строка длительности sing-box (hop_interval, heartbeat).
func validateDuration(field, s string) error {
	if _, err := time.ParseDuration(s); err != nil {
		return fmt.Errorf("%s: некорректная длительность %q (пример: 30s)", field, s)
	}
	return nil
}

// validateServerPorts — строка «a:b» / список портов hy2 (mport): только
// цифры, «:», «-», «,»; каждая числовая компонента в 1..65535 (LNB-15).
func validateServerPorts(s string) error {
	if s == "" {
		return nil
	}
	token := strings.Builder{}
	flush := func() error {
		if token.Len() == 0 {
			return fmt.Errorf("mport: пустая компонента в %q", s)
		}
		n, err := strconv.Atoi(token.String())
		if err != nil || !validPort(n) {
			return fmt.Errorf("mport: порт %q вне диапазона 1–65535", token.String())
		}
		token.Reset()
		return nil
	}
	for i := 0; i < len(s); i++ {
		switch c := s[i]; {
		case c >= '0' && c <= '9':
			token.WriteByte(c)
		case c == ':' || c == '-' || c == ',':
			if err := flush(); err != nil {
				return err
			}
		default:
			return fmt.Errorf("mport: недопустимый символ %q в %q", string(rune(c)), s)
		}
	}
	return flush()
}

// isHex нужен проверке hex-строк (sid и пр. не проверяем строго — пропускаем).
func isHex(s string) bool {
	if s == "" {
		return false
	}
	for i := 0; i < len(s); i++ {
		if !isHexDigit(s[i]) {
			return false
		}
	}
	return true
}

// checkFieldLen — лимит длины строкового поля (LNB-17: длина секрета).
func checkFieldLen(scheme, field, v string, max int) error {
	if len(v) > max {
		return fmt.Errorf("%s: поле %s длиннее %d байт", scheme, field, max)
	}
	return nil
}
