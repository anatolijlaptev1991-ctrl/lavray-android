// SPDX-License-Identifier: GPL-3.0-only

package links

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"strconv"
	"strings"

	"github.com/anatolijlaptev1991-ctrl/lavray-android/core/internal/model"
)

// Options — параметры генерации ссылок.
type Options struct {
	// VMessURLFormat: false (по умолчанию) — vmess:// в формате v2rayN
	// (base64 JSON; настройка NekoBox old_share_link_format, дефолт true —
	// FR-UI-164 «Old Share Link Format»); true — URL-формат XTLS #716
	// (vmess://uuid@host:port?encryption=…).
	VMessURLFormat bool
}

// BuildLink — Bean2Link: генерирует стандартную share-ссылку профиля
// (FR-UI-190). Типы custom*/chain/naive не экспортируются share-ссылкой
// (возвращается ошибка; полное сохранение любых полей — BuildNekoLink,
// FR-UI-191).
func BuildLink(p *model.Profile) (string, error) {
	return BuildLinkWithOptions(p, Options{})
}

func BuildLinkWithOptions(p *model.Profile, o Options) (string, error) {
	if p == nil {
		return "", errors.New("профиль пуст")
	}
	// нетипичные для share-ссылок типы отсекаем ДО валидации полей
	switch p.Type {
	case model.TypeNaive:
		return "", fmt.Errorf("тип %s не экспортируется share-ссылкой (внешнее ядро — за флагом enable_external_cores; используйте neko-ссылку)", p.Type)
	case model.TypeCustomOutbound, model.TypeCustomConfig, model.TypeCustomExternal, model.TypeChain:
		return "", fmt.Errorf("тип %s не экспортируется share-ссылкой (используйте neko-ссылку BuildNekoLink)", p.Type)
	}
	if err := validateForBuild(p); err != nil {
		return "", err
	}
	switch p.Type {
	case model.TypeSOCKS, model.TypeHTTP:
		return buildSocksHTTP(p)
	case model.TypeShadowsocks:
		return buildSS(p)
	case model.TypeVMess:
		if o.VMessURLFormat {
			return buildVMessURL(p)
		}
		return buildVMessV2RayN(p)
	case model.TypeVLESS, model.TypeTrojan:
		return buildTrojanVLESS(p)
	case model.TypeHysteria2:
		return buildHysteria2(p)
	case model.TypeTUIC:
		return buildTUIC(p)
	default:
		return "", fmt.Errorf("тип %s не поддерживается", p.Type)
	}
}

// validateForBuild — общие проверки (LNB-15: порт 1..65535; LNB-17: списки).
func validateForBuild(p *model.Profile) error {
	if !validPort(p.Port) {
		return fmt.Errorf("порт %d вне диапазона 1–65535", p.Port)
	}
	if strings.TrimSpace(p.Address) == "" {
		return errors.New("не указан адрес сервера")
	}
	if len(p.Name) > maxRemarkLength {
		return fmt.Errorf("имя длиннее %d байт", maxRemarkLength)
	}
	if p.Stream != nil {
		if p.Stream.Network != "" && !networks[p.Stream.Network] {
			return fmt.Errorf("сеть %q не поддерживается", p.Stream.Network)
		}
		switch p.Stream.Security {
		case "", "tls", "reality":
		default:
			return fmt.Errorf("security %q не поддерживается", p.Stream.Security)
		}
		if p.Stream.UTLSFingerprint != "" && !utlsFPs[p.Stream.UTLSFingerprint] {
			return fmt.Errorf("отпечаток uTLS %q не поддерживается", p.Stream.UTLSFingerprint)
		}
		if p.Stream.Security == "reality" && p.Stream.RealityPublicKey == "" {
			return errors.New("security=reality требует reality_public_key (pbk)")
		}
	}
	return nil
}

// linkBuilder — ручная сборка ссылки с Qt-FullyEncoded-подобным
// экранированием (percentEncodeAll; без '+' в query). Логин и пароль
// хранятся РАЗДЕЛЬНО: «:» в логине кодируется как %3A и не путается с
// разделителем userinfo.
type linkBuilder struct {
	scheme   string
	user     string
	pass     string
	hasPass  bool
	host     string
	port     int
	query    []queryPair
	fragment string
}

func (b *linkBuilder) setUser(u string) *linkBuilder {
	b.user, b.hasPass = u, false
	return b
}

func (b *linkBuilder) setUserPass(u, p string) *linkBuilder {
	b.user, b.pass, b.hasPass = u, p, true
	return b
}

func (b *linkBuilder) addQ(key, value string) *linkBuilder {
	b.query = append(b.query, queryPair{key, value})
	return b
}

func (b *linkBuilder) String() string {
	var sb strings.Builder
	sb.WriteString(b.scheme)
	sb.WriteString("://")
	if b.user != "" || b.hasPass {
		sb.WriteString(percentEncodeAll(b.user))
		if b.hasPass {
			sb.WriteByte(':')
			sb.WriteString(percentEncodeAll(b.pass))
		}
		sb.WriteByte('@')
	}
	if ip := net.ParseIP(b.host); ip != nil && strings.Contains(b.host, ":") {
		sb.WriteString("[" + ip.String() + "]") // канонический IPv6
	} else {
		sb.WriteString(b.host)
	}
	if b.port > 0 {
		sb.WriteByte(':')
		sb.WriteString(strconv.Itoa(b.port))
	}
	if len(b.query) > 0 {
		sb.WriteByte('?')
		for i, q := range b.query {
			if i > 0 {
				sb.WriteByte('&')
			}
			sb.WriteString(percentEncodeAll(q.key))
			sb.WriteByte('=')
			sb.WriteString(percentEncodeAll(q.value))
		}
	}
	if b.fragment != "" {
		sb.WriteByte('#')
		sb.WriteString(percentEncodeAll(b.fragment))
	}
	return sb.String()
}

func buildSocksHTTP(p *model.Profile) (string, error) {
	d, ok := p.Detail.(*model.SocksHTTPDetail)
	if !ok {
		return "", fmt.Errorf("деталь типа %T не соответствует профилю socks/http", p.Detail)
	}
	scheme := "socks5"
	switch {
	case p.Type == model.TypeHTTP:
		scheme = "http"
		if p.Stream != nil && p.Stream.Security == "tls" {
			scheme = "https"
		}
	case d.Proto == "socks4":
		scheme = "socks4"
	}
	b := &linkBuilder{scheme: scheme, host: p.Address, port: p.Port, fragment: p.Name}
	if d.Password != "" {
		b.setUserPass(d.Username, d.Password)
	} else if d.Username != "" {
		b.setUser(d.Username)
	}
	// Расширение к oracle (импорт эти параметры читает ещё NekoBox):
	// socks с TLS и SNI — в query; для http SNI тоже в query (схема https
	// кодирует сам TLS).
	if p.Stream != nil {
		if scheme != "https" && p.Stream.Security == "tls" {
			b.addQ("security", "tls")
		}
		if p.Stream.SNI != "" {
			b.addQ("sni", p.Stream.SNI)
		}
	}
	return b.String(), nil
}

func buildSS(p *model.Profile) (string, error) {
	d, ok := p.Detail.(*model.ShadowsocksDetail)
	if !ok {
		return "", fmt.Errorf("деталь типа %T не соответствует профилю shadowsocks", p.Detail)
	}
	if !ssMethods[d.Method] {
		return "", fmt.Errorf("ss: метод шифрования %q не поддерживается", d.Method)
	}
	if d.Password == "" {
		return "", errors.New("ss: не указан пароль")
	}
	b := &linkBuilder{scheme: "ss", host: p.Address, port: p.Port, fragment: p.Name}
	if strings.HasPrefix(d.Method, "2022-") {
		// 2022-формат: method:password без base64 (пароль %-экранируется)
		b.setUserPass(d.Method, d.Password)
	} else {
		// традиционный: base64url(method:password)
		b.setUser(encodeB64URL(d.Method + ":" + d.Password))
	}
	if d.Plugin != "" {
		if err := checkFieldLen("ss", "plugin", d.Plugin, maxPluginLength); err != nil {
			return "", err
		}
		b.addQ("plugin", d.Plugin)
	}
	return b.String(), nil
}

// appendStreamQuery — параметры транспорта/безопасности для vless/trojan/
// vmess-URL. security печатается всегда (совместимость; присутствующий-пустой
// различается парсером с отсутствующим).
func appendStreamQuery(b *linkBuilder, st *model.StreamSettings, withEncryptionNone bool) {
	sec := st.Security
	if sec == "tls" && st.RealityPublicKey != "" {
		sec = "reality"
	}
	b.addQ("security", sec)
	if st.SNI != "" {
		b.addQ("sni", st.SNI)
	}
	if st.ALPN != "" {
		b.addQ("alpn", st.ALPN)
	}
	if st.AllowInsecure {
		b.addQ("allowInsecure", "1")
	}
	if st.UTLSFingerprint != "" {
		b.addQ("fp", st.UTLSFingerprint)
	}
	if sec == "reality" {
		b.addQ("pbk", st.RealityPublicKey)
		if st.RealityShortID != "" {
			b.addQ("sid", st.RealityShortID)
		}
		if st.RealitySPX != "" {
			b.addQ("spx", st.RealitySPX)
		}
	}
	netw := st.Network
	if netw == "" {
		netw = "tcp"
	}
	b.addQ("type", netw)
	switch netw {
	case "ws", "http", "httpupgrade", "xhttp":
		b.addIf("path", st.Path).addIf("host", st.Host)
		if netw == "xhttp" {
			b.addIf("mode", st.XHTTPMode).addIf("extra", st.XHTTPExtra)
		}
	case "grpc":
		if st.Path != "" {
			b.addQ("serviceName", st.Path)
		}
	case "tcp":
		if st.HeaderType == "http" {
			b.addQ("headerType", "http")
			if st.Host != "" {
				b.addQ("host", st.Host)
			}
			if st.Path != "" {
				b.addQ("path", st.Path)
			}
		}
	}
	if withEncryptionNone {
		b.addQ("encryption", "none")
	}
}

func buildTrojanVLESS(p *model.Profile) (string, error) {
	d, ok := p.Detail.(*model.TrojanVlessDetail)
	if !ok {
		return "", fmt.Errorf("деталь типа %T не соответствует профилю trojan/vless", p.Detail)
	}
	if d.Password == "" {
		if p.Type == model.TypeVLESS {
			return "", errors.New("vless: не указан UUID")
		}
		return "", errors.New("trojan: не указан пароль")
	}
	if p.Type == model.TypeVLESS && d.Flow != "" && !vlessFlows[d.Flow] {
		return "", fmt.Errorf("vless: flow %q не поддерживается", d.Flow)
	}
	scheme := "vless"
	if p.Type == model.TypeTrojan {
		scheme = "trojan"
	}
	b := &linkBuilder{scheme: scheme, host: p.Address, port: p.Port, fragment: p.Name}
	b.setUser(d.Password)
	st := p.Stream
	if st == nil {
		st = &model.StreamSettings{}
	}
	appendStreamQuery(b, st, p.Type == model.TypeVLESS)
	if p.Type == model.TypeVLESS && d.Flow != "" {
		b.addQ("flow", d.Flow)
	}
	return b.String(), nil
}

// buildVMessV2RayN — формат v2rayN: vmess://BASE64(JSON) (FR-UI-164).
func buildVMessV2RayN(p *model.Profile) (string, error) {
	d, ok := p.Detail.(*model.VMessDetail)
	if !ok {
		return "", fmt.Errorf("деталь типа %T не соответствует профилю vmess", p.Detail)
	}
	if d.UUID == "" {
		return "", errors.New("vmess: не указан UUID")
	}
	scy := d.Security
	if scy == "" {
		scy = "auto"
	}
	if !vmessSecurities[scy] {
		return "", fmt.Errorf("vmess: шифрование %q не поддерживается", scy)
	}
	st := p.Stream
	if st == nil {
		st = &model.StreamSettings{}
	}
	netw := st.Network
	if netw == "" {
		netw = "tcp"
	}
	obj := map[string]any{
		"v":    "2",
		"ps":   p.Name,
		"add":  p.Address,
		"port": p.Port,
		"id":   d.UUID,
		"aid":  d.AlterID,
		"net":  netw,
		"host": st.Host,
		"path": st.Path,
		"type": st.HeaderType,
		"scy":  scy,
		"tls":  map[bool]string{true: "tls", false: ""}[st.Security == "tls" || st.Security == "reality"],
		"sni":  st.SNI,
	}
	// Расширение к oracle (NekoBox эти ключи не переносил; v2rayN их пишет):
	// alpn/fp и REALITY-параметры.
	if st.ALPN != "" {
		obj["alpn"] = st.ALPN
	}
	if st.UTLSFingerprint != "" {
		obj["fp"] = st.UTLSFingerprint
	}
	if st.RealityPublicKey != "" {
		obj["pbk"] = st.RealityPublicKey
		if st.RealityShortID != "" {
			obj["sid"] = st.RealityShortID
		}
		if st.RealitySPX != "" {
			obj["spx"] = st.RealitySPX
		}
	}
	j, err := json.Marshal(obj)
	if err != nil {
		return "", err
	}
	return "vmess://" + base64.StdEncoding.EncodeToString(j), nil
}

// buildVMessURL — URL-формат (XTLS #716).
func buildVMessURL(p *model.Profile) (string, error) {
	d, ok := p.Detail.(*model.VMessDetail)
	if !ok {
		return "", fmt.Errorf("деталь типа %T не соответствует профилю vmess", p.Detail)
	}
	if d.UUID == "" {
		return "", errors.New("vmess: не указан UUID")
	}
	scy := d.Security
	if scy == "" {
		scy = "auto"
	}
	if !vmessSecurities[scy] {
		return "", fmt.Errorf("vmess: шифрование %q не поддерживается", scy)
	}
	b := &linkBuilder{scheme: "vmess", host: p.Address, port: p.Port, fragment: p.Name}
	b.setUser(d.UUID)
	st := p.Stream
	if st == nil {
		st = &model.StreamSettings{}
	}
	// aid всегда 0 — стандарт только для VMess AEAD
	q := []queryPair{{"encryption", scy}}
	b.query = append(q, b.query...)
	appendStreamQuery(b, st, false)
	return b.String(), nil
}

func buildHysteria2(p *model.Profile) (string, error) {
	d, ok := p.Detail.(*model.Hysteria2Detail)
	if !ok {
		return "", fmt.Errorf("деталь типа %T не соответствует профилю hysteria2", p.Detail)
	}
	if d.Password == "" {
		return "", errors.New("hy2: не указан пароль")
	}
	if err := validateServerPorts(d.ServerPorts); err != nil {
		return "", err
	}
	if d.HopInterval != "" {
		if err := validateDuration("hop_interval", d.HopInterval); err != nil {
			return "", err
		}
	}
	b := &linkBuilder{scheme: "hy2", host: p.Address, port: p.Port, fragment: p.Name}
	if u, pass, has := strings.Cut(d.Password, ":"); has {
		b.setUserPass(u, pass)
	} else {
		b.setUser(d.Password)
	}
	if d.ObfsPassword != "" {
		b.addQ("obfs", "salamander")
		b.addQ("obfs-password", d.ObfsPassword)
	}
	if d.ServerPorts != "" {
		b.addQ("mport", d.ServerPorts)
	}
	if d.HopInterval != "" {
		b.addQ("hop_interval", d.HopInterval)
	}
	if d.UploadMbps > 0 {
		b.addQ("upmbps", strconv.Itoa(d.UploadMbps)) // официальный URI hysteria2
	}
	if d.DownloadMbps > 0 {
		b.addQ("downmbps", strconv.Itoa(d.DownloadMbps))
	}
	if d.AllowInsecure {
		b.addQ("insecure", "1")
	}
	if d.DisableSNI {
		b.addQ("disable_sni", "1")
	}
	if d.SNI != "" {
		b.addQ("sni", d.SNI)
	}
	if d.ALPN != "" {
		b.addQ("alpn", d.ALPN)
	}
	return b.String(), nil
}

func buildTUIC(p *model.Profile) (string, error) {
	d, ok := p.Detail.(*model.TUICDetail)
	if !ok {
		return "", fmt.Errorf("деталь типа %T не соответствует профилю tuic", p.Detail)
	}
	if d.UUID == "" {
		return "", errors.New("tuic: не указан UUID")
	}
	if d.Password == "" {
		return "", errors.New("tuic: не указан пароль")
	}
	if !tuicCongestions[d.CongestionControl] {
		return "", fmt.Errorf("tuic: congestion_control %q не поддерживается", d.CongestionControl)
	}
	if !tuicRelayModes[d.UDPRelayMode] {
		return "", fmt.Errorf("tuic: udp_relay_mode %q не поддерживается", d.UDPRelayMode)
	}
	if d.Heartbeat != "" {
		if err := validateDuration("heartbeat", d.Heartbeat); err != nil {
			return "", err
		}
	}
	b := &linkBuilder{scheme: "tuic", host: p.Address, port: p.Port, fragment: p.Name}
	b.setUserPass(d.UUID, d.Password)
	if d.CongestionControl != "" {
		b.addQ("congestion_control", d.CongestionControl)
	}
	if d.ALPN != "" {
		b.addQ("alpn", d.ALPN)
	}
	if d.SNI != "" {
		b.addQ("sni", d.SNI)
	}
	if d.UDPRelayMode != "" {
		b.addQ("udp_relay_mode", d.UDPRelayMode)
	}
	if d.ZeroRttHandshake {
		b.addQ("zero_rtt_handshake", "1")
	}
	if d.Heartbeat != "" {
		b.addQ("heartbeat", d.Heartbeat)
	}
	if d.AllowInsecure {
		b.addQ("allow_insecure", "1")
	}
	if d.DisableSNI {
		b.addQ("disable_sni", "1")
	}
	return b.String(), nil
}
