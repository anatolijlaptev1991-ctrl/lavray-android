// SPDX-License-Identifier: GPL-3.0-only

package links

// neko-ссылки — FR-UI-191: nekoray://<type>#<base64url(JSON bean)> — полное
// сохранение полей профиля. Формат bean — JSON NekoBox (fmt/*Bean.hpp,
// AbstractBean::ToNekorayShareLink); ключи совпадают с маппингом
// internal/store/nekoimport (W1).

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/anatolijlaptev1991-ctrl/lavray-android/core/internal/model"
)

// nekoBean — JSON bean NekoBox (см. nekoimport.nekoBean; отдельная копия —
// пакет не должен тянуть SQLite-слой). Пустые значения NekoBox не пишет
// (JsonStore::ToJson), поэтому omitempty.
type nekoBean struct {
	V    int    `json:"_v,omitempty"`
	Name string `json:"name,omitempty"`
	Addr string `json:"addr,omitempty"`
	Port int    `json:"port,omitempty"`
	// c_cfg/c_out: ЭКСПОРТИРУЮТСЯ (полное сохранение полей — FR-UI-191), но
	// на ИМПОРТЕ отбрасываются (LNB-25/FR-UI-257: подписка/share-ссылка не
	// может доставить конфиг ядра; ручное включение — двухфазный импорт, W3).
	CCfg string `json:"c_cfg,omitempty"`
	COut string `json:"c_out,omitempty"`

	SocksType int    `json:"v,omitempty"` // 5 socks5 | 4 socks4 | -80 http
	Username  string `json:"username,omitempty"`
	Password  string `json:"password,omitempty"`

	Method string `json:"method,omitempty"`
	Pass   string `json:"pass,omitempty"`
	Plugin string `json:"plugin,omitempty"`
	Uot    int    `json:"uot,omitempty"`

	VMessID string `json:"id,omitempty"`
	Aid     int    `json:"aid,omitempty"`
	Sec     string `json:"sec,omitempty"`

	Flow string `json:"flow,omitempty"`

	ObfsPassword   string `json:"obfsPassword,omitempty"`
	UploadMbps     int    `json:"uploadMbps,omitempty"`
	DownloadMbps   int    `json:"downloadMbps,omitempty"`
	HopIntervalSec int    `json:"hopInterval,omitempty"`
	HopPort        string `json:"hopPort,omitempty"`
	TUICUUID       string `json:"uuid,omitempty"`
	Congestion     string `json:"congestionControl,omitempty"`
	UDPRelayMode   string `json:"udpRelayMode,omitempty"`
	ZeroRtt        bool   `json:"zeroRttHandshake,omitempty"`
	Heartbeat      string `json:"heartbeat,omitempty"`
	Uos            bool   `json:"uos,omitempty"`
	AllowInsecure  bool   `json:"allowInsecure,omitempty"`
	QUICSNI        string `json:"sni,omitempty"`
	ALPN           string `json:"alpn,omitempty"`
	CaText         string `json:"caText,omitempty"`
	DisableSni     bool   `json:"disableSni,omitempty"`

	Protocol     string `json:"protocol,omitempty"`
	ExtraHeaders string `json:"extra_headers,omitempty"`
	Certificate  string `json:"certificate,omitempty"`
	InsecureConc int    `json:"insecure_concurrency,omitempty"`
	DisableLog   bool   `json:"disable_log,omitempty"`

	Core         string   `json:"core,omitempty"`
	Command      []string `json:"cmd,omitempty"`
	ConfigSimple string   `json:"cs,omitempty"`
	ConfigSuffix string   `json:"cs_suffix,omitempty"`
	MappingPort  int      `json:"mapping_port,omitempty"`
	SocksPort    int      `json:"socks_port,omitempty"`

	List []int64 `json:"list,omitempty"`

	Stream *nekoStream `json:"stream,omitempty"`
}

// nekoStream — V2rayStreamSettings (ключи fmt/V2RayStreamSettings.hpp).
type nekoStream struct {
	Network        string `json:"net,omitempty"`
	Security       string `json:"sec,omitempty"`
	PacketEncoding string `json:"pac_enc,omitempty"`
	Path           string `json:"path,omitempty"`
	Host           string `json:"host,omitempty"`
	HeaderType     string `json:"h_type,omitempty"`
	SNI            string `json:"sni,omitempty"`
	ALPN           string `json:"alpn,omitempty"`
	Certificate    string `json:"cert,omitempty"`
	UTLS           string `json:"utls,omitempty"`
	Insecure       bool   `json:"insecure,omitempty"`
	EdName         string `json:"ed_name,omitempty"`
	EdLen          int    `json:"ed_len,omitempty"`
	Pbk            string `json:"pbk,omitempty"`
	Sid            string `json:"sid,omitempty"`
	Spx            string `json:"spx,omitempty"`
	MuxS           int    `json:"mux_s,omitempty"`
}

// nekoTypeOf — строковый тип ссылки (host-часть nekoray://<type>#…).
func nekoTypeOf(p *model.Profile) (string, error) {
	switch p.Type {
	case model.TypeSOCKS, model.TypeHTTP, model.TypeShadowsocks, model.TypeVMess,
		model.TypeTrojan, model.TypeVLESS, model.TypeNaive, model.TypeHysteria2,
		model.TypeTUIC, model.TypeChain:
		return string(p.Type), nil
	case model.TypeCustomOutbound, model.TypeCustomConfig, model.TypeCustomExternal:
		return "custom", nil
	default:
		return "", fmt.Errorf("тип %q не поддерживается neko-ссылкой", p.Type)
	}
}

// BuildNekoLink — FR-UI-191: nekoray://<type>#<base64url(JSON bean)>.
// Сохраняет ВСЕ поля профиля, представимые в bean NekoBox (включая stream и
// custom-поля c_cfg/c_out — получатель обязан отбросить их при импорте,
// см. LNB-25). Метаданные (id, группа, трафик, тесты, теги/заметки LAVRay)
// в формат NekoBox не входят.
func BuildNekoLink(p *model.Profile) (string, error) {
	if p == nil {
		return "", errors.New("профиль пуст")
	}
	typ, err := nekoTypeOf(p)
	if err != nil {
		return "", err
	}
	b, err := beanFromProfile(p)
	if err != nil {
		return "", err
	}
	j, err := json.Marshal(b)
	if err != nil {
		return "", err
	}
	return "nekoray://" + typ + "#" + encodeB64URL(string(j)), nil
}

// encodeB64URL — base64url c padding (QByteArray::toBase64(Base64UrlEncoding),
// как в AbstractBean::ToNekorayShareLink).
func encodeB64URL(s string) string {
	return base64.URLEncoding.EncodeToString([]byte(s))
}

// parseNekoLink — nekoray://<type>#<base64url(JSON bean)>.
func parseNekoLink(s string) (*model.Profile, error) {
	const scheme = "nekoray"
	body := s[len("nekoray://"):]
	typ, b64, has := strings.Cut(body, "#")
	typ = strings.ToLower(typ)
	if typ == "" || !has || strings.TrimSpace(b64) == "" {
		return nil, fmt.Errorf("%s: ожидается формат nekoray://<тип>#<base64 bean>", scheme)
	}
	raw, ok := decodeB64Any(b64)
	if !ok {
		return nil, fmt.Errorf("%s: некорректный base64 в bean", scheme)
	}
	var b nekoBean
	if err := json.Unmarshal(raw, &b); err != nil {
		return nil, fmt.Errorf("%s: bean не является корректным JSON: %v", scheme, err)
	}
	// LNB-25/FR-UI-257: конфиг ядра из ссылки не доставляется.
	b.CCfg, b.COut = "", ""
	return profileFromBean(typ, &b)
}

func beanFromProfile(p *model.Profile) (*nekoBean, error) {
	b := &nekoBean{Name: p.Name, Addr: p.Address, Port: p.Port,
		CCfg: p.CustomConfigJSON, COut: p.CustomOutboundJSON}
	if p.Stream != nil {
		st := p.Stream
		b.Stream = &nekoStream{
			Network: st.Network, Security: st.Security, PacketEncoding: st.PacketEncoding,
			Path: st.Path, Host: st.Host, HeaderType: st.HeaderType, SNI: st.SNI,
			ALPN: st.ALPN, Certificate: st.Certificate, UTLS: st.UTLSFingerprint,
			Insecure: st.AllowInsecure, EdName: st.WSEarlyDataHeader, EdLen: st.WSEarlyDataMax,
			Pbk: st.RealityPublicKey, Sid: st.RealityShortID, Spx: st.RealitySPX,
			MuxS: st.Multiplex,
		}
	}
	switch d := p.Detail.(type) {
	case *model.SocksHTTPDetail:
		switch d.Proto {
		case "http":
			b.SocksType = -80
		case "socks4":
			b.SocksType = 4
		default:
			b.SocksType = 5
		}
		b.Username, b.Password = d.Username, d.Password
	case *model.ShadowsocksDetail:
		b.Method, b.Pass, b.Plugin, b.Uot = d.Method, d.Password, d.Plugin, d.UDPOverTCP
	case *model.VMessDetail:
		b.VMessID, b.Aid, b.Sec = d.UUID, d.AlterID, d.Security
	case *model.TrojanVlessDetail:
		b.Pass, b.Flow = d.Password, d.Flow
	case *model.NaiveDetail:
		b.Username, b.Password, b.Protocol = d.Username, d.Password, d.Proto
		b.ExtraHeaders, b.Certificate = d.ExtraHeaders, d.Certificate
		b.InsecureConc, b.DisableLog = d.InsecureConcurrency, d.DisableLogs
		if d.SNI != "" {
			b.QUICSNI = d.SNI
		}
	case *model.Hysteria2Detail:
		b.Password = d.Password
		b.ObfsPassword, b.UploadMbps, b.DownloadMbps = d.ObfsPassword, d.UploadMbps, d.DownloadMbps
		b.HopPort = d.ServerPorts
		if d.HopInterval != "" {
			sec, err := durationToSeconds(d.HopInterval)
			if err != nil {
				return nil, fmt.Errorf("hysteria2: %v", err)
			}
			b.HopIntervalSec = sec
		}
		b.QUICSNI, b.ALPN, b.CaText = d.SNI, d.ALPN, d.CA
		b.AllowInsecure, b.DisableSni = d.AllowInsecure, d.DisableSNI
	case *model.TUICDetail:
		b.TUICUUID, b.Password = d.UUID, d.Password
		b.Congestion, b.UDPRelayMode = d.CongestionControl, d.UDPRelayMode
		b.ZeroRtt, b.Heartbeat, b.Uos = d.ZeroRttHandshake, d.Heartbeat, d.UDPOverStream
		b.QUICSNI, b.ALPN, b.CaText = d.SNI, d.ALPN, d.CA
		b.AllowInsecure, b.DisableSni = d.AllowInsecure, d.DisableSNI
	case *model.CustomDetail:
		b.Command, b.ConfigSuffix = d.Command, d.ConfigSuffix
		b.MappingPort, b.SocksPort = d.MappingPort, d.SocksPort
		switch p.Type {
		case model.TypeCustomOutbound:
			b.Core, b.ConfigSimple = "internal", d.OutboundJSON
		case model.TypeCustomConfig:
			b.Core, b.ConfigSimple = "internal-full", d.FullConfigJSON
		default: // external: bean хранит имя внешнего ядра в core
			b.Core, b.ConfigSimple = d.ExternalCoreName, d.ConfigTemplate
		}
	case *model.ChainDetail:
		b.List = d.ProfileIDs
	default:
		return nil, fmt.Errorf("тип %q не поддерживается neko-ссылкой", p.Type)
	}
	return b, nil
}

// durationToSeconds — Duration-строка → int-секунды (формат bean NekoBox;
// дробные округляются).
func durationToSeconds(s string) (int, error) {
	d, err := time.ParseDuration(s)
	if err != nil {
		return 0, fmt.Errorf("hop_interval: некорректная длительность %q (пример: 30s)", s)
	}
	return int(d.Seconds() + 0.5), nil
}

func profileFromBean(typ string, b *nekoBean) (*model.Profile, error) {
	const scheme = "nekoray"
	p := &model.Profile{Name: b.Name, Address: strings.TrimSpace(b.Addr), Port: b.Port}
	if b.Stream != nil {
		ns := b.Stream
		p.Stream = &model.StreamSettings{
			Network: ns.Network, HeaderType: ns.HeaderType, Path: ns.Path, Host: ns.Host,
			WSEarlyDataHeader: ns.EdName, WSEarlyDataMax: ns.EdLen,
			Security: ns.Security, SNI: ns.SNI, ALPN: ns.ALPN,
			AllowInsecure: ns.Insecure, Certificate: ns.Certificate,
			UTLSFingerprint: ns.UTLS, RealityPublicKey: ns.Pbk,
			RealityShortID: ns.Sid, RealitySPX: ns.Spx,
			PacketEncoding: ns.PacketEncoding, Multiplex: ns.MuxS,
		}
		if p.Stream.Network == "h2" {
			p.Stream.Network = "http"
		}
	}
	needAddrPort := true
	switch typ {
	case "socks", "http":
		proto := "socks5"
		p.Type = model.TypeSOCKS
		if typ == "http" {
			p.Type, proto = model.TypeHTTP, "http"
		} else if b.SocksType == 4 {
			proto = "socks4"
		}
		p.Detail = &model.SocksHTTPDetail{Proto: proto, Username: b.Username, Password: b.Password}
	case "shadowsocks":
		p.Type = model.TypeShadowsocks
		p.Detail = &model.ShadowsocksDetail{Method: b.Method, Password: b.Pass, Plugin: b.Plugin, UDPOverTCP: b.Uot}
		if !ssMethods[b.Method] {
			return nil, fmt.Errorf("%s: ss-метод %q не поддерживается", scheme, b.Method)
		}
		if b.Pass == "" {
			return nil, fmt.Errorf("%s: ss-пароль пуст", scheme)
		}
	case "vmess":
		p.Type = model.TypeVMess
		if b.Sec != "" && !vmessSecurities[b.Sec] {
			return nil, fmt.Errorf("%s: vmess-шифрование %q не поддерживается", scheme, b.Sec)
		}
		p.Detail = &model.VMessDetail{UUID: b.VMessID, AlterID: b.Aid, Security: b.Sec}
		if b.VMessID == "" {
			return nil, fmt.Errorf("%s: vmess uuid пуст", scheme)
		}
	case "trojan", "vless":
		p.Type = model.TypeTrojan
		if typ == "vless" {
			p.Type = model.TypeVLESS
			if b.Flow != "" && !vlessFlows[b.Flow] {
				return nil, fmt.Errorf("%s: vless flow %q не поддерживается", scheme, b.Flow)
			}
		}
		p.Detail = &model.TrojanVlessDetail{Password: b.Pass, Flow: b.Flow}
		if b.Pass == "" {
			return nil, fmt.Errorf("%s: %s: пароль/uuid пуст", scheme, typ)
		}
	case "naive":
		p.Type = model.TypeNaive
		p.Detail = &model.NaiveDetail{
			Username: b.Username, Password: b.Password, Proto: b.Protocol,
			ExtraHeaders: b.ExtraHeaders, SNI: b.QUICSNI, Certificate: b.Certificate,
			InsecureConcurrency: b.InsecureConc, DisableLogs: b.DisableLog,
		}
	case "hysteria2":
		p.Type = model.TypeHysteria2
		hop := ""
		if b.HopIntervalSec > 0 {
			hop = fmt.Sprintf("%ds", b.HopIntervalSec)
		}
		p.Detail = &model.Hysteria2Detail{
			Password: b.Password, ObfsPassword: b.ObfsPassword,
			UploadMbps: b.UploadMbps, DownloadMbps: b.DownloadMbps,
			ServerPorts: b.HopPort, HopInterval: hop,
			SNI: b.QUICSNI, ALPN: b.ALPN, CA: b.CaText,
			AllowInsecure: b.AllowInsecure, DisableSNI: b.DisableSni,
		}
		if b.Password == "" {
			return nil, fmt.Errorf("%s: hysteria2-пароль пуст", scheme)
		}
		if err := validateServerPorts(b.HopPort); err != nil {
			return nil, fmt.Errorf("%s: %v", scheme, err)
		}
	case "tuic":
		p.Type = model.TypeTUIC
		if !tuicCongestions[b.Congestion] {
			return nil, fmt.Errorf("%s: tuic congestion_control %q не поддерживается", scheme, b.Congestion)
		}
		if !tuicRelayModes[b.UDPRelayMode] {
			return nil, fmt.Errorf("%s: tuic udp_relay_mode %q не поддерживается", scheme, b.UDPRelayMode)
		}
		p.Detail = &model.TUICDetail{
			UUID: b.TUICUUID, Password: b.Password,
			CongestionControl: b.Congestion, UDPRelayMode: b.UDPRelayMode,
			ZeroRttHandshake: b.ZeroRtt, Heartbeat: b.Heartbeat,
			SNI: b.QUICSNI, ALPN: b.ALPN, CA: b.CaText,
			AllowInsecure: b.AllowInsecure, DisableSNI: b.DisableSni,
			UDPOverStream: b.Uos,
		}
		if b.TUICUUID == "" || b.Password == "" {
			return nil, fmt.Errorf("%s: tuic uuid/пароль пуст", scheme)
		}
		if b.Heartbeat != "" {
			if err := validateDuration("heartbeat", b.Heartbeat); err != nil {
				return nil, fmt.Errorf("%s: %v", scheme, err)
			}
		}
	case "custom":
		d := &model.CustomDetail{Command: b.Command, ConfigSuffix: b.ConfigSuffix,
			MappingPort: b.MappingPort, SocksPort: b.SocksPort}
		switch b.Core {
		case "internal":
			p.Type, d.Core, d.OutboundJSON = model.TypeCustomOutbound, "internal", b.ConfigSimple
		case "internal-full":
			p.Type, d.Core, d.FullConfigJSON = model.TypeCustomConfig, "internal-full", b.ConfigSimple
		default:
			p.Type, d.Core = model.TypeCustomExternal, "external"
			d.ExternalCoreName, d.ConfigTemplate = b.Core, b.ConfigSimple
		}
		p.Detail = d
		needAddrPort = false
	case "chain":
		p.Type = model.TypeChain
		p.Detail = &model.ChainDetail{ProfileIDs: b.List}
		needAddrPort = false
	default:
		return nil, fmt.Errorf("%s: неизвестный тип профиля %q", scheme, typ)
	}
	if p.Stream != nil {
		st := p.Stream
		if st.Network != "" && !networks[st.Network] {
			return nil, fmt.Errorf("%s: сеть %q не поддерживается", scheme, st.Network)
		}
		switch st.Security {
		case "", "tls", "reality":
		default:
			return nil, fmt.Errorf("%s: security %q не поддерживается", scheme, st.Security)
		}
		if st.UTLSFingerprint != "" && !utlsFPs[st.UTLSFingerprint] {
			return nil, fmt.Errorf("%s: отпечаток uTLS %q не поддерживается", scheme, st.UTLSFingerprint)
		}
		// канонизация REALITY: pbk непусто ⇒ security=reality (как в share-парсере)
		if st.RealityPublicKey != "" {
			st.Security = "reality"
		}
		if err := validateStreamCaps(scheme, st); err != nil {
			return nil, err
		}
	}
	if needAddrPort {
		if p.Address == "" {
			return nil, fmt.Errorf("%s: не указан адрес сервера", scheme)
		}
		if !validPort(p.Port) {
			return nil, fmt.Errorf("%s: порт %d вне диапазона 1–65535", scheme, p.Port)
		}
	}
	if len(p.Name) > maxRemarkLength {
		return nil, fmt.Errorf("%s: имя длиннее %d байт", scheme, maxRemarkLength)
	}
	return p, nil
}
