// SPDX-License-Identifier: GPL-3.0-only

package links

import (
	"encoding/base64"
	"strings"
	"testing"

	"github.com/anatolijlaptev1991-ctrl/lavray-android/core/internal/model"
)

func b64std(s string) string { return base64.StdEncoding.EncodeToString([]byte(s)) }

func TestParseLinkPositive(t *testing.T) {
	tests := []struct {
		name  string
		link  string
		check func(*testing.T, *model.Profile)
	}{
		{
			name: "ss традиционный (base64url userinfo)",
			link: "ss://" + encodeB64URL("aes-256-gcm:password") + "@1.2.3.4:8388#SS%20узел",
			check: func(t *testing.T, p *model.Profile) {
				d, ok := p.Detail.(*model.ShadowsocksDetail)
				if !ok {
					t.Fatalf("detail = %T", p.Detail)
				}
				if d.Method != "aes-256-gcm" || d.Password != "password" {
					t.Errorf("method/password = %q/%q", d.Method, d.Password)
				}
				if p.Address != "1.2.3.4" || p.Port != 8388 || p.Name != "SS узел" {
					t.Errorf("host/port/name = %q/%d/%q", p.Address, p.Port, p.Name)
				}
			},
		},
		{
			name: "ss 2022-формат (plain userinfo) + plugin",
			link: "ss://2022-blake3-aes-128-gcm:" + percentEncodeAll("p@ss:word/+") + "@h.example.com:8388?plugin=obfs-local%3Bobfs%3Dhttp%3Bobfs-host%3Dx",
			check: func(t *testing.T, p *model.Profile) {
				d := p.Detail.(*model.ShadowsocksDetail)
				if d.Method != "2022-blake3-aes-128-gcm" || d.Password != "p@ss:word/+" {
					t.Errorf("method/password = %q/%q", d.Method, d.Password)
				}
				if d.Plugin != "obfs-local;obfs=http;obfs-host=x" {
					t.Errorf("plugin = %q", d.Plugin)
				}
				if p.Port != 8388 || p.Address != "h.example.com" {
					t.Errorf("host/port = %q/%d", p.Address, p.Port)
				}
			},
		},
		{
			name: "ss v2rayN (полный base64)",
			link: "ss://" + b64std("aes-128-gcm:pw@5.6.7.8:443") + "#v2rayN",
			check: func(t *testing.T, p *model.Profile) {
				d := p.Detail.(*model.ShadowsocksDetail)
				if d.Method != "aes-128-gcm" || d.Password != "pw" || p.Address != "5.6.7.8" || p.Port != 443 {
					t.Errorf("got %+v", d)
				}
				if p.Name != "v2rayN" {
					t.Errorf("name = %q", p.Name)
				}
			},
		},
		{
			name:  "vmess v2rayN base64 JSON (FR-UI-164)",
			link:  "vmess://" + b64std(`{"v":"2","ps":"vm","add":"v.example.org","port":"443","id":"b831381d-6324-4d53-ad4f-8cda48b30811","aid":"0","net":"ws","host":"v.example.org","path":"/ws","type":"","scy":"auto","tls":"tls","sni":"v.example.org"}`),
			check: vmessCheck("b831381d-6324-4d53-ad4f-8cda48b30811"),
		},
		{
			name: "vmess v2rayN, port числом и без tls",
			link: "vmess://" + b64std(`{"v":"2","add":"v2.io","port":8443,"id":"id-1","net":"tcp","scy":"zero"}`),
			check: func(t *testing.T, p *model.Profile) {
				if p.Port != 8443 {
					t.Errorf("port = %d", p.Port)
				}
				if p.Stream != nil && (p.Stream.Network != "tcp" || p.Stream.Security != "") {
					t.Errorf("stream = %+v", p.Stream)
				}
				d := p.Detail.(*model.VMessDetail)
				if d.Security != "zero" || d.AlterID != 0 {
					t.Errorf("scy/aid = %q/%d", d.Security, d.AlterID)
				}
			},
		},
		{
			name:  "vmess URL-формат (XTLS #716)",
			link:  "vmess://uuid-vm@v3.io:443?encryption=chacha20-poly1305&security=tls&sni=v3.io&fp=chrome&type=grpc&serviceName=grpc-svc&allowInsecure=1#vmurl",
			check: vmessURLCheck,
		},
		{
			name: "vless REALITY",
			link: "vless://uuid-vl@vl.io:443?encryption=none&security=reality&pbk=PBK123&sid=6ba85179&spx=%2Fx&fp=chrome&type=tcp&flow=xtls-rprx-vision#vl",
			check: func(t *testing.T, p *model.Profile) {
				d := p.Detail.(*model.TrojanVlessDetail)
				if d.Password != "uuid-vl" || d.Flow != "xtls-rprx-vision" {
					t.Errorf("detail = %+v", d)
				}
				st := p.Stream
				if st == nil || st.Security != "reality" || st.RealityPublicKey != "PBK123" ||
					st.RealityShortID != "6ba85179" || st.RealitySPX != "/x" || st.UTLSFingerprint != "chrome" {
					t.Errorf("stream = %+v", st)
				}
			},
		},
		{
			name: "trojan ws + tls (дефолт security)",
			link: "trojan://pw%40x@t.io?type=ws&path=%2Ft&host=t.io&alpn=h2%2C%20http%2F1.1&allowInsecure=1",
			check: func(t *testing.T, p *model.Profile) {
				d := p.Detail.(*model.TrojanVlessDetail)
				if d.Password != "pw@x" {
					t.Errorf("password = %q", d.Password)
				}
				if p.Port != 443 { // дефолт
					t.Errorf("port = %d", p.Port)
				}
				st := p.Stream
				if st == nil || st.Security != "tls" || st.Network != "ws" || st.Path != "/t" ||
					st.Host != "t.io" || st.ALPN != "h2,http/1.1" || !st.AllowInsecure {
					t.Errorf("stream = %+v", st)
				}
			},
		},
		{
			name: "vless h2 → http, host через |",
			link: "vless://u1@h.io:8443?security=tls&type=h2&host=a.io%7Cb.io&path=%2Fp",
			check: func(t *testing.T, p *model.Profile) {
				st := p.Stream
				if st.Network != "http" || st.Host != "a.io,b.io" || st.Path != "/p" {
					t.Errorf("stream = %+v", st)
				}
			},
		},
		{
			name: "tcp headerType=http",
			link: "vless://u2@h2.io:443?type=tcp&headerType=http&host=hh&path=%2Fhp",
			check: func(t *testing.T, p *model.Profile) {
				st := p.Stream
				if st.HeaderType != "http" || st.Host != "hh" || st.Path != "/hp" {
					t.Errorf("stream = %+v", st)
				}
			},
		},
		{
			name: "hy2 полный + дефолт порта 443 (LNB-15)",
			link: "hy2://pw@hy.io?obfs=salamander&obfs-password=ob&mport=20000:30000&insecure=1&sni=hy.io&alpn=h3&hop_interval=30s",
			check: func(t *testing.T, p *model.Profile) {
				d := p.Detail.(*model.Hysteria2Detail)
				if d.Password != "pw" || d.ObfsPassword != "ob" || d.ServerPorts != "20000:30000" ||
					!d.AllowInsecure || d.SNI != "hy.io" || d.ALPN != "h3" || d.HopInterval != "30s" {
					t.Errorf("detail = %+v", d)
				}
				if p.Port != 443 {
					t.Errorf("port = %d (дефолт 443 не применён)", p.Port)
				}
			},
		},
		{
			name: "hysteria2 схема-алиас, пароль с двоеточием",
			link: "hysteria2://user:pass:word@hy2.io:8443?sni=s.io",
			check: func(t *testing.T, p *model.Profile) {
				d := p.Detail.(*model.Hysteria2Detail)
				if d.Password != "user:pass:word" || p.Port != 8443 || d.SNI != "s.io" {
					t.Errorf("got %+v port=%d", d, p.Port)
				}
			},
		},
		{
			name: "tuic полный + дефолт порта 443 (LNB-15)",
			link: "tuic://uuid-tu:pass@tq.io?congestion_control=bbr&udp_relay_mode=native&zero_rtt_handshake=1&heartbeat=10s&sni=tq.io&alpn=h3&allow_insecure=1&disable_sni=0",
			check: func(t *testing.T, p *model.Profile) {
				d := p.Detail.(*model.TUICDetail)
				if d.UUID != "uuid-tu" || d.Password != "pass" || d.CongestionControl != "bbr" ||
					d.UDPRelayMode != "native" || !d.ZeroRttHandshake || d.Heartbeat != "10s" ||
					d.SNI != "tq.io" || d.ALPN != "h3" || !d.AllowInsecure || d.DisableSNI {
					t.Errorf("detail = %+v", d)
				}
				if p.Port != 443 {
					t.Errorf("port = %d (дефолт 443 не применён)", p.Port)
				}
			},
		},
		{
			name: "socks5 с авторизацией",
			link: "socks://user:pa%3Ass@1.2.3.4:1080#s5",
			check: func(t *testing.T, p *model.Profile) {
				d := p.Detail.(*model.SocksHTTPDetail)
				if p.Type != model.TypeSOCKS || d.Proto != "socks5" || d.Username != "user" || d.Password != "pa:ss" {
					t.Errorf("got %+v", d)
				}
			},
		},
		{
			name: "socks4 без порта → 1080",
			link: "socks4://5.6.7.8",
			check: func(t *testing.T, p *model.Profile) {
				d := p.Detail.(*model.SocksHTTPDetail)
				if d.Proto != "socks4" || p.Port != 1080 {
					t.Errorf("got %+v port=%d", d, p.Port)
				}
			},
		},
		{
			name: "socks v2rayN (логин — base64 user:pass)",
			link: "socks://" + b64std("john:doe") + "@9.9.9.9:1080",
			check: func(t *testing.T, p *model.Profile) {
				d := p.Detail.(*model.SocksHTTPDetail)
				if d.Username != "john" || d.Password != "doe" {
					t.Errorf("got %+v", d)
				}
			},
		},
		{
			name: "http без порта → 80",
			link: "http://h.io",
			check: func(t *testing.T, p *model.Profile) {
				if p.Type != model.TypeHTTP || p.Port != 80 || p.Detail.(*model.SocksHTTPDetail).Proto != "http" {
					t.Errorf("got %+v", p)
				}
			},
		},
		{
			name: "https без порта → 443 + security=tls + sni из query",
			link: "https://h.io?sni=ssl.io",
			check: func(t *testing.T, p *model.Profile) {
				if p.Port != 443 || p.Stream == nil || p.Stream.Security != "tls" || p.Stream.SNI != "ssl.io" {
					t.Errorf("got %+v %+v", p, p.Stream)
				}
			},
		},
		{
			name: "IPv6-хост канонизируется",
			link: "trojan://pw@[2001:0DB8::0001]:443?security=tls&type=tcp",
			check: func(t *testing.T, p *model.Profile) {
				if p.Address != "2001:db8::1" {
					t.Errorf("address = %q", p.Address)
				}
			},
		},
		{
			name: "SNI из host при IP-адресе (§4.1)",
			link: "vless://u@1.2.3.4:443?security=tls&type=ws&host=example.com&path=%2Fw",
			check: func(t *testing.T, p *model.Profile) {
				if p.Stream == nil || p.Stream.SNI != "example.com" {
					t.Errorf("stream = %+v", p.Stream)
				}
			},
		},
		{
			name: "security=0/none/1 (нормализация §4.1)",
			link: "vless://u@h.io:443?security=none",
			check: func(t *testing.T, p *model.Profile) {
				if p.Stream.Security != "" {
					t.Errorf("security = %q", p.Stream.Security)
				}
			},
		},
		{
			name: "пробелы вокруг ссылки вырезаются, схема в верхнем регистре",
			link: "  VMESS://" + b64std(`{"add":"a.io","port":1,"id":"x","net":"tcp"}`) + "  ",
			check: func(t *testing.T, p *model.Profile) {
				if p.Address != "a.io" || p.Port != 1 {
					t.Errorf("got %+v", p)
				}
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p, err := ParseLink(tt.link)
			if err != nil {
				t.Fatalf("ParseLink: %v", err)
			}
			tt.check(t, p)
		})
	}
}

func vmessCheck(uuid string) func(*testing.T, *model.Profile) {
	return func(t *testing.T, p *model.Profile) {
		d, ok := p.Detail.(*model.VMessDetail)
		if !ok {
			t.Fatalf("detail = %T", p.Detail)
		}
		if d.UUID != uuid || d.Security != "auto" || d.AlterID != 0 {
			t.Errorf("detail = %+v", d)
		}
		if p.Address != "v.example.org" || p.Port != 443 || p.Name != "vm" {
			t.Errorf("host/port/name = %q/%d/%q", p.Address, p.Port, p.Name)
		}
		st := p.Stream
		if st == nil || st.Network != "ws" || st.Host != "v.example.org" || st.Path != "/ws" ||
			st.Security != "tls" || st.SNI != "v.example.org" {
			t.Errorf("stream = %+v", st)
		}
	}
}

func vmessURLCheck(t *testing.T, p *model.Profile) {
	d := p.Detail.(*model.VMessDetail)
	if d.UUID != "uuid-vm" || d.Security != "chacha20-poly1305" || d.AlterID != 0 {
		t.Errorf("detail = %+v", d)
	}
	st := p.Stream
	if st == nil || st.Security != "tls" || st.SNI != "v3.io" || st.UTLSFingerprint != "chrome" ||
		st.Network != "grpc" || st.Path != "grpc-svc" || !st.AllowInsecure {
		t.Errorf("stream = %+v", st)
	}
	if p.Name != "vmurl" {
		t.Errorf("name = %q", p.Name)
	}
}

// TestParseLinkLNB15 — регрессия LNB-15: порт никогда не выходит за 1..65535,
// отсутствие порта у tuic/hy2 — корректный дефолт 443 (в oracle ссылка
// отвергалась, а явный 0/−1 утекал в конфиг), явный мусорный порт — ошибка.
func TestParseLinkLNB15(t *testing.T) {
	// дефолты применяются
	for _, link := range []string{
		"tuic://u:p@h.io",
		"hy2://pw@h.io",
		"hysteria2://pw@h.io",
		"trojan://pw@h.io",
		"vless://u@h.io",
		"vmess://u@h.io?security=tls",
	} {
		p, err := ParseLink(link)
		if err != nil {
			t.Errorf("%s: %v", link, err)
			continue
		}
		if p.Port != 443 {
			t.Errorf("%s: port = %d, want 443 (дефолт)", link, p.Port)
		}
	}
	// явные неверные порты — ошибки, порт 0 не попадает в модель
	bad := []struct{ link, wantSub string }{
		{"tuic://u:p@h.io:0", "вне диапазона 1–65535"},
		{"hy2://pw@h.io:70000", "вне диапазона 1–65535"},
		{"ss://" + encodeB64URL("aes-128-gcm:p") + "@h.io:99999", "вне диапазона 1–65535"},
		{"vmess://" + b64std(`{"add":"a.io","port":0,"id":"x"}`), "порт"},
		{"socks://h.io:65536", "вне диапазона 1–65535"},
		{"https://h.io:0", "вне диапазона 1–65535"},
		{"tuic://u:p@h.io:-1", "некорректный URL"},
	}
	for _, b := range bad {
		p, err := ParseLink(b.link)
		if err == nil {
			t.Errorf("%s: ожидается ошибка, got port=%d", b.link, p.Port)
			continue
		}
		if !strings.Contains(err.Error(), b.wantSub) {
			t.Errorf("%s: ошибка %q не содержит %q", b.link, err.Error(), b.wantSub)
		}
	}
}

// TestParseLinkLNB17 — регрессия LNB-17: слабая валидация полей (vmess port=0,
// произвольный SS-метод, пустые секреты) → ошибки с указанием поля.
func TestParseLinkLNB17(t *testing.T) {
	bad := []struct{ name, link, wantSub string }{
		{"vmess без порта", "vmess://" + b64std(`{"add":"a.io","id":"x","net":"tcp"}`), "port"},
		{"vmess порт-мусор", "vmess://" + b64std(`{"add":"a.io","port":"x1","id":"x"}`), "не является числом"},
		{"vmess без uuid", "vmess://" + b64std(`{"add":"a.io","port":1}`), "UUID"},
		{"ss произвольный метод", "ss://" + encodeB64URL("xyz-method:pw") + "@h.io:1", "метод"},
		{"ss пустой пароль", "ss://" + encodeB64URL("aes-128-gcm:") + "@h.io:1", "метод:пароль"},
		{"ss битый base64", "ss://!*(@h.io:1", "base64"},
		{"ss без порта", "ss://" + encodeB64URL("aes-128-gcm:pw") + "@h.io", "порт не указан"},
		{"trojan без пароля", "trojan://@h.io:443", "пароль"},
		{"vless без uuid", "vless://@h.io:443", "UUID"},
		{"tuic без пароля", "tuic://u@h.io:443", "пароль"},
		{"tuic без uuid", "tuic://:p@h.io:443", "UUID"},
		{"hy2 без пароля", "hy2://@h.io:443", "пароль"},
		{"vless flow-мусор", "vless://u@h.io:443?flow=xtls-rprx-origin", "flow"},
		{"fp-мусор", "vless://u@h.io:443?fp=opera&security=tls", "uTLS"},
		{"сеть-мусор", "trojan://p@h.io:443?type=kcp", "транспорт"},
		{"security-мусор", "vless://u@h.io:443?security=ssl", "security"},
		{"hy2 obfs-мусор", "hy2://pw@h.io:443?obfs=xxx", "obfs"},
		{"hy2 mport-мусор", "hy2://pw@h.io:443?mport=abc", "mport"},
		{"hy2 mport вне диапазона", "hy2://pw@h.io:443?mport=1:99999", "mport"},
		{"hy2 hop_interval-мусор", "hy2://pw@h.io:443?hop_interval=asap", "hop_interval"},
		{"tuic congestion-мусор", "tuic://u:p@h.io:443?congestion_control=xyz", "congestion_control"},
		{"tuic relay-мусор", "tuic://u:p@h.io:443?udp_relay_mode=udp", "udp_relay_mode"},
		{"tuic heartbeat-мусор", "tuic://u:p@h.io:443?heartbeat=10sec", "heartbeat"},
		{"пустой хост", "trojan://pw@:443", "адрес"},
	}
	for _, b := range bad {
		t.Run(b.name, func(t *testing.T) {
			_, err := ParseLink(b.link)
			if err == nil {
				t.Fatalf("ожидается ошибка с %q", b.wantSub)
			}
			if !strings.Contains(err.Error(), b.wantSub) {
				t.Fatalf("ошибка %q не содержит %q", err.Error(), b.wantSub)
			}
		})
	}
}

func TestParseLinkMiscErrors(t *testing.T) {
	bad := []struct{ name, link, wantSub string }{
		{"пусто", "", "пуста"},
		{"пробелы", "   ", "пуста"},
		{"нет схемы", "just-a-host:443", "схема"},
		{"unsupported", "wireguard://key@h.io:443", "неподдерживаемая схема"},
		{"зашифрованная ссылка — в подписки", "happ://xxx", "«Подписки»"},
		{"naive+https", "naive+https://u:p@h.io:443", "неподдерживаемая схема"},
	}
	for _, b := range bad {
		t.Run(b.name, func(t *testing.T) {
			_, err := ParseLink(b.link)
			if err == nil || !strings.Contains(err.Error(), b.wantSub) {
				t.Fatalf("err = %v, want substring %q", err, b.wantSub)
			}
		})
	}
	// ссылка длиннее 64 КиБ
	if _, err := ParseLink("vmess://" + strings.Repeat("a", maxLinkLength)); err == nil ||
		!strings.Contains(err.Error(), "длиннее") {
		t.Errorf("лимит длины не сработал: %v", err)
	}
}

// TestNekoLinkLNB25 — c_cfg/c_out из neko-ссылки отбрасываются (LNB-25).
func TestNekoLinkLNB25(t *testing.T) {
	p := &model.Profile{
		Type: model.TypeVMess, Name: "vm", Address: "a.io", Port: 443,
		CustomConfigJSON:   `{"experimental":{"clash_api":{"external_ui_download_url":"http://evil"}}}`,
		CustomOutboundJSON: `{"type":"socks"}`,
		Detail:             &model.VMessDetail{UUID: "u", Security: "auto"},
	}
	link, err := BuildNekoLink(p)
	if err != nil {
		t.Fatalf("BuildNekoLink: %v", err)
	}
	got, err := ParseLink(link)
	if err != nil {
		t.Fatalf("ParseLink: %v", err)
	}
	if got.CustomConfigJSON != "" || got.CustomOutboundJSON != "" {
		t.Errorf("LNB-25 нарушен: c_cfg=%q c_out=%q", got.CustomConfigJSON, got.CustomOutboundJSON)
	}
	if got.CustomFieldsConfirmed {
		t.Errorf("custom_fields_confirmed должен быть false")
	}
}
