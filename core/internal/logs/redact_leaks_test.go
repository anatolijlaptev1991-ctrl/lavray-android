// SPDX-License-Identifier: GPL-3.0-only

package logs

import (
	"encoding/json"
	"strings"
	"testing"
)

// T-49 (A1-13, B2-17, D1-12): формы секретов, которые Redact раньше пропускал.
// secret — подстроки, которых в выводе быть не должно; keep — подстроки,
// которые обязаны остаться (хост, путь, имена ключей).
var t49LeakCases = []struct {
	name   string
	in     string
	secret []string
	keep   []string
}{
	{"токен в пути URL",
		`подписка: Get "https://panel.example.com/sub/8f3a1c9e7b2d4f60a1b2c3d4e5f60718": dial tcp: i/o timeout`,
		[]string{"8f3a1c9e7b2d4f60a1b2c3d4e5f60718"}, []string{"panel.example.com/sub/", "dial tcp: i/o timeout"}},
	{"токен в пути, смешанный регистр",
		`обновление https://panel.example.com/sub/Zk3Qx9aBcDeF12345 завершено`,
		[]string{"Zk3Qx9aBcDeF12345"}, []string{"panel.example.com/sub/", "завершено"}},
	{"JWT в пути",
		`Get "https://panel.example.com/sub/eyJhbGciOiJIUzI1NiJ9.eyJzdWIiOiJ1c2VyIn0.c2lnbmF0dXJl1234567": EOF`,
		[]string{"eyJhbGciOiJIUzI1NiJ9", "c2lnbmF0dXJl1234567"}, []string{"panel.example.com/sub/"}},
	{"query sid= и t=",
		`Get "https://panel.example.com/api/sub?sid=Zk39dj2Lq&t=ab12cd34ef56": EOF`,
		[]string{"Zk39dj2Lq", "ab12cd34ef56"}, []string{"panel.example.com/api/sub", "sid=", "t="}},
	{"query uuid=",
		`GET https://h.example/sub?uuid=11111111-2222-3333-4444-555555555555&flag=1`,
		[]string{"11111111-2222-3333-4444-555555555555"}, []string{"h.example/sub", "uuid="}},
	{"пароль с @ в userinfo",
		`dial socks5://user:p@ssw0rd-tail@host.example:1080: refused`,
		[]string{"ssw0rd-tail", "p@ss", "user:"}, []string{"host.example:1080", "refused"}},
	{"пароль со / в userinfo",
		`proxy http://user:pa/ss-word-xyz@host.example:8080 failed`,
		[]string{"ss-word-xyz", "pa/ss"}, []string{"host.example:8080", "failed"}},
	{"JSON: число и массив",
		`{"short_id": ["0123456789abcdef"], "password": 123456789}`,
		[]string{"0123456789abcdef", "123456789"}, []string{`"short_id"`, `"password"`}},
	{"JSON: pre_shared_key и private_key",
		`{"type":"wireguard","private_key":"PRIVKEYVALUE","pre_shared_key":"PSKVALUE"}`,
		[]string{"PRIVKEYVALUE", "PSKVALUE"}, []string{`"type":"wireguard"`}},
	{"JSON: auth_str, auth и obfs",
		`{"type":"hysteria","auth_str":"hunter2-hunter2","auth":"AUTHVALUE","obfs":"OBFSVALUE"}`,
		[]string{"hunter2-hunter2", "AUTHVALUE", "OBFSVALUE"}, []string{`"type":"hysteria"`, "auth_str"}},
	{"JSON: private_key_passphrase",
		`{"private_key_passphrase":"correct horse battery"}`,
		[]string{"correct", "horse", "battery"}, []string{"private_key_passphrase"}},
	{"JSON: экранированная кавычка в пароле",
		`{"password":"abc\"def-secret-tail"}`,
		[]string{"abc", "def-secret-tail"}, []string{`"password"`}},
	{"JSON: заголовок Authorization",
		`{"headers":{"Authorization":"Bearer TOKENVALUE123"}}`,
		[]string{"TOKENVALUE123"}, []string{"Authorization"}},
	{"YAML Clash: flow-стиль",
		"  - {name: a, type: trojan, server: h, port: 443, password: s3cr3t-yaml-pass}",
		[]string{"s3cr3t-yaml-pass"}, []string{"server: h", "port: 443"}},
	{"YAML Clash: блочный uuid",
		"    uuid: 11111111-2222-3333-4444-555555555555",
		[]string{"11111111-2222-3333-4444-555555555555"}, []string{"uuid:"}},
	{"psk= и obfs-key= без ? и &",
		`hy2 start: psk=SuperSecretPSK-1234 obfs-key=abcdef`,
		[]string{"SuperSecretPSK-1234", "abcdef"}, []string{"hy2 start:", "psk=", "obfs-key="}},
	{"user=<uuid> pass=",
		`user=b831381d-6324-4d53-ad4f-8cda48b30811 pass=hunter2`,
		[]string{"b831381d-6324-4d53-ad4f-8cda48b30811", "hunter2"}, []string{"user=", "pass="}},
	{"голый UUID в тексте ошибки",
		`outbound[2]: vless: invalid user id 8f3a1c9e-7b2d-4f60-a1b2-c3d4e5f60718`,
		[]string{"8f3a1c9e-7b2d-4f60-a1b2-c3d4e5f60718"}, []string{"invalid user id"}},
	{"ssh private_key с переводами строк",
		`ssh outbound private_key="-----BEGIN OPENSSH ` + "PRIVATE KEY-----" + `\nAAAA\n-----END OPENSSH PRIVATE KEY-----"`,
		[]string{"BEGIN OPENSSH PRIVATE KEY", "AAAA"}, []string{"ssh outbound"}},
	{"путь без схемы после метода",
		`GET /sub/abcdef0123456789abcdef0123456789 HTTP/1.1 Authorization: Bearer TOKENVALUE`,
		[]string{"abcdef0123456789abcdef0123456789", "TOKENVALUE"}, []string{"GET /sub/", "HTTP/1.1"}},
	{"токен в пути, латиница и цифры",
		`fetch https://provider.example/sub/PATHSECRET0123456789 failed: timeout`,
		[]string{"PATHSECRET0123456789"}, []string{"provider.example/sub/", "failed: timeout"}},
}

func TestRedactT49Leaks(t *testing.T) {
	for _, c := range t49LeakCases {
		t.Run(c.name, func(t *testing.T) {
			out := Redact(c.in)
			for _, s := range c.secret {
				if strings.Contains(out, s) {
					t.Errorf("секрет %q остался\n  вход:  %s\n  вывод: %s", s, c.in, out)
				}
			}
			for _, k := range c.keep {
				if !strings.Contains(out, k) {
					t.Errorf("пропала диагностика %q\n  вход:  %s\n  вывод: %s", k, c.in, out)
				}
			}
			if twice := Redact(out); twice != out {
				t.Errorf("Redact не идемпотентен\n  1: %s\n  2: %s", out, twice)
			}
		})
	}
}

// Редакция конфига (GetRuntimeConfig) оставляет валидный JSON и несекретные поля.
func TestRedactT49KeepsJSONValid(t *testing.T) {
	in := `{
  "outbounds": [
    {"type": "vless", "tag": "proxy", "server": "example.com", "uuid": "b831381d-6324-4d53-ad4f-8cda05ad21b1",
     "tls": {"server_name": "cdn.example.com",
             "reality": {"public_key": "PUBKEYVALUE", "short_id": "ab12cd34"}}},
    {"type": "shadowsocks", "tag": "ss", "password": 12345678, "method": "aes-128-gcm"},
    {"type": "direct", "tag": "bypass"}
  ],
  "route": {"final": "proxy"}
}`
	out := Redact(in)
	if !json.Valid([]byte(out)) {
		t.Fatalf("после Redact JSON невалиден:\n%s", out)
	}
	for _, secret := range []string{"b831381d-6324-4d53-ad4f-8cda05ad21b1", "ab12cd34", "12345678"} {
		if strings.Contains(out, secret) {
			t.Errorf("секрет %q остался:\n%s", secret, out)
		}
	}
	for _, keep := range []string{"PUBKEYVALUE", `"server": "example.com"`, `"tag": "bypass"`, `"final": "proxy"`} {
		if !strings.Contains(out, keep) {
			t.Errorf("пропало %q:\n%s", keep, out)
		}
	}
}

// Безвредный текст не меняется: иначе журнал станет бесполезным.
func TestRedactT49KeepsHarmlessText(t *testing.T) {
	for _, in := range []string{
		`порт 2080 занят`,
		`outbound/vmess[0]: dial tcp 203.0.113.5:443: i/o timeout`,
		`https://www.gstatic.com/generate_204`,
		`{"tag":"bypass","type":"direct"}`,
		`скачивание https://updates.example.com/stable/lavray-windows-x64-0.1.0-setup.exe завершено`,
		`POST /lavray.v1.CoreService/GetStatus 200`,
		`база C:/Users/user/AppData/Roaming/LAVRay/lavray.db открыта`,
		`auth failed: connection reset`,
		`https://example.com/@user/profile`,
		`https://example.com:8080/@user/profile`,
		`tun.bypass_cidr=10.0.0.0/8 final=proxy`,
	} {
		if out := Redact(in); out != in {
			t.Errorf("безвредный текст изменён\n  вход:  %s\n  вывод: %s", in, out)
		}
	}
}
