// SPDX-License-Identifier: GPL-3.0-only

package web

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// D-4: отказ шлюза дочитывает тело запроса, иначе при Connection: close
// клиент на Windows иногда получал RST вместо ответа.
func TestRejectionsDrainRequestBody(t *testing.T) {
	f := newGate(t)
	cases := []struct {
		name   string
		mutate func(*http.Request)
		want   int
	}{
		{"неверный Bearer", func(r *http.Request) { r.Header.Set("Authorization", "Bearer wrong") }, http.StatusUnauthorized},
		{"нет сессии", func(*http.Request) {}, http.StatusUnauthorized},
		{"нет CSRF", f.cookie, http.StatusForbidden},
	}
	for _, c := range cases {
		body := strings.NewReader(strings.Repeat("x", 64<<10))
		req := httptest.NewRequest(http.MethodPost, procStatus, body)
		req.Header.Set("Content-Type", "application/json")
		req.Close = true
		c.mutate(req)
		rec := httptest.NewRecorder()
		f.h.ServeHTTP(rec, req)
		if rec.Code != c.want {
			t.Errorf("%s: статус %d, ожидался %d", c.name, rec.Code, c.want)
		}
		if left := body.Len(); left != 0 {
			t.Errorf("%s: тело не дочитано, осталось %d байт", c.name, left)
		}
	}
}

func TestDiscardBodyIsBounded(t *testing.T) {
	body := strings.NewReader(strings.Repeat("x", maxDiscardBody+100))
	req := httptest.NewRequest(http.MethodPost, procStatus, body)
	DiscardBody(req)
	if left := body.Len(); left != 100 {
		t.Fatalf("осталось %d байт, ожидалось 100: дочитывание без предела", left)
	}
	DiscardBody(&http.Request{}) // без тела — не паника
}
