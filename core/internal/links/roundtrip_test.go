// SPDX-License-Identifier: GPL-3.0-only

package links

import (
	"reflect"
	"strings"
	"testing"

	"github.com/anatolijlaptev1991-ctrl/lavray-android/core/internal/model"
)

// canon приводит профиль к канонической форме: сеть tcp ≡ "" (дефолт),
// REALITY ⇔ pbk, scy "" ≡ auto, proto socks "" ≡ socks5, tcp/h2-алиасы,
// тримминг адреса/SNI, SNI-наследование из host при IP (§4.1). Пустой
// Stream после нормализации ≡ nil.
func canon(t *testing.T, p *model.Profile) *model.Profile {
	t.Helper()
	c, err := p.Clone()
	if err != nil {
		t.Fatalf("clone: %v", err)
	}
	c.ID, c.GroupID = 0, 0
	c.Address = canonicalHost(strings.TrimSpace(c.Address))
	if c.Stream != nil {
		st := c.Stream
		if st.Network == "tcp" || st.Network == "h2" {
			st.Network = ""
		}
		if st.RealityPublicKey != "" {
			st.Security = "reality"
		}
		if st.Security == "reality" && st.RealityPublicKey == "" {
			st.Security = "tls"
		}
		st.SNI = strings.TrimSpace(st.SNI)
		st.ALPN = normalizeALPN(st.ALPN)
		if *st == (model.StreamSettings{}) {
			c.Stream = nil
		}
	}
	sniFromHost(c)
	switch d := c.Detail.(type) {
	case *model.VMessDetail:
		if d.Security == "" {
			d.Security = "auto"
		}
	case *model.SocksHTTPDetail:
		if d.Proto == "" {
			d.Proto = "socks5"
		}
	}
	return c
}

// assertRoundtripShare — Parse(Build(p)) == p (каноническая форма).
func assertRoundtripShare(t *testing.T, o Options, p *model.Profile) {
	t.Helper()
	link, err := BuildLinkWithOptions(p, o)
	if err != nil {
		t.Fatalf("BuildLink(%s): %v", p.Type, err)
	}
	got, err := ParseLink(link)
	if err != nil {
		t.Fatalf("ParseLink(%s): %v\nссылка: %s", link, err, link)
	}
	want := canon(t, p)
	have := canon(t, got)
	if !reflect.DeepEqual(want, have) {
		t.Fatalf("roundtrip %s:\nссылка: %s\nwant: %+v\nhave: %+v", p.Type, link, want, have)
	}
}

func assertRoundtripNeko(t *testing.T, p *model.Profile) {
	t.Helper()
	link, err := BuildNekoLink(p)
	if err != nil {
		t.Fatalf("BuildNekoLink(%s): %v", p.Type, err)
	}
	got, err := ParseLink(link)
	if err != nil {
		t.Fatalf("ParseLink(neko %s): %v\nссылка: %s", p.Type, err, link)
	}
	want := canon(t, p)
	have := canon(t, got)
	if !reflect.DeepEqual(want, have) {
		t.Fatalf("neko-roundtrip %s:\nссылка: %s\nwant: %+v\nhave: %+v", p.Type, link, want, have)
	}
}

func longRemark() string { return strings.Repeat("тест узел №5 🚀 ", 120) } // 1200+ байт

// TestRoundtripShareMatrix — матрица типы × транспорты × tls/reality (W2:
// «roundtrip-матрица», core-port-plan §4.3).
func TestRoundtripShareMatrix(t *testing.T) {
	streams := map[string]*model.StreamSettings{
		"plain":       nil,
		"tcp":         {Network: "tcp"},
		"tcp-http":    {Network: "tcp", HeaderType: "http", Host: "h.io", Path: "/hp"},
		"ws-tls":      {Network: "ws", Path: "/ws", Host: "w.io", Security: "tls", SNI: "w.io", ALPN: "h2,http/1.1"},
		"grpc":        {Network: "grpc", Path: "svc-name", Security: "tls", SNI: "g.io", UTLSFingerprint: "chrome"},
		"hup":         {Network: "httpupgrade", Path: "/up", Host: "u.io"},
		"http-net":    {Network: "http", Path: "/h2p", Host: "a.io,b.io"},
		"reality":     {Network: "tcp", Security: "reality", RealityPublicKey: "pbk-key", RealityShortID: "6ba85179", UTLSFingerprint: "chrome"},
		"ws-insecure": {Network: "ws", Path: "/w2", Host: "w2.io", Security: "tls", AllowInsecure: true},
	}

	profiles := []*model.Profile{
		// socks / http
		{Type: model.TypeSOCKS, Name: "s5", Address: "1.2.3.4", Port: 1080,
			Detail: &model.SocksHTTPDetail{Proto: "socks5"}},
		{Type: model.TypeSOCKS, Name: "спецсимволы u:p@/%", Address: "s5.io", Port: 1080,
			Detail: &model.SocksHTTPDetail{Proto: "socks5", Username: "u:ser@нэйм", Password: "p@ss:word/+%~"}},
		{Type: model.TypeSOCKS, Name: "s4", Address: "s4.io", Port: 1080,
			Detail: &model.SocksHTTPDetail{Proto: "socks4"}},
		{Type: model.TypeSOCKS, Name: "socks+tls", Address: "st.io", Port: 443,
			Stream: &model.StreamSettings{Security: "tls", SNI: "st.io"},
			Detail: &model.SocksHTTPDetail{Proto: "socks5", Username: "u", Password: "p"}},
		{Type: model.TypeHTTP, Name: "http", Address: "h1.io", Port: 8080,
			Detail: &model.SocksHTTPDetail{Proto: "http"}},
		{Type: model.TypeHTTP, Name: "https", Address: "h2.io", Port: 443,
			Stream: &model.StreamSettings{Security: "tls", SNI: "h2.io"},
			Detail: &model.SocksHTTPDetail{Proto: "http", Username: "u", Password: "p"}},

		// shadowsocks
		{Type: model.TypeShadowsocks, Name: "ss традиционный", Address: "ss1.io", Port: 8388,
			Detail: &model.ShadowsocksDetail{Method: "aes-256-gcm", Password: "простойпароль"}},
		{Type: model.TypeShadowsocks, Name: "ss спецсимволы", Address: "ss2.io", Port: 8388,
			Detail: &model.ShadowsocksDetail{Method: "chacha20-ietf-poly1305", Password: "p@ss:word/+%~"}},
		{Type: model.TypeShadowsocks, Name: "ss 2022", Address: "ss3.io", Port: 8388,
			Detail: &model.ShadowsocksDetail{Method: "2022-blake3-aes-128-gcm", Password: "bmVrby1rZXk="}},
		{Type: model.TypeShadowsocks, Name: "ss plugin", Address: "ss4.io", Port: 8388,
			Detail: &model.ShadowsocksDetail{Method: "aes-128-gcm", Password: "pw", Plugin: "obfs-local;obfs=http;obfs-host=w.io"}},
		{Type: model.TypeShadowsocks, Name: longRemark(), Address: "ss5.io", Port: 8388,
			Detail: &model.ShadowsocksDetail{Method: "rc4-md5", Password: "pw"}},

		// trojan / vless × транспорта
		{Type: model.TypeTrojan, Name: "tj", Address: "t.io", Port: 443,
			Detail: &model.TrojanVlessDetail{Password: "pw"}},
		{Type: model.TypeTrojan, Name: "tj-спец", Address: "t.io", Port: 443,
			Detail: &model.TrojanVlessDetail{Password: "p@ss:word/+%~"}},
		{Type: model.TypeVLESS, Name: "vl", Address: "v.io", Port: 443,
			Detail: &model.TrojanVlessDetail{Password: "uuid-vl"}},
		{Type: model.TypeVLESS, Name: "vl-flow", Address: "v.io", Port: 443,
			Detail: &model.TrojanVlessDetail{Password: "uuid-vl", Flow: "xtls-rprx-vision"}},
	}
	for name, st := range streams {
		profiles = append(profiles,
			&model.Profile{Type: model.TypeVLESS, Name: "vl-" + name, Address: "vl-" + name + ".io", Port: 443,
				Stream: st, Detail: &model.TrojanVlessDetail{Password: "uuid-" + name}},
			&model.Profile{Type: model.TypeTrojan, Name: "tj-" + name, Address: "tj-" + name + ".io", Port: 443,
				Stream: cloneStream(st), Detail: &model.TrojanVlessDetail{Password: "pw-" + name}},
		)
	}

	// hysteria2 / tuic
	profiles = append(profiles,
		&model.Profile{Type: model.TypeHysteria2, Name: "hy2", Address: "hy.io", Port: 443,
			Detail: &model.Hysteria2Detail{Password: "pw"}},
		&model.Profile{Type: model.TypeHysteria2, Name: "hy2 полный", Address: "hy.io", Port: 8443,
			Detail: &model.Hysteria2Detail{
				Password: "user:pass", ObfsPassword: "obfs-pw", UploadMbps: 100, DownloadMbps: 200,
				ServerPorts: "20000:30000", HopInterval: "30s", SNI: "hy.io", ALPN: "h3", AllowInsecure: true}},
		&model.Profile{Type: model.TypeHysteria2, Name: "hy2 спецпароль", Address: "hy.io", Port: 443,
			Detail: &model.Hysteria2Detail{Password: "p@ss/word~+", DisableSNI: true}},
		&model.Profile{Type: model.TypeHysteria2, Name: "hy2 ipv6", Address: "2001:db8::1", Port: 443,
			Detail: &model.Hysteria2Detail{Password: "pw", SNI: "hy6.io"}},
		&model.Profile{Type: model.TypeTUIC, Name: "tuic", Address: "tq.io", Port: 443,
			Detail: &model.TUICDetail{UUID: "uuid", Password: "pw"}},
		&model.Profile{Type: model.TypeTUIC, Name: "tuic полный", Address: "tq.io", Port: 8443,
			Detail: &model.TUICDetail{
				UUID: "uuid-1", Password: "p@ss:word", CongestionControl: "bbr", UDPRelayMode: "native",
				ZeroRttHandshake: true, Heartbeat: "10s", SNI: "tq.io", ALPN: "h3", AllowInsecure: true}},
		&model.Profile{Type: model.TypeTUIC, Name: "tuic cubic/quic", Address: "tq.io", Port: 443,
			Detail: &model.TUICDetail{UUID: "u", Password: "p", CongestionControl: "cubic", UDPRelayMode: "quic", DisableSNI: true}},
	)

	for _, p := range profiles {
		assertRoundtripShare(t, Options{}, p) // vmess — v2rayN (дефолт, FR-UI-164)
		assertRoundtripShare(t, Options{VMessURLFormat: true}, p)
		assertRoundtripNeko(t, p)
	}
}

func cloneStream(st *model.StreamSettings) *model.StreamSettings {
	if st == nil {
		return nil
	}
	c := *st
	return &c
}

// TestRoundtripVMessFormats — оба формата vmess (FR-UI-164): дефолт
// v2rayN base64-JSON и URL-формат XTLS.
func TestRoundtripVMessFormats(t *testing.T) {
	base := func(st *model.StreamSettings) *model.Profile {
		return &model.Profile{Type: model.TypeVMess, Name: "vm-«кавычки» & спец", Address: "vm.io",
			Port: 443, Stream: st, Detail: &model.VMessDetail{UUID: "b831381d-6324-4d53-ad4f-8cda48b30811", Security: "auto"}}
	}
	profiles := []*model.Profile{
		base(nil),
		base(&model.StreamSettings{Network: "ws", Path: "/ws", Host: "vm.io", Security: "tls", SNI: "vm.io"}),
		base(&model.StreamSettings{Network: "grpc", Path: "svc", Security: "tls", SNI: "vm.io", UTLSFingerprint: "chrome"}),
		base(&model.StreamSettings{Network: "tcp", HeaderType: "http", Host: "vm.io", Path: "/hp"}),
		{Type: model.TypeVMess, Name: "vm aid+scy", Address: "vm2.io", Port: 8443,
			Detail: &model.VMessDetail{UUID: "u2", AlterID: 64, Security: "zero"}},
		{Type: model.TypeVMess, Name: "vm ipv6", Address: "2001:db8:0:0:0:0:0:1", Port: 443,
			Detail: &model.VMessDetail{UUID: "u3"}},
	}
	for _, p := range profiles {
		assertRoundtripShare(t, Options{}, p)
		// aid≠0 выразим только в формате v2rayN — URL-формат XTLS #716
		// рассчитан на VMess AEAD (aid всегда 0).
		if p.Detail.(*model.VMessDetail).AlterID == 0 {
			assertRoundtripShare(t, Options{VMessURLFormat: true}, p)
		}
		assertRoundtripNeko(t, p)
	}
	// дефолтный формат — v2rayN base64 JSON
	link, err := BuildLink(base(nil))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(link, "vmess://") || strings.Contains(link, "@") {
		t.Fatalf("дефолтный формат vmess должен быть base64 JSON (FR-UI-164): %s", link)
	}
	// URL-формат
	link2, err := BuildLinkWithOptions(base(nil), Options{VMessURLFormat: true})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(link2, "vmess://b831381d-6324-4d53-ad4f-8cda48b30811@vm.io:443?") {
		t.Fatalf("URL-формат vmess: %s", link2)
	}
}

// TestRoundtripNekoLinkAllTypes — neko-ссылка (FR-UI-191) roundtrip для ВСЕХ
// типов, включая custom/chain и поля, недоступные share-ссылкам (mux,
// packet_encoding, ws early data, сертификат).
func TestRoundtripNekoLinkAllTypes(t *testing.T) {
	fullStream := &model.StreamSettings{
		Network: "ws", Path: "/ws", Host: "w.io",
		WSEarlyDataHeader: "Sec-WebSocket-Protocol", WSEarlyDataMax: 2048,
		Security: "tls", SNI: "w.io", ALPN: "h2,http/1.1", AllowInsecure: true,
		Certificate: "-----BEGIN CERTIFICATE-----", UTLSFingerprint: "chrome",
		RealityPublicKey: "pbk", RealityShortID: "sid",
		PacketEncoding: "xudp", Multiplex: 1,
	}
	profiles := []*model.Profile{
		{Type: model.TypeSOCKS, Name: "s5", Address: "1.2.3.4", Port: 1080, Stream: fullStream,
			Detail: &model.SocksHTTPDetail{Proto: "socks5", Username: "u", Password: "p"}},
		{Type: model.TypeHTTP, Name: "http", Address: "h.io", Port: 8080,
			Detail: &model.SocksHTTPDetail{Proto: "http", Username: "u", Password: "p"}},
		{Type: model.TypeShadowsocks, Name: "ss", Address: "s.io", Port: 8388,
			Detail: &model.ShadowsocksDetail{Method: "2022-blake3-aes-128-gcm", Password: "k=", Plugin: "obfs-local;obfs=http", UDPOverTCP: 2}},
		{Type: model.TypeVMess, Name: "vm", Address: "v.io", Port: 443, Stream: cloneStream(fullStream),
			Detail: &model.VMessDetail{UUID: "uuid", AlterID: 64, Security: "chacha20-poly1305"}},
		{Type: model.TypeTrojan, Name: "tj", Address: "t.io", Port: 443, Stream: cloneStream(fullStream),
			Detail: &model.TrojanVlessDetail{Password: "pw"}},
		{Type: model.TypeVLESS, Name: "vl", Address: "vl.io", Port: 443, Stream: cloneStream(fullStream),
			Detail: &model.TrojanVlessDetail{Password: "uuid", Flow: "xtls-rprx-vision"}},
		{Type: model.TypeNaive, Name: "nv", Address: "n.io", Port: 443,
			Detail: &model.NaiveDetail{Username: "u", Password: "p", Proto: "https", SNI: "n.io",
				ExtraHeaders: "X-K: v", InsecureConcurrency: 2, DisableLogs: true}},
		{Type: model.TypeHysteria2, Name: "hy2", Address: "hy.io", Port: 443,
			Detail: &model.Hysteria2Detail{Password: "pw", ObfsPassword: "ob", UploadMbps: 100,
				DownloadMbps: 200, ServerPorts: "20000:30000", HopInterval: "30s",
				SNI: "hy.io", ALPN: "h3", CA: "ca-text", AllowInsecure: true, DisableSNI: true}},
		{Type: model.TypeTUIC, Name: "tu", Address: "tq.io", Port: 443,
			Detail: &model.TUICDetail{UUID: "u", Password: "p", CongestionControl: "bbr",
				UDPRelayMode: "quic", ZeroRttHandshake: true, Heartbeat: "10s",
				SNI: "tq.io", ALPN: "h3", CA: "ca-text", AllowInsecure: true, DisableSNI: true, UDPOverStream: true}},
		{Type: model.TypeCustomOutbound, Name: "co",
			Detail: &model.CustomDetail{Core: "internal", OutboundJSON: `{"type":"socks"}`}},
		{Type: model.TypeCustomConfig, Name: "cc",
			Detail: &model.CustomDetail{Core: "internal-full", FullConfigJSON: `{"inbounds":[]}`}},
		{Type: model.TypeCustomExternal, Name: "ce",
			Detail: &model.CustomDetail{Core: "external", ExternalCoreName: "naive",
				Command: []string{"naive", "--proxy"}, ConfigTemplate: "cfg", ConfigSuffix: ".json",
				MappingPort: 1234, SocksPort: 5678}},
		{Type: model.TypeChain, Name: "ch",
			Detail: &model.ChainDetail{ProfileIDs: []int64{4, 5, 6}}},
	}
	for _, p := range profiles {
		assertRoundtripNeko(t, p)
	}
}

// TestBuildLinkNotExportable — custom/chain/naive не экспортируются
// share-ссылкой (FR-UI-190), ошибка — понятная.
func TestBuildLinkNotExportable(t *testing.T) {
	for _, p := range []*model.Profile{
		{Type: model.TypeChain, Name: "ch", Detail: &model.ChainDetail{ProfileIDs: []int64{1}}},
		{Type: model.TypeCustomOutbound, Detail: &model.CustomDetail{Core: "internal"}},
		{Type: model.TypeCustomConfig, Detail: &model.CustomDetail{Core: "internal-full"}},
		{Type: model.TypeCustomExternal, Detail: &model.CustomDetail{Core: "external"}},
		{Type: model.TypeNaive, Address: "n.io", Port: 443, Detail: &model.NaiveDetail{Proto: "https"}},
	} {
		_, err := BuildLink(p)
		if err == nil || !strings.Contains(err.Error(), "не экспортируется") {
			t.Errorf("тип %s: ожидается ошибка «не экспортируется», got %v", p.Type, err)
		}
	}
}

// TestBuildLinkValidations — экспорт не выпускает невалидные поля (LNB-15/17).
func TestBuildLinkValidations(t *testing.T) {
	bad := []*model.Profile{
		{Type: model.TypeSOCKS, Address: "h.io", Port: 0,
			Detail: &model.SocksHTTPDetail{Proto: "socks5"}}, // порт 0
		{Type: model.TypeSOCKS, Address: "h.io", Port: 65536,
			Detail: &model.SocksHTTPDetail{Proto: "socks5"}},
		{Type: model.TypeSOCKS, Address: "", Port: 1080,
			Detail: &model.SocksHTTPDetail{Proto: "socks5"}}, // нет адреса
		{Type: model.TypeShadowsocks, Address: "h.io", Port: 8388,
			Detail: &model.ShadowsocksDetail{Method: "xyz", Password: "p"}},
		{Type: model.TypeShadowsocks, Address: "h.io", Port: 8388,
			Detail: &model.ShadowsocksDetail{Method: "aes-256-gcm"}},
		{Type: model.TypeVMess, Address: "h.io", Port: 443,
			Detail: &model.VMessDetail{UUID: "u", Security: "aes-256-gcm"}},
		{Type: model.TypeVLESS, Address: "h.io", Port: 443,
			Stream: &model.StreamSettings{Security: "reality"}, // reality без pbk
			Detail: &model.TrojanVlessDetail{Password: "u"}},
		{Type: model.TypeVLESS, Address: "h.io", Port: 443,
			Stream: &model.StreamSettings{Network: "kcp"},
			Detail: &model.TrojanVlessDetail{Password: "u"}},
		{Type: model.TypeHysteria2, Address: "h.io", Port: 443,
			Detail: &model.Hysteria2Detail{Password: "p", HopInterval: "bad"}},
		{Type: model.TypeHysteria2, Address: "h.io", Port: 443,
			Detail: &model.Hysteria2Detail{Password: "p", ServerPorts: "1:99999"}},
		{Type: model.TypeTUIC, Address: "h.io", Port: 443,
			Detail: &model.TUICDetail{UUID: "u", CongestionControl: "xyz"}},
	}
	for _, p := range bad {
		if _, err := BuildLink(p); err == nil {
			t.Errorf("тип %s port=%d: ожидалась ошибка валидации", p.Type, p.Port)
		}
	}
}
