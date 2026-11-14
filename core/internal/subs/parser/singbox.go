// SPDX-License-Identifier: GPL-3.0-only

package parser

// sing-box JSON-подписка (FR-UI-264, W3): автопроба — JSON-объект с массивом
// "outbounds" либо верхний уровень — массив outbounds. В профили попадают
// только известные типы; selector/urltest/direct/block/dns и неизвестные —
// skip (инвариант FuzzParseSingboxJson: unknown → skip, лимит длины).

import (
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/anatolijlaptev1991-ctrl/lavray-android/core/internal/model"
)

// sbTLS — блок tls{} sing-box outbound.
type sbTLS struct {
	Enabled    bool     `json:"enabled"`
	ServerName string   `json:"server_name"`
	Insecure   bool     `json:"insecure"`
	ALPN       []string `json:"alpn"`
	Disabled   bool     `json:"disabled"` // устаревшее поле старых конфигов
	UTLS       *struct {
		Enabled     bool   `json:"enabled"`
		Fingerprint string `json:"fingerprint"`
	} `json:"utls"`
	Reality *struct {
		Enabled   bool   `json:"enabled"`
		PublicKey string `json:"public_key"`
		ShortID   string `json:"short_id"`
	} `json:"reality"`
}

// sbTransport — блок transport{} sing-box outbound.
type sbTransport struct {
	Type                string         `json:"type"`
	Path                string         `json:"path"`
	ServiceName         string         `json:"service_name"`
	Headers             map[string]any `json:"headers"`
	EarlyDataHeaderName string         `json:"early_data_header_name"`
	MaxEarlyData        int            `json:"max_early_data"`
}

// sbObfs — obfs{} hysteria2.
type sbObfs struct {
	Type     string `json:"type"`
	Password string `json:"password"`
}

// sbOutbound — известные поля всех поддерживаемых типов outbounds.
// Незнакомые поля игнорируются (json.Unmarshal по умолчанию).
type sbOutbound struct {
	Type       string `json:"type"`
	Tag        string `json:"tag"`
	Server     string `json:"server"`
	ServerPort int    `json:"server_port"`
	Detour     string `json:"detour"`

	Username string `json:"username"`
	Password string `json:"password"`
	Version  string `json:"version"` // socks: "4"|"4a"|"5"

	Method     string `json:"method"` // shadowsocks
	Plugin     string `json:"plugin"`
	PluginOpts string `json:"plugin_opts"`

	UUID     string `json:"uuid"` // vmess/vless/tuic
	Security string `json:"security"`
	AlterID  int    `json:"alter_id"`
	Flow     string `json:"flow"`

	Obfs         *sbObfs `json:"obfs"` // hysteria2
	UploadMbps   int     `json:"up_mbps"`
	DownloadMbps int     `json:"down_mbps"`
	ServerPorts  string  `json:"server_ports"` // sing-box 1.14 (FR-UI-268)
	HopInterval  string  `json:"hop_interval"`

	CongestionControl string `json:"congestion_control"` // tuic
	UDPRelayMode      string `json:"udp_relay_mode"`
	UDPOverStream     bool   `json:"udp_over_stream"`
	ZeroRttHandshake  bool   `json:"zero_rtt_handshake"`
	Heartbeat         string `json:"heartbeat"`

	TLS       *sbTLS       `json:"tls"`
	Transport *sbTransport `json:"transport"`
}

// Локальные белые списки значений (копии внутренних списков links — пакет
// не должен зависеть от private-мап W2; рассинхрон ловится roundtrip-тестами).
var (
	sbNetworks = map[string]bool{
		"http": true, "ws": true, "quic": true, "grpc": true, "httpupgrade": true,
	}
	sbVMessSecurities = map[string]bool{
		"": true, "auto": true, "none": true, "zero": true,
		"aes-128-gcm": true, "chacha20-poly1305": true,
	}
	sbVlessFlows = map[string]bool{"": true, "xtls-rprx-vision": true}
	sbTUICCc     = map[string]bool{"": true, "bbr": true, "cubic": true, "new_reno": true}
	sbTUICRelay  = map[string]bool{"": true, "native": true, "quic": true}
	sbKnownTypes = map[string]bool{
		"socks": true, "http": true, "shadowsocks": true, "vmess": true,
		"vless": true, "trojan": true, "hysteria2": true, "tuic": true,
	}
)

// parseSingBoxJSON пробует разобрать содержимое как sing-box подписку.
// handled=false — содержимое не является JSON с outbounds (или не JSON вовсе).
func parseSingBoxJSON(data []byte, maxProfiles int) (res *Result, handled bool, err error) {
	var outbounds []json.RawMessage
	trim := strings.TrimSpace(string(data))
	if trim == "" {
		return nil, false, nil
	}
	switch trim[0] {
	case '{':
		var obj struct {
			Outbounds []json.RawMessage `json:"outbounds"`
		}
		if err := json.Unmarshal(data, &obj); err != nil {
			return nil, false, nil // не валидный JSON — пусть пробуют другие форматы
		}
		if obj.Outbounds == nil {
			return nil, true, errors.New("формат подписки не распознан: JSON без массива outbounds (Clash YAML не поддерживается)")
		}
		outbounds = obj.Outbounds
	case '[':
		if err := json.Unmarshal(data, &outbounds); err != nil {
			return nil, false, nil
		}
	default:
		return nil, false, nil
	}

	res = &Result{Format: FormatSingBox}
	for i, raw := range outbounds {
		if len(res.Profiles) >= maxProfiles {
			res.Warnings = append(res.Warnings,
				fmt.Sprintf("достигнут лимит числа профилей (%d) — остальные outbounds пропущены", maxProfiles))
			break
		}
		var ob sbOutbound
		if err := json.Unmarshal(raw, &ob); err != nil {
			res.Errors = append(res.Errors, LineError{Line: i + 1, Text: truncateRunes(string(raw)), Err: "не является JSON-объектом outbound"})
			continue
		}
		if !sbKnownTypes[ob.Type] {
			res.Skipped++ // selector/urltest/direct/block/dns/unknown → skip
			continue
		}
		prof, perr := outboundToProfile(&ob)
		if perr != nil {
			res.Errors = append(res.Errors, LineError{Line: i + 1, Text: ob.tagOrType(), Err: perr.Error()})
			continue
		}
		res.Profiles = append(res.Profiles, prof)
	}
	if len(res.Profiles) == 0 {
		if len(res.Errors) > 0 {
			first := res.Errors[0]
			return nil, true, fmt.Errorf("формат подписки не распознан: в sing-box JSON нет известных outbounds (первая ошибка: %d: %s)", first.Line, first.Err)
		}
		return nil, true, errors.New("формат подписки не распознан: в sing-box JSON нет известных outbounds (только служебные/неизвестные типы)")
	}
	return res, true, nil
}

func (ob *sbOutbound) tagOrType() string {
	if ob.Tag != "" {
		return truncateRunes(ob.Tag)
	}
	return ob.Type
}

// outboundToProfile — конвертация одного outbound в model.Profile.
func outboundToProfile(ob *sbOutbound) (*model.Profile, error) {
	p := &model.Profile{Name: ob.Tag}
	p.Address = strings.TrimSpace(ob.Server)
	if p.Address == "" {
		return nil, fmt.Errorf("%s: не указан server", ob.Type)
	}
	p.Port = ob.ServerPort
	if p.Port < 1 || p.Port > 65535 {
		return nil, fmt.Errorf("%s: server_port %d вне диапазона 1–65535", ob.Type, ob.ServerPort)
	}
	if p.Name == "" {
		p.Name = p.Address
	}
	if len(p.Name) > 4096 {
		return nil, fmt.Errorf("%s: tag длиннее 4096 байт", ob.Type)
	}

	switch ob.Type {
	case "socks":
		p.Type = model.TypeSOCKS
		proto := "socks5"
		switch ob.Version {
		case "", "5":
		case "4", "4a":
			proto = "socks4"
		default:
			return nil, fmt.Errorf("socks: version %q не поддерживается", ob.Version)
		}
		p.Detail = &model.SocksHTTPDetail{Proto: proto, Username: ob.Username, Password: ob.Password}
	case "http":
		p.Type = model.TypeHTTP
		p.Detail = &model.SocksHTTPDetail{Proto: "http", Username: ob.Username, Password: ob.Password}
	case "shadowsocks":
		p.Type = model.TypeShadowsocks
		d := &model.ShadowsocksDetail{Method: ob.Method, Password: ob.Password}
		if ob.Plugin != "" {
			if ob.PluginOpts != "" {
				d.Plugin = ob.Plugin + ";" + ob.PluginOpts
			} else {
				d.Plugin = ob.Plugin
			}
		}
		p.Detail = d
		if d.Password == "" {
			return nil, errors.New("shadowsocks: password пуст")
		}
		if !ssMethodOK(d.Method) {
			return nil, fmt.Errorf("shadowsocks: метод %q не поддерживается", d.Method)
		}
	case "vmess":
		p.Type = model.TypeVMess
		sec := ob.Security
		if sec == "" {
			sec = "auto"
		}
		if !sbVMessSecurities[sec] {
			return nil, fmt.Errorf("vmess: security %q не поддерживается", sec)
		}
		if ob.UUID == "" {
			return nil, errors.New("vmess: uuid пуст")
		}
		if ob.AlterID < 0 || ob.AlterID > 65535 {
			return nil, fmt.Errorf("vmess: alter_id %d вне диапазона 0..65535", ob.AlterID)
		}
		p.Detail = &model.VMessDetail{UUID: ob.UUID, AlterID: ob.AlterID, Security: sec}
	case "vless":
		p.Type = model.TypeVLESS
		if !sbVlessFlows[ob.Flow] {
			return nil, fmt.Errorf("vless: flow %q не поддерживается (допустимо: xtls-rprx-vision)", ob.Flow)
		}
		if ob.UUID == "" {
			return nil, errors.New("vless: uuid пуст")
		}
		p.Detail = &model.TrojanVlessDetail{Password: ob.UUID, Flow: ob.Flow}
	case "trojan":
		p.Type = model.TypeTrojan
		if ob.Password == "" {
			return nil, errors.New("trojan: password пуст")
		}
		p.Detail = &model.TrojanVlessDetail{Password: ob.Password}
	case "hysteria2":
		p.Type = model.TypeHysteria2
		if ob.Password == "" {
			return nil, errors.New("hysteria2: password пуст")
		}
		d := &model.Hysteria2Detail{Password: ob.Password,
			UploadMbps: ob.UploadMbps, DownloadMbps: ob.DownloadMbps,
			ServerPorts: ob.ServerPorts, HopInterval: ob.HopInterval}
		if ob.Obfs != nil {
			if ob.Obfs.Type != "" && ob.Obfs.Type != "salamander" {
				return nil, fmt.Errorf("hysteria2: obfs.type %q не поддерживается (только salamander)", ob.Obfs.Type)
			}
			d.ObfsPassword = ob.Obfs.Password
		}
		if d.ServerPorts != "" && !serverPortsOK(d.ServerPorts) {
			return nil, fmt.Errorf("hysteria2: server_ports %q некорректны", d.ServerPorts)
		}
		if d.HopInterval != "" {
			if err := validateSBDuration(d.HopInterval); err != nil {
				return nil, fmt.Errorf("hysteria2: hop_interval %q: %v", d.HopInterval, err)
			}
		}
		if ob.TLS != nil {
			d.SNI = ob.TLS.ServerName
			d.ALPN = joinALPN(ob.TLS.ALPN)
			d.AllowInsecure = ob.TLS.Insecure
		}
		p.Detail = d
	case "tuic":
		p.Type = model.TypeTUIC
		if !sbTUICCc[ob.CongestionControl] {
			return nil, fmt.Errorf("tuic: congestion_control %q не поддерживается", ob.CongestionControl)
		}
		if !sbTUICRelay[ob.UDPRelayMode] {
			return nil, fmt.Errorf("tuic: udp_relay_mode %q не поддерживается", ob.UDPRelayMode)
		}
		if ob.UUID == "" || ob.Password == "" {
			return nil, errors.New("tuic: uuid/password пуст")
		}
		d := &model.TUICDetail{UUID: ob.UUID, Password: ob.Password,
			CongestionControl: ob.CongestionControl, UDPRelayMode: ob.UDPRelayMode,
			UDPOverStream: ob.UDPOverStream, ZeroRttHandshake: ob.ZeroRttHandshake,
			Heartbeat: ob.Heartbeat}
		if d.Heartbeat != "" {
			if err := validateSBDuration(d.Heartbeat); err != nil {
				return nil, fmt.Errorf("tuic: heartbeat %q: %v", d.Heartbeat, err)
			}
		}
		if ob.TLS != nil {
			d.SNI = ob.TLS.ServerName
			d.ALPN = joinALPN(ob.TLS.ALPN)
			d.AllowInsecure = ob.TLS.Insecure
		}
		p.Detail = d
	default:
		return nil, fmt.Errorf("тип %q не поддерживается", ob.Type)
	}

	// TLS + транспорт — для типов со StreamSettings.
	switch ob.Type {
	case "vmess", "vless", "trojan", "shadowsocks", "socks", "http":
		st, err := streamFrom(ob.TLS, ob.Transport, ob.Type)
		if err != nil {
			return nil, err
		}
		if *st != (model.StreamSettings{}) {
			p.Stream = st
		}
	}
	return p, nil
}

// streamFrom строит StreamSettings из tls/transport блоков.
func streamFrom(tls *sbTLS, tr *sbTransport, typ string) (*model.StreamSettings, error) {
	st := &model.StreamSettings{}
	if tls != nil && tls.Enabled && !tls.Disabled {
		st.Security = "tls"
		st.SNI = tls.ServerName
		st.AllowInsecure = tls.Insecure
		st.ALPN = joinALPN(tls.ALPN)
		if tls.UTLS != nil && tls.UTLS.Enabled {
			st.UTLSFingerprint = tls.UTLS.Fingerprint
		}
		if tls.Reality != nil && tls.Reality.Enabled {
			st.RealityPublicKey = tls.Reality.PublicKey
			st.RealityShortID = tls.Reality.ShortID
			if st.RealityPublicKey != "" {
				st.Security = "reality"
			}
		}
	}
	if tr != nil {
		n := strings.ToLower(tr.Type)
		if n == "" {
			n = "tcp"
		}
		if !sbNetworks[n] {
			return nil, fmt.Errorf("%s: transport.type %q не поддерживается", typ, tr.Type)
		}
		st.Network = n
		switch {
		case tr.Path != "":
			st.Path = tr.Path
		case tr.ServiceName != "":
			st.Path = tr.ServiceName
		}
		if host := headerHost(tr.Headers); host != "" {
			st.Host = host
		}
		if tr.EarlyDataHeaderName != "" {
			st.WSEarlyDataHeader = tr.EarlyDataHeaderName
			st.WSEarlyDataMax = tr.MaxEarlyData
		}
	}
	if st.Security == "reality" && st.RealityPublicKey == "" {
		return nil, fmt.Errorf("%s: reality без public_key", typ)
	}
	return st, nil
}

// headerHost — headers.Host (string или []string) → строка через запятую.
func headerHost(headers map[string]any) string {
	if headers == nil {
		return ""
	}
	v, ok := headers["Host"]
	if !ok {
		return ""
	}
	switch h := v.(type) {
	case string:
		return strings.ToValidUTF8(h, "�")
	case []any:
		parts := make([]string, 0, len(h))
		for _, e := range h {
			if s, ok := e.(string); ok {
				parts = append(parts, strings.ToValidUTF8(s, "�"))
			}
		}
		return strings.Join(parts, ",")
	}
	return ""
}

func joinALPN(list []string) string {
	parts := make([]string, 0, len(list))
	for _, s := range list {
		if t := strings.TrimSpace(s); t != "" {
			parts = append(parts, t)
		}
	}
	return strings.Join(parts, ",")
}

// ssMethodOK — белый список методов ss (синхронизирован с links; отдельная
// копия, чтобы не зависеть от private-мап W2).
func ssMethodOK(m string) bool {
	switch m {
	case "2022-blake3-aes-128-gcm", "2022-blake3-aes-256-gcm", "2022-blake3-chacha20-poly1305",
		"none", "aes-128-gcm", "aes-192-gcm", "aes-256-gcm",
		"chacha20-ietf-poly1305", "xchacha20-ietf-poly1305",
		"aes-128-ctr", "aes-192-ctr", "aes-256-ctr",
		"aes-128-cfb", "aes-192-cfb", "aes-256-cfb",
		"rc4-md5", "chacha20-ietf", "xchacha20":
		return true
	}
	return false
}

// serverPortsOK — «a:b» / список портов hy2 (та же грамматика, что mport).
func serverPortsOK(s string) bool {
	if s == "" {
		return true
	}
	token := strings.Builder{}
	flush := func() bool {
		if token.Len() == 0 {
			return false
		}
		n, err := strconv.Atoi(token.String())
		if err != nil || n < 1 || n > 65535 {
			return false
		}
		token.Reset()
		return true
	}
	for i := 0; i < len(s); i++ {
		switch c := s[i]; {
		case c >= '0' && c <= '9':
			token.WriteByte(c)
		case c == ':' || c == '-' || c == ',':
			if !flush() {
				return false
			}
		default:
			return false
		}
	}
	return flush()
}

// validateSBDuration — Duration-строка sing-box (hop_interval, heartbeat).
func validateSBDuration(s string) error {
	if _, err := time.ParseDuration(s); err != nil {
		return fmt.Errorf("некорректная длительность (пример: 30s)")
	}
	return nil
}
