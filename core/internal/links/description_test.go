// SPDX-License-Identifier: GPL-3.0-only

package links

import (
	"encoding/base64"
	"encoding/json"
	"strings"
	"testing"
)

const descUUID = "11111111-2222-4333-8444-555555555555"

func b64Desc(s string) string { return base64.StdEncoding.EncodeToString([]byte(s)) }

// Хвост «?serverDescription=<base64>» отрезается от имени в любой схеме, а
// описание попадает в заметки.
func TestServerDescriptionStrippedFromName(t *testing.T) {
	desc := "Германия, 1 Гбит/с — только Wi-Fi"
	tail := "?serverDescription=" + b64Desc(desc)
	vm, _ := json.Marshal(map[string]any{
		"v": "2", "ps": "🇩🇪 DE — Берлин (WS)" + tail, "add": "de.example.test", "port": "443",
		"id": descUUID, "aid": "0", "net": "ws", "tls": "tls",
	})
	cases := []struct {
		name     string
		link     string
		wantName string
	}{
		{"vless", "vless://" + descUUID + "@de.example.test:443?type=grpc&security=tls&serviceName=svc#🇩🇪 DE — Берлин (only WiFi)" + tail,
			"🇩🇪 DE — Берлин (only WiFi)"},
		{"vless, имя закодировано", "vless://" + descUUID + "@de.example.test:443?type=ws&path=/ws#" +
			url2("🇩🇪 DE — Берлин (only WiFi)") + url2(tail), "🇩🇪 DE — Берлин (only WiFi)"},
		{"vless, закодированы только ? и =", "vless://" + descUUID + "@de.example.test:443#DE%20Berlin" +
			"%3FserverDescription%3D" + strings.NewReplacer("+", "%2B", "/", "%2F", "=", "%3D").Replace(b64Desc(desc)),
			"DE Berlin"},
		{"vmess v2rayN", "vmess://" + base64.StdEncoding.EncodeToString(vm), "🇩🇪 DE — Берлин (WS)"},
		{"trojan", "trojan://pw@de.example.test:443#DE" + tail, "DE"},
		{"ss", "ss://" + base64.URLEncoding.EncodeToString([]byte("aes-256-gcm:pw")) + "@de.example.test:8388#DE" + tail, "DE"},
		{"hy2", "hy2://pw@de.example.test:443#DE" + tail, "DE"},
		{"tuic", "tuic://" + descUUID + ":pw@de.example.test:443#DE" + tail, "DE"},
		{"регистр ключа", "trojan://pw@de.example.test:443#DE?SERVERdescription=" + b64Desc(desc), "DE"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			p, err := ParseLink(c.link)
			if err != nil {
				t.Fatalf("ParseLink: %v", err)
			}
			if p.Name != c.wantName {
				t.Errorf("имя %q, ожидалось %q", p.Name, c.wantName)
			}
			if p.Notes != desc {
				t.Errorf("заметки %q, ожидалось %q", p.Notes, desc)
			}
		})
	}
}

// url2 кодирует всё, кроме безопасных символов (как encodeURIComponent).
func url2(s string) string {
	var b strings.Builder
	for _, c := range []byte(s) {
		switch {
		case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c >= '0' && c <= '9', strings.IndexByte("-_.!~*'()", c) >= 0:
			b.WriteByte(c)
		default:
			b.WriteString("%" + strings.ToUpper(string("0123456789abcdef"[c>>4])+string("0123456789abcdef"[c&15])))
		}
	}
	return b.String()
}

// Некорректное описание: хвост отрезается, заметки остаются пустыми.
func TestServerDescriptionInvalidBase64JustCut(t *testing.T) {
	for _, tail := range []string{
		"?serverDescription=%%%не-base64",
		"?serverDescription=",
		"?serverDescription=" + base64.StdEncoding.EncodeToString([]byte{0xff, 0xfe, 0xfd}), // не UTF-8
	} {
		p, err := ParseLink("trojan://pw@de.example.test:443#DE%20Berlin" + tail)
		if err != nil {
			t.Fatalf("%q: ParseLink: %v", tail, err)
		}
		if p.Name != "DE Berlin" || p.Notes != "" {
			t.Errorf("%q: имя %q, заметки %q; ожидалось «DE Berlin» без заметок", tail, p.Name, p.Notes)
		}
	}
}

// Имя без хвоста и «?» в обычном имени не трогаются; свои заметки профиля
// (neko-ссылка) описание не перетирает.
func TestServerDescriptionLeavesOrdinaryNames(t *testing.T) {
	for _, name := range []string{"DE Berlin", "Что? Где? Когда?", "a?b=c"} {
		p, err := ParseLink("trojan://pw@de.example.test:443#" + url2(name))
		if err != nil {
			t.Fatal(err)
		}
		if p.Name != name || p.Notes != "" {
			t.Errorf("имя %q, заметки %q; ожидалось %q без заметок", p.Name, p.Notes, name)
		}
	}
}

// Описание сверх предела обрезается по границе символа и остаётся валидным UTF-8.
func TestServerDescriptionLengthLimit(t *testing.T) {
	long := strings.Repeat("ж", maxDescriptionLength) // 2 байта на символ
	p, err := ParseLink("trojan://pw@de.example.test:443#DE?serverDescription=" + b64Desc(long))
	if err != nil {
		t.Fatal(err)
	}
	if len(p.Notes) > maxDescriptionLength || !strings.HasPrefix(long, p.Notes) || p.Notes == "" {
		t.Errorf("заметки: %d байт, ожидалась обрезка до %d по границе символа", len(p.Notes), maxDescriptionLength)
	}
}

// Длинное описание не приводит к отказу «имя длиннее предела»: хвост снимается до проверки.
func TestServerDescriptionDoesNotCountTowardNameLimit(t *testing.T) {
	long := strings.Repeat("описание ", 700) // base64 заведомо больше maxRemarkLength
	link := "vless://" + descUUID + "@de.example.test:443?type=tcp#DE?serverDescription=" + b64Desc(long)
	if len(link) <= maxRemarkLength {
		t.Fatal("тест не проверяет предел: ссылка короткая")
	}
	p, err := ParseLink(link)
	if err != nil {
		t.Fatalf("ParseLink: %v", err)
	}
	if p.Name != "DE" || p.Notes == "" {
		t.Errorf("имя %q, заметки %d байт", p.Name, len(p.Notes))
	}
}
