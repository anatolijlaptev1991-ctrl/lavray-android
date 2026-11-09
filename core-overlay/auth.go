// SPDX-License-Identifier: GPL-3.0-only

package mobile

import (
	"crypto/subtle"
	"net/http"
	"strings"

	"github.com/anatolijlaptev1991-ctrl/lavray/core/internal/web"
)

// bearerToken извлекает токен из «Authorization: Bearer <токен>». Схема
// нечувствительна к регистру; голый токен без схемы не принимается (как в
// cmd/lavray-core/auth.go).
func bearerToken(header string) (string, bool) {
	scheme, rest, found := strings.Cut(header, " ")
	if !found || !strings.EqualFold(scheme, "Bearer") {
		return "", false
	}
	return strings.TrimLeft(rest, " "), true
}

// authMiddleware — Bearer-токен на каждом запросе, кроме /healthz и OPTIONS.
// Пустой токен отклоняет всё (fail-closed); сравнение за постоянное время.
func authMiddleware(next http.Handler, token string) http.Handler {
	want := []byte(token)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/healthz" {
			w.WriteHeader(http.StatusOK)
			return
		}
		if r.Method == http.MethodOptions {
			next.ServeHTTP(w, r)
			return
		}
		got, ok := bearerToken(r.Header.Get("Authorization"))
		if ok && got != "" && len(want) > 0 && subtle.ConstantTimeCompare([]byte(got), want) == 1 {
			next.ServeHTTP(w, r)
			return
		}
		web.DiscardBody(r)
		http.Error(w, `{"code":"unauthenticated"}`, http.StatusUnauthorized)
	})
}
