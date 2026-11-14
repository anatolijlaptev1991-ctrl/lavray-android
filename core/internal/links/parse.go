// SPDX-License-Identifier: GPL-3.0-only

package links

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strconv"
	"strings"

	"github.com/anatolijlaptev1991-ctrl/lavray-android/core/internal/model"
)

// ParseLink разбирает share-ссылку в профиль (Link2Bean). Схема определяется
// по префиксу «scheme://» (lowercase). Ошибки — человекочитаемые, привязаны
// к полю; паник нет (инвариант FuzzParseLink).
func ParseLink(raw string) (*model.Profile, error) {
	s := strings.TrimSpace(raw)
	if s == "" {
		return nil, errors.New("ссылка пуста")
	}
	if len(s) > maxLinkLength {
		return nil, fmt.Errorf("ссылка длиннее %d байт", maxLinkLength)
	}
	i := strings.Index(s, "://")
	if i <= 0 {
		return nil, fmt.Errorf("схема не распознана (ожидается «протокол://…», например vmess://): %s", truncate(s, 64))
	}
	scheme := strings.ToLower(s[:i])
	// 0.2.1: хвост «?serverDescription=<base64>» в имени — служебный (description.go).
	s, description := splitLinkDescription(s)
	p, err := parseScheme(scheme, s)
	if err != nil {
		return nil, err
	}
	applyServerDescription(p, description)
	return p, nil
}

// parseScheme разбирает ссылку по схеме (scheme в нижнем регистре).
func parseScheme(scheme, s string) (*model.Profile, error) {
	switch scheme {
	case "ss":
		return parseSS(s)
	case "vmess":
		return parseVMess(s)
	case "vless":
		return parseTrojanVLESS(scheme, s)
	case "trojan":
		return parseTrojanVLESS(scheme, s)
	case "hy2", "hysteria2":
		return parseHysteria2(scheme, s)
	case "tuic":
		return parseTUIC(s)
	case "socks", "socks4", "socks4a", "socks5":
		return parseSocksHTTP(scheme, s)
	case "http", "https":
		return parseSocksHTTP(scheme, s)
	case "nekoray":
		return parseNekoLink(s)
	case "happ":
		return nil, errors.New("зашифрованная ссылка — это подписка: добавьте её в «Подписки»")
	default:
		return nil, fmt.Errorf("неподдерживаемая схема %q (поддерживаются: ss, vmess, vless, trojan, hy2/hysteria2, tuic, socks, http/https, nekoray)", scheme)
	}
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

// parseURL — общий разбор URL-подобной ссылки.
func parseURL(scheme, s string) (*url.URL, []queryPair, func(string) (string, bool), error) {
	u, err := url.Parse(s)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("%s: некорректный URL: %v", scheme, err)
	}
	pairs, get := parseQuery(u.RawQuery)
	return u, pairs, get, nil
}

// hostPort извлекает и канонизирует хост; present=false — порт в ссылке
// отсутствует (случай, отличный от явного «:0»).
func hostPort(scheme string, u *url.URL) (host string, port int, present bool, err error) {
	// L2-1 (ревью): зона IPv6 (fe80::1%eth0) не поддерживается — net.ParseIP
	// её не канонизирует, а экспорт без скобок ломал бы roundtrip-инвариант.
	if h := u.Hostname(); strings.Contains(h, "%") {
		return "", 0, false, fmt.Errorf("%s: зона IPv6 (%%25…) в адресе не поддерживается", scheme)
	}
	host = canonicalHost(u.Hostname())
	if host == "" {
		return "", 0, false, fmt.Errorf("%s: не указан адрес сервера", scheme)
	}
	if err := checkFieldLen(scheme, "адрес", host, maxFieldLength); err != nil {
		return "", 0, false, err
	}
	if u.Port() == "" {
		return host, 0, false, nil
	}
	port, err = parsePortStr(u.Port())
	if err != nil {
		return "", 0, false, fmt.Errorf("%s: %v", scheme, err)
	}
	return host, port, true, nil
}

// applyDefaultPort применяет дефолт ровно один раз (LNB-15: в oracle дефолт
// присваивался и немедленно перетирался реальным портом, а явный «:0»
// проходил в конфиг). Отсутствующий порт → дефолт; явный порт обязан быть
// в 1..65535.
func applyDefaultPort(scheme string, port int, present bool, def int) (int, error) {
	if !present {
		return def, nil
	}
	if !validPort(port) {
		return 0, fmt.Errorf("%s: порт %d вне диапазона 1–65535", scheme, port)
	}
	return port, nil
}

// requirePort — порт обязателен (ss, vmess-JSON): дефолта нет.
func requirePort(scheme string, port int, present bool) (int, error) {
	if !present {
		return 0, fmt.Errorf("%s: порт не указан", scheme)
	}
	if !validPort(port) {
		return 0, fmt.Errorf("%s: порт %d вне диапазона 1–65535", scheme, port)
	}
	return port, nil
}

// sniFromHost — канонизация §4.1: «SNI из host при IP» — если адрес
// IP-литерал, SNI пуст, TLS включён и есть Host транспорта, SNI := Host.
func sniFromHost(p *model.Profile) {
	st := p.Stream
	if st == nil || st.Host == "" || st.SNI != "" {
		return
	}
	if st.Security != "tls" && st.Security != "reality" {
		return
	}
	if isIPAddr(p.Address) {
		st.SNI = st.Host
	}
}

// ---- socks / http ----

func parseSocksHTTP(scheme, s string) (*model.Profile, error) {
	u, _, get, err := parseURL(scheme, s)
	if err != nil {
		return nil, err
	}
	p := &model.Profile{}
	switch scheme {
	case "http":
		p.Type = model.TypeHTTP
		p.Detail = &model.SocksHTTPDetail{Proto: "http"}
	case "https":
		p.Type = model.TypeHTTP
		p.Detail = &model.SocksHTTPDetail{Proto: "http"}
	default:
		p.Type = model.TypeSOCKS
		proto := "socks5"
		if scheme == "socks4" || scheme == "socks4a" {
			proto = "socks4"
		}
		p.Detail = &model.SocksHTTPDetail{Proto: proto}
	}
	d := p.Detail.(*model.SocksHTTPDetail)

	p.Name = fragName(u)
	host, port, present, err := hostPort(scheme, u)
	if err != nil {
		return nil, err
	}
	p.Address = host
	def := 1080
	if scheme == "http" {
		def = 80 // отклонение от oracle (443) — см. doc.go
	} else if scheme == "https" {
		def = 443
	}
	if p.Port, err = applyDefaultPort(scheme, port, present, def); err != nil {
		return nil, err
	}

	d.Username, d.Password, _ = userInfo(u)
	// v2rayN-формат socks: пароля нет, логин — base64 «user:pass»
	// (SocksHttpBean::TryParseLink). Усиление: декодируем только если
	// результат — корректный UTF-8 с «:», иначе логин оставляем как есть.
	if d.Password == "" && d.Username != "" {
		if b, ok := decodeB64Strict(d.Username); ok {
			if dec := string(b); strings.Contains(dec, ":") {
				u1, p1, _ := strings.Cut(dec, ":")
				d.Username, d.Password = u1, p1
			}
		}
	}
	if err := checkFieldLen(scheme, "username", d.Username, maxFieldLength); err != nil {
		return nil, err
	}
	if err := checkFieldLen(scheme, "password", d.Password, maxFieldLength); err != nil {
		return nil, err
	}

	// stream: security + sni (их читает ещё NekoBox; LAVRay и экспортирует).
	st := &model.StreamSettings{}
	if v, ok := get("security"); ok {
		sec, err := normalizeSecurityNone(v)
		if err != nil {
			return nil, fmt.Errorf("%s: %v", scheme, err)
		}
		st.Security = sec
	}
	if scheme == "https" {
		st.Security = "tls"
	}
	if v, ok := get("sni"); ok {
		st.SNI = strings.TrimSpace(v)
	}
	if *st != (model.StreamSettings{}) {
		p.Stream = st
	}
	if err := checkFieldLen(scheme, "имя", p.Name, maxRemarkLength); err != nil {
		return nil, err
	}
	return p, nil
}

// applyStreamFromQuery заполняет поля StreamSettings из параметров типа
// (type/security/sni/peer/alpn/fp/pbk/sid/spx/allowInsecure + сетевые).
// defSecurity — дефолт для ОТСУТСТВУЮЩЕГО параметра security
// (trojan/vmess-URL: «tls», vless: «»). Присутствующий-пустой → «»
// (отклонение от oracle — см. doc.go).
func applyStreamFromQuery(st *model.StreamSettings, get func(string) (string, bool), defSecurity string) error {
	// сеть
	netw := "tcp"
	if v, ok := get("type"); ok {
		netw = strings.ToLower(strings.TrimSpace(v))
	}
	if netw == "" {
		netw = "tcp"
	}
	netw = canonicalNetwork(netw)
	if !networks[netw] {
		return fmt.Errorf("тип транспорта %q не поддерживается (tcp, ws, grpc, http, httpupgrade, quic, xhttp)", netw)
	}
	st.Network = netw

	// security
	sec, err := normalizeSecurityTLS(defSecurity)
	if err != nil {
		return fmt.Errorf("дефолт security %q: %v", defSecurity, err)
	}
	if v, ok := get("security"); ok {
		if sec, err = normalizeSecurityTLS(v); err != nil {
			return err
		}
	}

	if v, ok := get("sni"); ok && strings.TrimSpace(v) != "" {
		st.SNI = strings.TrimSpace(v)
	} else if v, ok := get("peer"); ok && strings.TrimSpace(v) != "" {
		st.SNI = strings.TrimSpace(v)
	}
	if v, ok := get("alpn"); ok {
		st.ALPN = normalizeALPN(v)
	}
	if v, ok := get("allowInsecure"); ok {
		st.AllowInsecure = parseBoolParam(v)
	}
	if v, ok := get("fp"); ok {
		fp := strings.TrimSpace(v)
		if !utlsFPs[fp] {
			return fmt.Errorf("отпечаток uTLS %q не поддерживается", fp)
		}
		st.UTLSFingerprint = fp
	}
	if v, ok := get("pbk"); ok {
		st.RealityPublicKey = v
	}
	if v, ok := get("sid"); ok {
		st.RealityShortID = v
	}
	if v, ok := get("spx"); ok {
		st.RealitySPX = v
	}

	// канонизация REALITY: pbk непусто ⇒ security=reality, иначе sec как есть
	if st.RealityPublicKey != "" {
		st.Security = "reality"
	} else {
		st.Security = sec
	}

	// поля сети
	switch st.Network {
	case "ws", "http", "httpupgrade", "xhttp":
		applyXHTTPQuery(st, get)
		if v, ok := get("path"); ok {
			st.Path = v
		}
		if v, ok := get("host"); ok {
			if st.Network == "http" {
				st.Host = strings.ReplaceAll(v, "|", ",")
			} else {
				st.Host = v
			}
		}
	case "grpc":
		if v, ok := get("serviceName"); ok {
			st.Path = v
		}
	case "tcp":
		if v, ok := get("headerType"); ok && v == "http" {
			st.HeaderType = "http"
			if v, ok := get("path"); ok {
				st.Path = v
			}
			if v, ok := get("host"); ok {
				st.Host = v
			}
		}
	}
	return nil
}

func parseTrojanVLESS(scheme, s string) (*model.Profile, error) {
	u, _, get, err := parseURL(scheme, s)
	if err != nil {
		return nil, err
	}
	typ := model.TypeVLESS
	if scheme == "trojan" {
		typ = model.TypeTrojan
	}
	d := &model.TrojanVlessDetail{}
	p := &model.Profile{Type: typ, Name: fragName(u), Detail: d}

	d.Password, _, _ = userInfo(u)
	if err := checkFieldLen(scheme, "пароль", d.Password, maxFieldLength); err != nil {
		return nil, err
	}
	if d.Password == "" {
		if typ == model.TypeVLESS {
			return nil, fmt.Errorf("%s: не указан UUID", scheme)
		}
		return nil, fmt.Errorf("%s: не указан пароль", scheme)
	}

	host, port, present, err := hostPort(scheme, u)
	if err != nil {
		return nil, err
	}
	p.Address = host
	defSec := ""
	if scheme == "trojan" {
		defSec = "tls"
	}
	if p.Port, err = applyDefaultPort(scheme, port, present, 443); err != nil {
		return nil, err
	}

	st := &model.StreamSettings{}
	if err := applyStreamFromQuery(st, get, defSec); err != nil {
		return nil, fmt.Errorf("%s: %v", scheme, err)
	}
	if typ == model.TypeVLESS {
		if v, ok := get("flow"); ok && v != "" && !vlessFlows[v] {
			return nil, fmt.Errorf("%s: flow %q не поддерживается (допустимо: xtls-rprx-vision)", scheme, v)
		} else if ok {
			d.Flow = v
		}
	}
	if err := validateStreamCaps(scheme, st); err != nil {
		return nil, err
	}
	if *st != (model.StreamSettings{}) {
		p.Stream = st
	}
	sniFromHost(p)
	if err := checkFieldLen(scheme, "имя", p.Name, maxRemarkLength); err != nil {
		return nil, err
	}
	return p, nil
}

// ---- vmess ----

// vmessJSON — v2rayN-формат (FR-UI-164, NekoBox old_share_link_format).
type vmessJSON struct {
	V    any    `json:"v"`
	Ps   string `json:"ps"`
	Add  string `json:"add"`
	Port any    `json:"port"`
	ID   string `json:"id"`
	Aid  any    `json:"aid"`
	Net  string `json:"net"`
	Host string `json:"host"`
	Path string `json:"path"`
	Type string `json:"type"`
	Scy  string `json:"scy"`
	Tls  string `json:"tls"`
	Sni  string `json:"sni"`
	ALPN string `json:"alpn"` // расширение (v2rayN пишет; NekoBox игнорировал)
	Fp   string `json:"fp"`
	Pbk  string `json:"pbk"`
	Sid  string `json:"sid"`
	Spx  string `json:"spx"`
}

// anyToInt — порт/aid приходят числом ИЛИ строкой (разные клиенты); LNB-17:
// мусор отвергается, а не превращается в 0.
func anyToInt(scheme, field string, v any) (int, error) {
	switch x := v.(type) {
	case nil:
		return 0, fmt.Errorf("%s: поле %s отсутствует", scheme, field)
	case float64:
		return int(x), nil
	case string:
		if x == "" {
			// L2-3 (ревью): пустая строка aid — не мусор (реальные генераторы
			// пишут "aid":""); oracle toInt("") даёт 0.
			return 0, nil
		}
		n, err := strconv.Atoi(strings.TrimSpace(x))
		if err != nil {
			return 0, fmt.Errorf("%s: поле %s = %q не является числом", scheme, field, x)
		}
		return n, nil
	case json.Number:
		n, err := x.Int64()
		if err != nil {
			return 0, fmt.Errorf("%s: поле %s = %q не является числом", scheme, field, x.String())
		}
		return int(n), nil
	case bool:
		return 0, fmt.Errorf("%s: поле %s = %v не является числом", scheme, field, x)
	default:
		return 0, fmt.Errorf("%s: поле %s имеет неверный тип", scheme, field)
	}
}

func parseVMess(s string) (*model.Profile, error) {
	const scheme = "vmess"
	rest := s[len("vmess://"):]

	// v2rayN: весь хвост — строгий base64 JSON («#» в строгом base64
	// невозможен — ссылка с замечанием уходит в URL-ветку, как в oracle).
	if b, ok := decodeB64Strict(rest); ok {
		var j vmessJSON
		if err := json.Unmarshal(b, &j); err != nil {
			return nil, fmt.Errorf("vmess: base64-тело не является корректным JSON: %v", err)
		}
		return vmessFromJSON(&j)
	}

	// URL-формат (XTLS#716): uuid@host:port?encryption=…&security=…
	u, _, get, err := parseURL(scheme, s)
	if err != nil {
		return nil, err
	}
	d := &model.VMessDetail{AlterID: 0} // «此分享标准仅针对 VMess AEAD и VLESS»
	p := &model.Profile{Type: model.TypeVMess, Name: fragName(u), Detail: d}

	d.UUID, _, _ = userInfo(u)
	if err := checkFieldLen(scheme, "uuid", d.UUID, maxFieldLength); err != nil {
		return nil, err
	}
	if d.UUID == "" {
		return nil, errors.New("vmess: не указан UUID")
	}
	host, port, present, err := hostPort(scheme, u)
	if err != nil {
		return nil, err
	}
	p.Address = host
	if p.Port, err = applyDefaultPort(scheme, port, present, 443); err != nil {
		return nil, err
	}

	scy := "auto"
	if v, ok := get("encryption"); ok && strings.TrimSpace(v) != "" {
		scy = strings.ToLower(strings.TrimSpace(v))
	}
	if !vmessSecurities[scy] {
		return nil, fmt.Errorf("vmess: шифрование %q не поддерживается (auto, none, zero, aes-128-gcm, chacha20-poly1305)", scy)
	}
	d.Security = scy

	st := &model.StreamSettings{}
	if err := applyStreamFromQuery(st, get, "tls"); err != nil {
		return nil, fmt.Errorf("vmess: %v", err)
	}
	if err := validateStreamCaps(scheme, st); err != nil {
		return nil, err
	}
	if *st != (model.StreamSettings{}) {
		p.Stream = st
	}
	sniFromHost(p)
	if err := checkFieldLen(scheme, "имя", p.Name, maxRemarkLength); err != nil {
		return nil, err
	}
	return p, nil
}

func vmessFromJSON(j *vmessJSON) (*model.Profile, error) {
	const scheme = "vmess"
	if strings.TrimSpace(j.Add) == "" {
		return nil, errors.New("vmess: не указан адрес сервера (add)")
	}
	port, err := anyToInt(scheme, "port", j.Port)
	if err != nil {
		return nil, err // LNB-17: port 0/отсутствие — ошибка, не «0 в конфиг»
	}
	d := &model.VMessDetail{UUID: j.ID}
	if d.UUID == "" {
		return nil, errors.New("vmess: не указан UUID (id)")
	}
	if err := checkFieldLen(scheme, "uuid", d.UUID, maxFieldLength); err != nil {
		return nil, err
	}
	if j.Aid != nil {
		aid, err := anyToInt(scheme, "aid", j.Aid)
		if err != nil {
			return nil, err
		}
		if aid < 0 || aid > 65535 { // L3-7 (ревью): overflow-мусор вида 1e20
			return nil, fmt.Errorf("%s: aid = %d вне диапазона 0..65535", scheme, aid)
		}
		d.AlterID = aid
	}
	scy := j.Scy
	if strings.TrimSpace(scy) == "" {
		scy = "auto"
	}
	if !vmessSecurities[scy] {
		return nil, fmt.Errorf("vmess: шифрование %q не поддерживается (auto, none, zero, aes-128-gcm, chacha20-poly1305)", scy)
	}
	d.Security = scy

	p := &model.Profile{Type: model.TypeVMess, Name: j.Ps, Detail: d}
	p.Address = canonicalHost(j.Add)
	if p.Port, err = requirePort(scheme, port, true); err != nil {
		return nil, err
	}

	st := &model.StreamSettings{}
	netw := canonicalNetwork(strings.ToLower(strings.TrimSpace(j.Net)))
	if netw != "" && !networks[netw] {
		return nil, fmt.Errorf("vmess: сеть %q не поддерживается (tcp, ws, grpc, http, httpupgrade, quic, xhttp)", netw)
	}
	st.Network = netw
	st.Host = j.Host
	st.Path = j.Path
	st.HeaderType = j.Type
	sec, err := normalizeSecurityNone(j.Tls)
	if err != nil {
		return nil, fmt.Errorf("vmess: tls = %q: %v", j.Tls, err)
	}
	st.Security = sec
	st.SNI = strings.TrimSpace(j.Sni)
	st.ALPN = normalizeALPN(j.ALPN)
	if j.Fp != "" && !utlsFPs[j.Fp] {
		return nil, fmt.Errorf("vmess: отпечаток uTLS %q не поддерживается", j.Fp)
	}
	st.UTLSFingerprint = j.Fp
	st.RealityPublicKey = j.Pbk
	st.RealityShortID = j.Sid
	st.RealitySPX = j.Spx
	if st.RealityPublicKey != "" {
		st.Security = "reality"
	}
	if err := validateStreamCaps(scheme, st); err != nil {
		return nil, err
	}
	if *st != (model.StreamSettings{}) {
		p.Stream = st
	}
	sniFromHost(p)
	if err := checkFieldLen(scheme, "имя", p.Name, maxRemarkLength); err != nil {
		return nil, err
	}
	return p, nil
}

// ---- shadowsocks ----

func parseSS(s string) (*model.Profile, error) {
	const scheme = "ss"
	body := s[len("ss://"):]
	noFrag := body
	if i := strings.Index(body, "#"); i >= 0 {
		noFrag = body[:i]
	}

	d := &model.ShadowsocksDetail{}
	p := &model.Profile{Type: model.TypeShadowsocks, Detail: d}

	if strings.Contains(noFrag, "@") {
		// ss://[base64url(method:password)|method:password]@host:port[?plugin=…]
		u, _, get, err := parseURL(scheme, s)
		if err != nil {
			return nil, err
		}
		p.Name = fragName(u)
		host, port, present, err := hostPort(scheme, u)
		if err != nil {
			return nil, err
		}
		p.Address = host
		if p.Port, err = requirePort(scheme, port, present); err != nil {
			return nil, err
		}
		user, pass, hasPass := userInfo(u)
		if !hasPass || pass == "" {
			// традиционный формат: userinfo = base64url(method:password)
			b, ok := decodeB64Strict(user)
			if !ok {
				return nil, errors.New("ss: некорректный base64 в userinfo (ожидается base64url «метод:пароль»)")
			}
			m, pw, has := strings.Cut(sanitizeString(string(b)), ":")
			if !has || m == "" || pw == "" {
				return nil, errors.New("ss: в userinfo нет пары «метод:пароль»")
			}
			d.Method, d.Password = m, pw
		} else {
			// 2022-формат: method:password без base64
			d.Method, d.Password = user, pass
		}
		if v, ok := get("plugin"); ok {
			d.Plugin = strings.ReplaceAll(v, "simple-obfs;", "obfs-local;")
			if err := checkFieldLen(scheme, "plugin", d.Plugin, maxPluginLength); err != nil {
				return nil, err
			}
		}
	} else {
		// v2rayN: ss://BASE64(method:password@host:port)#замечание
		b, ok := decodeB64Strict(noFrag)
		if !ok {
			return nil, errors.New("ss: некорректный base64 (формат v2rayN: base64 «метод:пароль@хост:порт»)")
		}
		linkN := string(b)
		if i := strings.Index(body, "#"); i >= 0 {
			linkN += "#" + body[i+1:]
		}
		u, _, get, err := parseURL(scheme, "ss://"+linkN)
		if err != nil {
			return nil, fmt.Errorf("ss: %v", err)
		}
		p.Name = fragName(u)
		host, port, present, err := hostPort(scheme, u)
		if err != nil {
			return nil, err
		}
		p.Address = host
		if p.Port, err = requirePort(scheme, port, present); err != nil {
			return nil, err
		}
		d.Method, d.Password, _ = userInfo(u)
		if v, ok2 := get("plugin"); ok2 {
			d.Plugin = strings.ReplaceAll(v, "simple-obfs;", "obfs-local;")
			if err := checkFieldLen(scheme, "plugin", d.Plugin, maxPluginLength); err != nil {
				return nil, err
			}
		}
	}

	if err := checkFieldLen(scheme, "пароль", d.Password, maxFieldLength); err != nil {
		return nil, err
	}
	if d.Password == "" {
		return nil, errors.New("ss: не указан пароль")
	}
	if !ssMethods[d.Method] {
		return nil, fmt.Errorf("ss: метод шифрования %q не поддерживается", d.Method) // LNB-17
	}
	if err := checkFieldLen(scheme, "имя", p.Name, maxRemarkLength); err != nil {
		return nil, err
	}
	return p, nil
}

// ---- hysteria2 ----

func parseHysteria2(scheme, s string) (*model.Profile, error) {
	u, _, get, err := parseURL(scheme, s)
	if err != nil {
		return nil, err
	}
	d := &model.Hysteria2Detail{}
	p := &model.Profile{Type: model.TypeHysteria2, Name: fragName(u), Detail: d}

	host, port, present, err := hostPort(scheme, u)
	if err != nil {
		return nil, err
	}
	p.Address = host
	if p.Port, err = applyDefaultPort(scheme, port, present, 443); err != nil {
		return nil, err
	}

	// пароль: user[:pass] ⇒ user(+":"+pass); без пароля ⇒ весь userinfo
	user, pass, hasPass := userInfo(u)
	if hasPass && pass != "" {
		d.Password = user + ":" + pass
	} else {
		d.Password = user
	}
	if err := checkFieldLen(scheme, "пароль", d.Password, maxFieldLength); err != nil {
		return nil, err
	}
	if d.Password == "" {
		return nil, errors.New("hy2: не указан пароль")
	}

	if v, ok := get("obfs"); ok && v != "" && v != "salamander" {
		return nil, fmt.Errorf("hy2: obfs %q не поддерживается (только salamander)", v)
	}
	if v, ok := get("obfs-password"); ok {
		d.ObfsPassword = v
		if err := checkFieldLen(scheme, "obfs-password", v, maxFieldLength); err != nil {
			return nil, err
		}
	}
	if v, ok := get("mport"); ok {
		if err := validateServerPorts(v); err != nil {
			return nil, err
		}
		d.ServerPorts = v
	}
	if v, ok := get("insecure"); ok {
		d.AllowInsecure = parseBoolParam(v)
	}
	if v, ok := get("disable_sni"); ok {
		d.DisableSNI = parseBoolParam(v)
	}
	if v, ok := get("sni"); ok {
		d.SNI = strings.TrimSpace(v)
	}
	if v, ok := get("alpn"); ok {
		d.ALPN = normalizeALPN(v)
	}
	if v, ok := get("hop_interval"); ok && v != "" {
		if err := validateDuration("hy2: hop_interval", v); err != nil {
			return nil, err
		}
		d.HopInterval = v
	}
	// Полоса up/down (официальный URI hysteria2: upmbps/downmbps; NekoBox
	// эти параметры не переносил).
	for _, f := range []struct {
		key   string
		field string
		dst   *int
	}{
		{"upmbps", "upmbps", &d.UploadMbps},
		{"downmbps", "downmbps", &d.DownloadMbps},
	} {
		if v, ok := get(f.key); ok && v != "" {
			n, err := strconv.Atoi(v)
			if err != nil || n < 0 || n > 1_000_000 {
				return nil, fmt.Errorf("hy2: %s = %q: ожидается число Мбит/с", f.field, v)
			}
			*f.dst = n
		}
	}
	if err := checkFieldLen(scheme, "sni", d.SNI, maxFieldLength); err != nil {
		return nil, err
	}
	if err := checkFieldLen(scheme, "имя", p.Name, maxRemarkLength); err != nil {
		return nil, err
	}
	return p, nil
}

// ---- tuic ----

func parseTUIC(s string) (*model.Profile, error) {
	const scheme = "tuic"
	u, _, get, err := parseURL(scheme, s)
	if err != nil {
		return nil, err
	}
	d := &model.TUICDetail{}
	p := &model.Profile{Type: model.TypeTUIC, Name: fragName(u), Detail: d}

	host, port, present, err := hostPort(scheme, u)
	if err != nil {
		return nil, err
	}
	p.Address = host
	// LNB-15: дефолт 443 применяется один раз (в oracle присваивался и
	// перетирался url.port(), а ссылка без порта вообще отвергалась).
	if p.Port, err = applyDefaultPort(scheme, port, present, 443); err != nil {
		return nil, err
	}

	d.UUID, d.Password, _ = userInfo(u)
	if d.UUID == "" {
		return nil, errors.New("tuic: не указан UUID") // LNB-15
	}
	if d.Password == "" {
		return nil, errors.New("tuic: не указан пароль") // LNB-15
	}
	for _, f := range []struct{ name, val string }{{"uuid", d.UUID}, {"пароль", d.Password}} {
		if err := checkFieldLen(scheme, f.name, f.val, maxFieldLength); err != nil {
			return nil, err
		}
	}

	if v, ok := get("congestion_control"); ok {
		if !tuicCongestions[v] {
			return nil, fmt.Errorf("tuic: congestion_control %q не поддерживается (bbr, cubic, new_reno)", v)
		}
		d.CongestionControl = v
	}
	if v, ok := get("udp_relay_mode"); ok {
		if !tuicRelayModes[v] {
			return nil, fmt.Errorf("tuic: udp_relay_mode %q не поддерживается (native, quic)", v)
		}
		d.UDPRelayMode = v
	}
	if v, ok := get("alpn"); ok {
		d.ALPN = normalizeALPN(v)
	}
	if v, ok := get("sni"); ok {
		d.SNI = strings.TrimSpace(v)
	}
	if v, ok := get("allow_insecure"); ok {
		d.AllowInsecure = parseBoolParam(v)
	}
	if v, ok := get("disable_sni"); ok {
		d.DisableSNI = parseBoolParam(v)
	}
	if v, ok := get("zero_rtt_handshake"); ok {
		d.ZeroRttHandshake = parseBoolParam(v)
	}
	if v, ok := get("heartbeat"); ok && v != "" {
		if err := validateDuration("tuic: heartbeat", v); err != nil {
			return nil, err
		}
		d.Heartbeat = v
	}
	if err := checkFieldLen(scheme, "sni", d.SNI, maxFieldLength); err != nil {
		return nil, err
	}
	if err := checkFieldLen(scheme, "имя", p.Name, maxRemarkLength); err != nil {
		return nil, err
	}
	return p, nil
}
