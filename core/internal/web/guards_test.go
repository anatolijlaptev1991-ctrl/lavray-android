// SPDX-License-Identifier: GPL-3.0-only

package web

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func postWithOrigin(host, origin string) *http.Request {
	r := httptest.NewRequest(http.MethodPost, "http://"+host+"/x", strings.NewReader(""))
	r.Host = host
	if origin != "" {
		r.Header.Set("Origin", origin)
	}
	return r
}

// TestOriginMismatchRejected — чужой, пустой и кривой Origin отклоняются (S).
func TestOriginMismatchRejected(t *testing.T) {
	cases := []struct {
		host, origin string
		required     bool
		ok           bool
		what         string
	}{
		{"192.168.1.1:8443", "https://192.168.1.1:8443", true, true, "тот же хост и порт"},
		{"Router.lan:8443", "https://router.LAN:8443", true, true, "регистр хоста"},
		{"router.lan", "https://router.lan:443", true, true, "порт по умолчанию https"},
		{"router.lan", "http://router.lan:80", true, true, "порт по умолчанию http"},
		{"192.168.1.1:8443", "https://evil.example", true, false, "чужой хост"},
		{"192.168.1.1:8443", "https://192.168.1.1:9999", true, false, "другой порт"},
		{"192.168.1.1:8443", "https://192.168.1.1.evil.example:8443", true, false, "хост с суффиксом"},
		{"192.168.1.1:8443", "null", true, false, "Origin: null (sandbox, file://)"},
		{"192.168.1.1:8443", "ftp://192.168.1.1:8443", true, false, "чужая схема"},
		{"192.168.1.1:8443", "https://u:p@192.168.1.1:8443", true, false, "userinfo"},
		{"192.168.1.1:8443", "https://192.168.1.1:8443/path", true, false, "путь в Origin"},
		{"192.168.1.1:8443", "garbage", true, false, "мусор"},
		{"192.168.1.1:8443", "", true, false, "нет Origin, обязателен"},
		{"192.168.1.1:8443", "", false, true, "нет Origin, не обязателен (curl)"},
		{"192.168.1.1:8443", "https://evil.example", false, false, "чужой Origin даже при необязательном"},
	}
	for _, c := range cases {
		err := CheckOrigin(postWithOrigin(c.host, c.origin), c.required)
		if (err == nil) != c.ok {
			t.Errorf("%s: host=%q origin=%q err=%v, ждали ok=%v", c.what, c.host, c.origin, err, c.ok)
		}
	}
}

func TestCSRFConstantTimeCompare(t *testing.T) {
	tok, err := newToken()
	if err != nil || len(tok) < 40 {
		t.Fatalf("токен: %q err=%v", tok, err)
	}
	other, _ := newToken()
	if tok == other {
		t.Fatal("токены повторяются")
	}
	cases := []struct {
		got, want string
		eq        bool
	}{{tok, tok, true}, {tok, other, false}, {"", "", false}, {tok, "", false}, {"", tok, false}, {tok[:10], tok, false}}
	for _, c := range cases {
		if CSRFEqual(c.got, c.want) != c.eq {
			t.Errorf("CSRFEqual(%q,%q) != %v", c.got, c.want, c.eq)
		}
	}
}

func TestCookieAttributes(t *testing.T) {
	c := NewCookie("tok", 12*time.Hour, true)
	if !c.HttpOnly || !c.Secure || c.SameSite != http.SameSiteStrictMode || c.Path != "/" || c.MaxAge != 43200 {
		t.Fatalf("cookie не укреплена: %+v", c)
	}
	if NewCookie("tok", time.Hour, false).Secure {
		t.Fatal("Secure выставлен для небезопасного соединения")
	}
	clear := ClearCookie(true)
	if clear.MaxAge >= 0 || clear.Value != "" || !clear.HttpOnly {
		t.Fatalf("ClearCookie: %+v", clear)
	}
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	if CookieToken(r) != "" {
		t.Fatal("токен из пустого запроса")
	}
	r.AddCookie(&http.Cookie{Name: CookieName, Value: "abc"})
	if CookieToken(r) != "abc" {
		t.Fatal("токен не прочитан")
	}
}
