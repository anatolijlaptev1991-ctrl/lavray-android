// SPDX-License-Identifier: GPL-3.0-only

package api

// cors.go — CORS для окна оболочки и веб-панели.

import (
	"net/http"
	"strings"
)

// corsWrap — CORS для Tauri-окна: webview отдаёт страницу с origin
// tauri://localhost (или http://tauri.localhost на Windows), а ядро живёт на
// http://127.0.0.1:<port> — это кросс-оригин запрос. Разрешаем только
// локальные origin'ы оболочки и веб-панели; Bearer-токен остаётся
// обязательным (CORS — не аутентификация).
func corsWrap(next http.Handler) http.Handler {
	allowed := map[string]bool{
		"tauri://localhost":       true,
		"http://tauri.localhost":  true,
		"https://tauri.localhost": true,
		"http://localhost:1420":   true, // vite dev
		"http://127.0.0.1:1420":   true,
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origin := r.Header.Get("Origin")
		if origin != "" && (allowed[origin] || strings.HasPrefix(origin, "http://127.0.0.1:") || strings.HasPrefix(origin, "http://localhost:")) {
			w.Header().Set("Access-Control-Allow-Origin", origin)
			w.Header().Set("Vary", "Origin")
			w.Header().Set("Access-Control-Allow-Credentials", "true")
			w.Header().Set("Access-Control-Allow-Methods", "POST, GET, OPTIONS")
			w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization, Connect-Protocol-Version, Connect-Timeout-Ms, X-LAVRay-CSRF")
			w.Header().Set("Access-Control-Expose-Headers", "Connect-Content-Encoding")
			w.Header().Set("Access-Control-Max-Age", "600")
		}
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}
