// SPDX-License-Identifier: GPL-3.0-only

package web

import "net/http"

// contentSecurityPolicy — политика страницы панели. Скрипты и соединения —
// только со своего origin; встроенные стили разрешены (React ставит style=…);
// фреймы, плагины и сторонние формы закрыты.
const contentSecurityPolicy = "default-src 'self'; script-src 'self'; style-src 'self' 'unsafe-inline'; " +
	"img-src 'self' data: blob:; font-src 'self' data:; connect-src 'self'; object-src 'none'; " +
	"base-uri 'none'; frame-ancestors 'none'; form-action 'self'"

// SecurityHeaders добавляет защитные заголовки ко всем ответам веб-слушателя:
// CSP, запрет фреймов (X-Frame-Options), Referrer-Policy, nosniff и изоляцию
// окна. HSTS не ставится: сертификат самоподписанный, а «запомнить» его
// браузер не должен.
func SecurityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("Content-Security-Policy", contentSecurityPolicy)
		h.Set("X-Frame-Options", "DENY")
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("Referrer-Policy", "no-referrer")
		h.Set("Cross-Origin-Opener-Policy", "same-origin")
		h.Set("Cross-Origin-Resource-Policy", "same-origin")
		h.Set("Permissions-Policy", "camera=(), microphone=(), geolocation=(), payment=()")
		next.ServeHTTP(w, r)
	})
}
