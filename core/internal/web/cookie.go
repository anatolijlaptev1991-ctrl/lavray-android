// SPDX-License-Identifier: GPL-3.0-only

package web

import (
	"net/http"
	"time"
)

// CookieName — имя cookie сессии панели.
const CookieName = "lavray_session"

// NewCookie — cookie сессии: HttpOnly (скрипт страницы токен не читает),
// SameSite=Strict (кросс-сайтовые запросы идут без неё), Path=/. Secure
// выставляется, когда запрос пришёл по TLS: по обычному HTTP браузер
// Secure-cookie не вернёт, и вход на LAN без TLS стал бы невозможен — в этом
// режиме UI показывает предупреждение «Не защищено».
func NewCookie(token string, maxAge time.Duration, secure bool) *http.Cookie {
	return &http.Cookie{
		Name:     CookieName,
		Value:    token,
		Path:     "/",
		MaxAge:   int(maxAge.Seconds()),
		HttpOnly: true,
		Secure:   secure,
		SameSite: http.SameSiteStrictMode,
	}
}

// ClearCookie — cookie, которая стирает сессию в браузере (выход).
func ClearCookie(secure bool) *http.Cookie {
	c := NewCookie("", 0, secure)
	c.MaxAge = -1
	return c
}

// CookieToken возвращает значение cookie сессии запроса ("" — нет).
func CookieToken(r *http.Request) string {
	c, err := r.Cookie(CookieName)
	if err != nil {
		return ""
	}
	return c.Value
}
