// SPDX-License-Identifier: GPL-3.0-only

package logs

import (
	"regexp"
	"strings"
)

// Redact вычищает секреты из строки лога (LNB-01/28; единая точка
// «log.sanitize» из BUGS.md п.1). Разбор — по СТРОКЕ, не по структуре: лог —
// текст, обломки JSON/YAML/URL встречаются в нём в произвольном контексте.
// Шаги (порядок важен, см. Redact):
//
//  1. URL: userinfo (до ПОСЛЕДНЕГО «@» authority), токены в пути, значения query;
//  2. «GET /путь?query» без схемы (access-логи);
//  3. Authorization: Basic|Bearer;
//  4. «ключ: значение» / «ключ=значение» с секретным именем ключа (JSON-строка,
//     число, bool, массив; YAML; INI; env), затем env-пары и query вне URL;
//  5. голые UUID; 6. содержимое share-ссылок (vmess://, vless://, …) целиком.
//
// Результат идемпотентен, JSON-вход остаётся валидным JSON (GetRuntimeConfig).
const redacted = "[REDACTED]"

// ---- 1. URL ---------------------------------------------------------------

// reURL — URL со схемой; обрывается на пробеле, кавычках, угловых скобках и
// обратной косой (в JSON-тексте за URL идёт «\"»).
var reURL = regexp.MustCompile("(?i)\\b[a-z][a-z0-9+.-]*://[^\\s\"'<>`\\\\]+")

func redactURLs(s string) string { return reURL.ReplaceAllStringFunc(s, redactURL) }

// redactURL разбирает scheme://[userinfo@]host[/path][?query][#fragment]:
// userinfo и значения query скрываются целиком, секретные сегменты пути — по
// отдельности; хост, порт, имена параметров и фрагмент остаются.
func redactURL(u string) string {
	i := strings.Index(u, "://")
	scheme, rest := u[:i+3], u[i+3:]
	frag := ""
	if j := strings.IndexByte(rest, '#'); j >= 0 {
		rest, frag = rest[:j], rest[j:]
	}
	query := ""
	if j := strings.IndexByte(rest, '?'); j >= 0 {
		rest, query = rest[:j], rest[j:]
	}
	userinfo := ""
	if at := userinfoEnd(rest); at >= 0 {
		userinfo, rest = redacted+"@", rest[at+1:]
	}
	host, path := rest, ""
	if j := strings.IndexByte(rest, '/'); j >= 0 {
		host, path = rest[:j], rest[j:]
	}
	return scheme + userinfo + host + redactPath(path) + redactQuery(query) + frag
}

// userinfoEnd — индекс «@», после которого начинается хост, либо -1. Как в
// WHATWG URL, граница userinfo — ПОСЛЕДНИЙ «@» authority (пароль с «@»).
// Запасной случай — пароль со «/» (http://user:pa/ss@host): «@» стоит после
// первого «/», но authority похож на «логин:не-порт». Пути /@user, host:8080/@x
// и IPv6-литералы под это правило не попадают.
func userinfoEnd(rest string) int {
	at := strings.LastIndexByte(rest, '@')
	if at < 0 {
		return -1
	}
	slash := strings.IndexByte(rest, '/')
	if slash < 0 || at < slash {
		return at
	}
	auth := rest[:slash]
	if strings.HasPrefix(auth, "[") {
		return -1
	}
	_, pw, ok := strings.Cut(auth, ":")
	if !ok || pw == "" || isDigits(pw) {
		return -1
	}
	return at
}

func isDigits(s string) bool {
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return s != ""
}

// redactPath скрывает сегменты пути, похожие на токены подписки
// (/sub/8f3a…0718, /api/sub/Zk3Qx9aBcDeF12345, JWT).
func redactPath(path string) string {
	if path == "" {
		return path
	}
	segs := strings.Split(path, "/")
	for i, seg := range segs {
		// Хвостовая пунктуация из прозы («…/sub/abc123.») не часть токена.
		core := strings.TrimRight(seg, ").,;:!?]}")
		if isTokenSegment(core) {
			segs[i] = redacted + seg[len(core):]
		}
	}
	return strings.Join(segs, "/")
}

// isTokenSegment — у сегмента «a.b.c» хотя бы одна часть похожа на токен.
// Точки разделяют части, чтобы JWT и имена файлов с версиями оценивались по частям.
func isTokenSegment(seg string) bool {
	for _, part := range strings.Split(seg, ".") {
		if isTokenPart(part) {
			return true
		}
	}
	return false
}

// isTokenPart — ≥ 16 символов [A-Za-z0-9_=~%+-] и признак случайности: без
// «-»/«_» есть цифра ИЛИ смешаны регистры (hex, base64, 3x-ui subId); с «-»/«_»
// есть цифра, строчные И заглавные (base64url). Слова через дефис
// (lavray-windows-x64-0, subscription-userinfo) под правило не попадают.
func isTokenPart(p string) bool {
	if len(p) < 16 {
		return false
	}
	var digit, lower, upper, sep bool
	for _, r := range p {
		switch {
		case r >= '0' && r <= '9':
			digit = true
		case r >= 'a' && r <= 'z':
			lower = true
		case r >= 'A' && r <= 'Z':
			upper = true
		case r == '-' || r == '_':
			sep = true
		case r == '=' || r == '~' || r == '%' || r == '+':
		default:
			return false
		}
	}
	if sep {
		return digit && lower && upper
	}
	return digit || (lower && upper)
}

// redactQuery скрывает значения ВСЕХ параметров query: у панелей подписок
// секрет бывает под любым именем (t=, sid=, sub=, uuid=). Имена остаются.
func redactQuery(q string) string {
	if len(q) <= 1 {
		return q
	}
	pairs := strings.Split(q[1:], "&")
	for i, p := range pairs {
		if k, v, ok := strings.Cut(p, "="); ok && v != "" && v != redacted {
			pairs[i] = k + "=" + redacted
		}
	}
	return "?" + strings.Join(pairs, "&")
}

// ---- 2. «GET /путь?query» без схемы ----------------------------------------

var reRequestLine = regexp.MustCompile("\\b(GET|POST|PUT|PATCH|DELETE|HEAD|OPTIONS)(\\s+)(/[^\\s\"'<>`\\\\]*)")

func redactRequestLines(s string) string {
	return reRequestLine.ReplaceAllStringFunc(s, func(m string) string {
		sub := reRequestLine.FindStringSubmatch(m)
		path, query := sub[3], ""
		if j := strings.IndexByte(path, '?'); j >= 0 {
			path, query = path[:j], path[j:]
		}
		return sub[1] + sub[2] + redactPath(path) + redactQuery(query)
	})
}

// ---- 3. заголовки, env, query вне URL --------------------------------------

// Authorization (Basic/Bearer) — токен целиком; допускает JSON-вид "Authorization":"Bearer …".
var reAuthHeader = regexp.MustCompile(`(?i)(authorization["']?\s*[:=]\s*["']?(?:basic|bearer)\s+)[^\s"']+`)

// env-пары: PASSWORD=value / SECRET_TOKEN=value (LNB-28, env внешних ядер).
var reEnvSecret = regexp.MustCompile(`(?i)(\b[A-Z0-9_]*(?:PASSWORD|PASSWD|TOKEN|SECRET|PRIVATE_KEY|PRIVATEKEY)[A-Z0-9_]*=)([^\s]*)`)

// Секретные query-параметры вне полного URL («?token=…», «&key=…»). «key»
// ловится и внутри составных имён (api_key): перередакция безопаснее пропуска.
var reQuerySecret = regexp.MustCompile(`(?i)([?&](?:[a-z0-9_.-]*` + `(?:token|secret|password|passwd|pwd|key|auth|uuid|psk|sid)` + `[a-z0-9_.-]*)=)([^&#\s]*)`)

// ---- 4. «ключ: значение» / «ключ=значение» ---------------------------------

// reKV: [кавычка]ключ[кавычка] разделитель значение; значение — строка в
// двойных (с экранированием) или одинарных кавычках, массив [...] либо «голое»
// слово без пробелов и разделителей. Объект {...} как значение не берётся:
// его поля обрабатываются по отдельности.
var reKV = regexp.MustCompile(`(?i)(["']?)([a-z0-9_][a-z0-9_.-]*)(["']?)([ \t]*[:=][ \t]*)("(?:[^"\\]|\\.)*"|'(?:[^'\\]|\\.)*'|\[[^\]]*\]|[^\s,;&{}\[\]"']+)`)

// secretKeyStems — основы, секретные как подстрока ЛЮБОГО места имени ключа
// (obfs-password, private_key_passphrase, SUB_TOKEN, api-key).
var secretKeyStems = []string{
	"password", "passwd", "passphrase", "pwd", "token", "secret", "uuid",
	"private_key", "private-key", "privatekey",
	"pre_shared_key", "pre-shared-key", "preshared", "psk",
	"credential", "short_id", "short-id", "shortid",
	"api_key", "api-key", "apikey", "access_key", "access-key", "accesskey",
}

// secretKeyWords — короткие основы: секретны только как ЦЕЛЫЙ сегмент имени
// (разделители «.», «_», «-»), чтобы не задеть bypass, author, passed и т.п.
var secretKeyWords = map[string]bool{"pass": true, "auth": true, "obfs": true, "sid": true}

func isSecretKey(name string) bool {
	n := strings.ToLower(name)
	for _, stem := range secretKeyStems {
		if strings.Contains(n, stem) {
			return true
		}
	}
	for _, seg := range strings.FieldsFunc(n, func(r rune) bool { return r == '.' || r == '_' || r == '-' }) {
		if secretKeyWords[seg] {
			return true
		}
	}
	return false
}

// redactKV скрывает значения секретных ключей. Несекретная пара не «съедает»
// текст: сканирование продолжается с начала её значения, поэтому в
// «a= password=x» вторая пара находится.
func redactKV(s string) string {
	var b strings.Builder
	pos := 0
	for pos < len(s) {
		loc := reKV.FindStringSubmatchIndex(s[pos:])
		if loc == nil {
			break
		}
		// Абсолютные границы: ключ — группа 2, значение — группа 5.
		keyStart, keyEnd := pos+loc[4], pos+loc[5]
		valStart, valEnd := pos+loc[10], pos+loc[11]
		if !isSecretKey(s[keyStart:keyEnd]) {
			b.WriteString(s[pos:valStart])
			pos = valStart
			continue
		}
		quotedKey := loc[3] > loc[2] && s[pos+loc[2]] == '"'
		val := s[valStart:valEnd]
		masked := redacted
		switch {
		case quotedKey || strings.HasPrefix(val, `"`):
			masked = `"` + redacted + `"` // JSON остаётся валидным
		case strings.HasPrefix(val, `'`):
			masked = `'` + redacted + `'`
		}
		b.WriteString(s[pos:valStart])
		b.WriteString(masked)
		pos = valEnd
	}
	b.WriteString(s[pos:])
	return b.String()
}

// ---- 5. голые UUID, 6. share/happ-ссылки -------------------------------------

var reUUID = regexp.MustCompile(`(?i)\b[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}\b`)

// Содержимое после схемы скрывается ЦЕЛИКОМ (внутри base64/happ — пароли и
// UUID, LNB-01).
var reShareLinks = regexp.MustCompile(`(?i)\b(vmess|vless|trojan|ss|hysteria2?|hy2|tuic|happ|nekoray|sn)://[^\s#]*`)

// Redact применяет все правила; секретных вхождений в результате нет.
func Redact(s string) string {
	if s == "" {
		return s
	}
	s = redactURLs(s)
	s = redactRequestLines(s)
	s = reAuthHeader.ReplaceAllString(s, `$1`+redacted)
	s = redactKV(s)
	s = reEnvSecret.ReplaceAllString(s, `$1`+redacted)
	s = reQuerySecret.ReplaceAllString(s, `${1}`+redacted)
	s = reUUID.ReplaceAllString(s, redacted)
	s = reShareLinks.ReplaceAllStringFunc(s, func(m string) string {
		i := strings.Index(m, "://")
		if i < 0 {
			return m
		}
		return m[:i+3] + redacted
	})
	return s
}

// ansiEscape — CSI/OSC-последовательности (FR-UI-218: VT100-чистка в ядре).
var ansiEscape = regexp.MustCompile(`\x1b(?:\[[0-9;?]*[a-zA-Z]|\][^\x07\x1b]*(?:\x07|\x1b\\)|[@-Z\\-_])`)

// StripANSI удаляет ANSI/VT100-последовательности (цвета внешних ядер и пр.).
func StripANSI(s string) string {
	return ansiEscape.ReplaceAllString(s, "")
}
