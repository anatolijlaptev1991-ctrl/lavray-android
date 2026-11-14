// SPDX-License-Identifier: GPL-3.0-only

package links

import (
	"strings"
	"testing"

	"github.com/anatolijlaptev1991-ctrl/lavray-android/core/internal/model"
)

// FuzzParseLink — инварианты (core-port-plan §4.3, TEST-PLAN набор 3):
//  1. ParseLink никогда не паникует (ошибка — допустимый результат);
//  2. при успехе у сетевых типов порт ∈ 1..65535 и адрес непуст (LNB-15);
//  3. результат парсинга переживает повторную сборку/разбор (build→parse
//     без ошибок, neko-roundtrip).
func FuzzParseLink(f *testing.F) {
	seeds := []string{
		// по одному на схему
		"socks://user:pass@1.2.3.4:1080#s5",
		"socks4://1.2.3.4:1080",
		"http://h.io:8080",
		"https://h.io",
		"ss://" + encodeB64URL("aes-256-gcm:password") + "@1.2.3.4:8388#ss",
		"ss://2022-blake3-aes-128-gcm:psw@h.example.com:8388?plugin=obfs-local%3Bobfs%3Dhttp",
		"ss://" + b64std("aes-128-gcm:pw@5.6.7.8:443"),
		"vmess://" + b64std(`{"v":"2","ps":"vm","add":"v.io","port":"443","id":"b831381d-6324-4d53-ad4f-8cda48b30811","aid":"0","net":"ws","host":"v.io","path":"/ws","scy":"auto","tls":"tls","sni":"v.io"}`),
		"vmess://uuid@v.io:443?encryption=auto&security=tls&sni=v.io&type=ws&path=%2Fws&host=v.io",
		"vless://uuid@vl.io:443?encryption=none&security=reality&pbk=PBK&sid=6ba85179&spx=%2Fx&fp=chrome&type=tcp&flow=xtls-rprx-vision",
		"trojan://pw@t.io:443?security=tls&type=ws&path=%2Ft&host=t.io&alpn=h2%2Chttp%2F1.1&allowInsecure=1",
		"hy2://user:pass@hy.io:8443?obfs=salamander&obfs-password=ob&mport=20000:30000&insecure=1&sni=hy.io&hop_interval=30s&upmbps=100&downmbps=200",
		"hysteria2://pw@hy.io",
		"tuic://uuid:pass@tq.io?congestion_control=bbr&udp_relay_mode=native&zero_rtt_handshake=1&heartbeat=10s&sni=tq.io&alpn=h3",
		// neko-ссылка
		"nekoray://vmess#" + encodeB64URL(`{"name":"vm","addr":"v.io","port":443,"id":"u","aid":0,"sec":"auto","stream":{"net":"ws","sec":"tls"}}`),
		// отклоняемые схемы
		"happ://crypt5.1712044823.AB#x",
		"naive+https://u:p@h.io:443",
		"wireguard://key@h.io",
		// мусор/граничные случаи
		"",
		"   ",
		"://",
		"vmess://",
		"ss://",
		"ss://@",
		"ss://!!@h:1",
		"vmess://%%%===",
		"vmess://" + b64std(`{"add":"a.io","port":0,"id":"x"}`), // LNB-17
		"vmess://" + b64std(`{"add":"a.io","port":99999,"id":"x"}`),
		"tuic://u:p@h.io:0",     // LNB-15
		"tuic://u:p@h.io:70000", // LNB-15
		"trojan://pw@[2001:db8::1]:443?security=tls&type=tcp",
		"vless://u@1.2.3.4:443?security=tls&type=ws&host=example.com&path=%2Fw",
		"socks://" + b64std("john:doe") + "@9.9.9.9:1080",
		"hy2://pw@hy.io:443?mport=1:99999",
		"hy2://pw@hy.io:443?hop_interval=asap",
		"vless://u@h.io:443?fp=opera&security=tls",
		"trojan://p@h.io:443?type=kcp",
		"nekoray://unknown#" + encodeB64URL(`{}`),
		"nekoray://vmess#not-base64!!!",
		"nekoray://vmess#" + encodeB64URL(`{"port":443,"id":"u"}`), // addr пуст
		strings.Repeat("vmess://", 8),
		"\x00\x01\x02\xff://x",
		"vmess://" + strings.Repeat("€", 512),
	}
	for _, s := range seeds {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, raw string) {
		p, err := ParseLink(raw)
		if err != nil {
			return // ошибка — допустимо; главное — не паника
		}
		if p == nil {
			t.Fatal("ParseLink вернула (nil, nil)")
		}
		// LNB-15: сетевые типы всегда с валидным портом и адресом.
		switch p.Type {
		case model.TypeChain, model.TypeCustomOutbound, model.TypeCustomConfig, model.TypeCustomExternal:
		default:
			if !validPort(p.Port) {
				t.Fatalf("порт %d вне 1..65535 у %s (вход %.64q)", p.Port, p.Type, raw)
			}
			if p.Address == "" {
				t.Fatalf("пустой адрес у %s (вход %.64q)", p.Type, raw)
			}
		}
		// Результат парсинга снова собирается и разбирается без ошибок.
		if link, err := BuildNekoLink(p); err == nil {
			if _, err := ParseLink(link); err != nil {
				t.Fatalf("neko-сборка/разбор: %v (вход %.64q)", err, raw)
			}
		} else {
			t.Fatalf("BuildNekoLink(%s): %v", p.Type, err)
		}
		switch p.Type {
		case model.TypeNaive, model.TypeChain, model.TypeCustomOutbound, model.TypeCustomConfig, model.TypeCustomExternal:
		default:
			link2, err := BuildLink(p)
			if err != nil {
				t.Fatalf("BuildLink(%s): %v", p.Type, err)
			}
			if _, err := ParseLink(link2); err != nil {
				t.Fatalf("пересборка share-ссылки %s: %v\nссылка: %s", p.Type, err, link2)
			}
		}
	})
}
