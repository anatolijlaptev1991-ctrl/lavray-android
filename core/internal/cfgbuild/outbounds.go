// SPDX-License-Identifier: GPL-3.0-only

// outbounds.go — профиль модели → sing-box outbound JSON (порт
// fmt/Bean2CoreObj_box.cpp: SocksHttpBean/ShadowSocksBean/VMessBean/
// TrojanVLESSBean/QUICBean/CustomBean + V2rayStreamSettings).
//
// Отклонения от оракула фиксируются в testdata/deviations.yaml:
//   - hysteria2: имена полей sing-box 1.14 — server_ports/hop_interval
//     вместо бага оракула hop_ports/int (LNB-35, FR-UI-268);
//   - пустые строки не эмитятся (omitempty), оракул местами пишет "";
//   - stream-поля применяются только к типам, где sing-box их поддерживает
//     (у socks/ss в оракуле это мёртвый код — строгий парсер sing-box
//     такие поля отвергает).
package cfgbuild

import (
	"encoding/json"
	"net"
	"strconv"
	"strings"

	"github.com/anatolijlaptev1991-ctrl/lavray-android/core/internal/model"
)

// buildOutbound строит sing-box outbound для одиночного (нецепочечного)
// профиля без тега.
func buildOutbound(p *model.Profile, st *BuildSettings) (map[string]any, error) {
	switch p.Type {
	case model.TypeSOCKS, model.TypeHTTP:
		return buildSocksHTTP(p, st)
	case model.TypeShadowsocks:
		return buildShadowsocks(p)
	case model.TypeVMess:
		return buildVMess(p, st)
	case model.TypeTrojan, model.TypeVLESS:
		return buildTrojanVless(p, st)
	case model.TypeHysteria2:
		return buildHysteria2(p)
	case model.TypeTUIC:
		return buildTUIC(p)
	case model.TypeCustomOutbound:
		return buildCustomInternal(p)
	default:
		// naive/custom_external (внешние ядра — за флагом, W6), custom_config
		// (internal-full — отдельная ветка Build), chain (разрешается раньше).
		return nil, failedPrecondition("type", "unsupported outbound type %q", string(p.Type))
	}
}

// ---- SocksHttpBean ---------------------------------------------------------

func buildSocksHTTP(p *model.Profile, st *BuildSettings) (map[string]any, error) {
	d, ok := p.Detail.(*model.SocksHTTPDetail)
	if !ok || d == nil {
		return nil, invalidArgument("detail", "socks/http: missing or wrong detail type")
	}
	out := map[string]any{
		"type":        "socks",
		"server":      p.Address,
		"server_port": p.Port,
	}
	isHTTP := false
	switch d.Proto {
	case "http":
		out["type"] = "http"
		isHTTP = true
	case "socks4":
		out["version"] = "4"
	}
	if d.Username != "" && d.Password != "" {
		out["username"] = d.Username
		out["password"] = d.Password
	}
	// Оракул применяет stream ко всем типам, но socks в sing-box не имеет
	// tls/transport, http — только tls.
	if isHTTP {
		applyStreamSettings(out, p.Stream, st, streamCaps{tls: true})
	}
	return out, nil
}

// ---- ShadowSocksBean -------------------------------------------------------

func buildShadowsocks(p *model.Profile) (map[string]any, error) {
	d, ok := p.Detail.(*model.ShadowsocksDetail)
	if !ok || d == nil {
		return nil, invalidArgument("detail", "shadowsocks: missing or wrong detail type")
	}
	if d.Method == "" || d.Password == "" {
		return nil, invalidArgument("method", "shadowsocks: method and password are required")
	}
	out := map[string]any{
		"type":        "shadowsocks",
		"server":      p.Address,
		"server_port": p.Port,
		"method":      d.Method,
		"password":    d.Password,
	}
	if d.UDPOverTCP != 0 {
		out["udp_over_tcp"] = map[string]any{"enabled": true, "version": d.UDPOverTCP}
	} else {
		out["udp_over_tcp"] = false
	}
	if plugin := strings.TrimSpace(d.Plugin); plugin != "" {
		// «plugin;arg=val;…» → plugin + plugin_opts (всё после первой «;»).
		name, opts, _ := strings.Cut(plugin, ";")
		out["plugin"] = name
		if opts != "" {
			out["plugin_opts"] = opts
		}
	}
	return out, nil
}

// ---- VMessBean -------------------------------------------------------------

func buildVMess(p *model.Profile, st *BuildSettings) (map[string]any, error) {
	d, ok := p.Detail.(*model.VMessDetail)
	if !ok || d == nil {
		return nil, invalidArgument("detail", "vmess: missing or wrong detail type")
	}
	if strings.TrimSpace(d.UUID) == "" {
		return nil, invalidArgument("uuid", "vmess: uuid is required")
	}
	out := map[string]any{
		"type":        "vmess",
		"server":      p.Address,
		"server_port": p.Port,
		"uuid":        strings.TrimSpace(d.UUID),
		"alter_id":    d.AlterID, // оракул эмитит всегда (0 включительно)
	}
	if d.Security != "" {
		out["security"] = d.Security
	}
	applyStreamSettings(out, p.Stream, st, streamCaps{transport: true, tls: true, packetEncoding: true})
	return out, nil
}

// ---- TrojanVLESSBean -------------------------------------------------------

func buildTrojanVless(p *model.Profile, st *BuildSettings) (map[string]any, error) {
	d, ok := p.Detail.(*model.TrojanVlessDetail)
	if !ok || d == nil {
		return nil, invalidArgument("detail", "trojan/vless: missing or wrong detail type")
	}
	vless := p.Type == model.TypeVLESS
	out := map[string]any{
		"type":        "trojan",
		"server":      p.Address,
		"server_port": p.Port,
	}
	if vless {
		out["type"] = "vless"
		if strings.TrimSpace(d.Password) == "" {
			return nil, invalidArgument("uuid", "vless: uuid is required")
		}
		out["uuid"] = strings.TrimSpace(d.Password)
		flow := strings.TrimSuffix(d.Flow, "-udp443") // суффикс срезается (оракул)
		if flow == "none" {
			flow = ""
		}
		if flow != "" {
			out["flow"] = flow
		}
	} else {
		if d.Password == "" {
			return nil, invalidArgument("password", "trojan: password is required")
		}
		out["password"] = d.Password
	}
	applyStreamSettings(out, p.Stream, st, streamCaps{transport: true, tls: true, packetEncoding: vless})
	return out, nil
}

// ---- QUICBean: hysteria2 / tuic --------------------------------------------

// quicTLS — общий TLS-блок QUICBean (hysteria2/tuic).
func quicTLS(disableSNI, allowInsecure bool, ca, sni, alpn string, hysteria2 bool) map[string]any {
	tls := map[string]any{
		"enabled":     true,
		"disable_sni": disableSNI,
		"insecure":    allowInsecure,
	}
	if s := strings.TrimSpace(ca); s != "" {
		tls["certificate"] = s
	}
	if sni != "" {
		tls["server_name"] = sni
	}
	switch {
	case hysteria2:
		// Оракул безусловно переопределяет alpn на "h3" для hysteria2.
		tls["alpn"] = "h3"
	case strings.TrimSpace(alpn) != "":
		tls["alpn"] = splitCSV(alpn)
	}
	return tls
}

func buildHysteria2(p *model.Profile) (map[string]any, error) {
	d, ok := p.Detail.(*model.Hysteria2Detail)
	if !ok || d == nil {
		return nil, invalidArgument("detail", "hysteria2: missing or wrong detail type")
	}
	out := map[string]any{
		"type":        "hysteria2",
		"server":      p.Address,
		"server_port": p.Port,
		"tls":         quicTLS(d.DisableSNI, d.AllowInsecure, d.CA, d.SNI, d.ALPN, true),
	}
	if d.Password != "" {
		out["password"] = d.Password
	}
	if d.UploadMbps > 0 {
		out["up_mbps"] = d.UploadMbps
	}
	if d.DownloadMbps > 0 {
		out["down_mbps"] = d.DownloadMbps
	}
	// LNB-35/FR-UI-268: имена полей sing-box 1.14 — server_ports (список
	// диапазонов «a:b», D-01) + hop_interval (Duration-строка); оракул эмитил
	// hop_ports + int — баг, не переносим (deviations.yaml, вектор G14).
	ports, err := hysteria2ServerPorts(d.ServerPorts)
	if err != nil {
		return nil, err
	}
	if len(ports) > 0 {
		out["server_ports"] = ports
		if hi := strings.TrimSpace(d.HopInterval); hi != "" {
			out["hop_interval"] = hi
		}
	}
	if d.ObfsPassword != "" {
		out["obfs"] = map[string]any{"type": "salamander", "password": d.ObfsPassword}
	}
	return out, nil
}

func buildTUIC(p *model.Profile) (map[string]any, error) {
	d, ok := p.Detail.(*model.TUICDetail)
	if !ok || d == nil {
		return nil, invalidArgument("detail", "tuic: missing or wrong detail type")
	}
	out := map[string]any{
		"type":        "tuic",
		"server":      p.Address,
		"server_port": p.Port,
		"tls":         quicTLS(d.DisableSNI, d.AllowInsecure, d.CA, d.SNI, d.ALPN, false),
	}
	if d.UUID != "" {
		out["uuid"] = d.UUID
	}
	if d.Password != "" {
		out["password"] = d.Password
	}
	if d.CongestionControl != "" {
		out["congestion_control"] = d.CongestionControl
	}
	if d.UDPOverStream {
		out["udp_over_stream"] = true
	} else if d.UDPRelayMode != "" {
		out["udp_relay_mode"] = d.UDPRelayMode
	}
	if d.ZeroRttHandshake {
		out["zero_rtt_handshake"] = true
	}
	if hb := strings.TrimSpace(d.Heartbeat); hb != "" {
		out["heartbeat"] = hb
	}
	return out, nil
}

// ---- CustomBean core=internal ----------------------------------------------

func buildCustomInternal(p *model.Profile) (map[string]any, error) {
	d, ok := p.Detail.(*model.CustomDetail)
	if !ok || d == nil {
		return nil, invalidArgument("detail", "custom: missing or wrong detail type")
	}
	raw := strings.TrimSpace(d.OutboundJSON)
	if raw == "" {
		return nil, invalidArgument("custom", "custom internal profile: outbound JSON is empty")
	}
	var out map[string]any
	if err := json.Unmarshal([]byte(raw), &out); err != nil {
		return nil, invalidArgument("custom", "custom internal profile: outbound JSON: %v", err)
	}
	// T-08: outbound tor запускает executable_path, ssh читает
	// private_key_path, tls — certificate_path: пути из чужого JSON не нужны.
	if err := rejectFileKeys("custom", out); err != nil {
		return nil, err
	}
	return out, nil
}

// ---- V2rayStreamSettings → transport/tls/packet_encoding -------------------

// streamCaps — что из stream-настроек поддерживает тип outbound'а.
type streamCaps struct {
	transport      bool
	tls            bool
	packetEncoding bool // vmess/vless
}

func applyStreamSettings(out map[string]any, s *model.StreamSettings, st *BuildSettings, caps streamCaps) {
	if s == nil {
		return
	}
	transportType := ""
	if caps.transport {
		if tr := buildTransport(s); tr != nil {
			out["transport"] = tr
			transportType, _ = tr["type"].(string)
		}
	}
	// L10 аудита №1: непустой RealityPublicKey сам по себе включает reality.
	// Раньше профиль с RealityPublicKey, но пустым Security, уезжал БЕЗ tls —
	// открытый текст и утечка UUID. Security=reality без ключа — ошибка
	// валидации, а не молчаливый plaintext.
	if caps.tls {
		realityRequested := s.Security == "reality" || s.RealityPublicKey != ""
		if s.Security == "tls" || realityRequested {
			if tls := buildTLS(s, st, transportType); len(tls) > 0 {
				out["tls"] = tls
			}
		}
	}
	if caps.packetEncoding && s.PacketEncoding != "" {
		out["packet_encoding"] = s.PacketEncoding
	}
}

// buildTransport — порт V2rayStreamSettings::BuildStreamSettingsSingBox
// (ветка network != tcp + ветка tcp header http).
func buildTransport(s *model.StreamSettings) map[string]any {
	if s.Network == "" || s.Network == "tcp" {
		if s.HeaderType == "http" {
			return map[string]any{
				"type":    "http",
				"method":  "GET",
				"path":    s.Path,
				"headers": map[string]any{"Host": splitCSV(s.Host)},
			}
		}
		return nil
	}
	tr := map[string]any{"type": s.Network}
	switch s.Network {
	case "ws":
		if s.Host != "" {
			tr["headers"] = map[string]any{"Host": s.Host}
		}
		// «/path?ed=2048»: путь до ?ed= + max_early_data из суффикса.
		pathNoEd := s.Path
		edMax := 0
		if i := strings.Index(s.Path, "?ed="); i >= 0 {
			pathNoEd = s.Path[:i]
			if v, err := strconv.Atoi(s.Path[i+4:]); err == nil && v > 0 {
				edMax = v
			}
		}
		if pathNoEd != "" {
			tr["path"] = pathNoEd
		}
		if edMax > 0 {
			tr["max_early_data"] = edMax
			tr["early_data_header_name"] = "Sec-WebSocket-Protocol"
		}
		// Явные ws early data перекрывают значения из ?ed= (второй if оракула).
		if s.WSEarlyDataMax > 0 {
			tr["max_early_data"] = s.WSEarlyDataMax
			if s.WSEarlyDataHeader != "" {
				tr["early_data_header_name"] = s.WSEarlyDataHeader
			}
		}
	case "http": // h2
		if s.Path != "" {
			tr["path"] = s.Path
		}
		if s.Host != "" {
			tr["host"] = splitCSV(s.Host)
		}
	case "grpc":
		if s.Path != "" {
			tr["service_name"] = s.Path
		}
	case "httpupgrade":
		if s.Path != "" {
			tr["path"] = s.Path
		}
		if s.Host != "" {
			tr["host"] = s.Host
		}
		// quic: только type.
	}
	return tr
}

// buildTLS — ветка security == "tls" | "reality" (у модели reality —
// самостоятельное значение security при непустом pbk, FR-UI-090i).
func buildTLS(s *model.StreamSettings, st *BuildSettings, transportType string) map[string]any {
	tls := map[string]any{"enabled": true}
	if s.AllowInsecure || st.AllowInsecureDefault {
		tls["insecure"] = true
	}
	if sni := strings.TrimSpace(s.SNI); sni != "" {
		tls["server_name"] = sni
	}
	if cert := strings.TrimSpace(s.Certificate); cert != "" {
		tls["certificate"] = cert
	}
	reality := strings.TrimSpace(s.RealityPublicKey) != ""
	if alpn := alpnForTransport(transportType, splitCSV(strings.TrimSpace(s.ALPN)), reality); len(alpn) > 0 {
		tls["alpn"] = alpn
	}
	fp := s.UTLSFingerprint
	if pbk := strings.TrimSpace(s.RealityPublicKey); pbk != "" {
		reality := map[string]any{"enabled": true, "public_key": pbk}
		// sid может быть списком через запятую — берём первый (оракул).
		if sid := strings.TrimSpace(s.RealityShortID); sid != "" {
			reality["short_id"] = strings.SplitN(sid, ",", 2)[0]
		}
		tls["reality"] = reality
		if fp == "" {
			fp = "random"
		}
	}
	if fp != "" {
		tls["utls"] = map[string]any{"enabled": true, "fingerprint": fp}
	}
	return tls
}

// alpnForTransport приводит ALPN из ссылки к тому, что транспорт реально
// говорит по проводу (0.2.1). Клиент WebSocket и httpupgrade в sing-box всегда
// поднимает апгрейд как HTTP/1.1 и ставит ALPN http/1.1 лишь при пустом списке,
// а uTLS подменяет расширение на заданные протоколы. Ссылка с alpn=h2 (панели
// ставят её на ws по умолчанию) договаривалась о h2, апгрейд HTTP/1.1 шёл по
// соединению h2, и сервер его рвал, хотя ядро стартовало и интерфейс писал
// «Подключено». Xray для WebSocket всегда шлёт http/1.1.
//   - ws, httpupgrade: h2 и h3 убираются; если ничего не осталось, ALPN не
//     задаётся (sing-box поставит http/1.1);
//   - grpc, http (h2-транспорт): заданный ALPN без h2 заменяется на ["h2"];
//   - quic, tcp без обфускации и REALITY: как в ссылке.
func alpnForTransport(transportType string, list []any, reality bool) []any {
	if reality || len(list) == 0 {
		return list
	}
	switch transportType {
	case "ws", "httpupgrade":
		kept := make([]any, 0, len(list))
		for _, p := range list {
			if p != "h2" && p != "h3" {
				kept = append(kept, p)
			}
		}
		return kept
	case "grpc", "http":
		for _, p := range list {
			if p == "h2" {
				return list
			}
		}
		return []any{"h2"}
	}
	return list
}

// splitCSV — «a,b,c» → ["a","b","c"] (без пустых элементов).
func splitCSV(s string) []any {
	parts := strings.Split(s, ",")
	out := make([]any, 0, len(parts))
	for _, p := range parts {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

// isIPAddr — адрес является IP-литералом (для dns-direct full: списка, W5).
func isIPAddr(s string) bool {
	return net.ParseIP(strings.TrimSpace(s)) != nil
}
