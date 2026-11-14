// SPDX-License-Identifier: GPL-3.0-only

package web

import (
	"errors"
	"net/http"
	"net/url"
	"strings"
)

// Ошибки проверки происхождения запроса.
var (
	ErrOriginMismatch = errors.New("заголовок Origin не совпадает с Host")
	ErrOriginMissing  = errors.New("у изменяющего запроса нет заголовка Origin")
)

// CheckOrigin проверяет, что запрос пришёл со страницы этого же сервера:
// хост из Origin совпадает с Host запроса (без учёта регистра, порт по
// умолчанию схемы Origin отбрасывается). Схема не сравнивается: за
// TLS-терминатором (uhttpd, nginx) ядро видит http, а браузер — https.
// required=true: отсутствие Origin — отказ (так проверяются изменяющие запросы
// с cookie-сессией; браузеры присылают Origin на любой POST). required=false:
// отсутствующий Origin допускается (curl, скрипты), но присланный проверяется.
func CheckOrigin(r *http.Request, required bool) error {
	origin := r.Header.Get("Origin")
	if origin == "" {
		if required {
			return ErrOriginMissing
		}
		return nil
	}
	u, err := url.Parse(origin)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" ||
		u.User != nil || (u.Path != "" && u.Path != "/") || u.RawQuery != "" || u.Fragment != "" {
		return ErrOriginMismatch
	}
	if r.Host == "" || !strings.EqualFold(stripDefaultPort(u.Scheme, u.Host), stripDefaultPort(u.Scheme, r.Host)) {
		return ErrOriginMismatch
	}
	return nil
}

// stripDefaultPort убирает :80 у http и :443 у https.
func stripDefaultPort(scheme, host string) string {
	def := map[string]string{"http": ":80", "https": ":443"}[scheme]
	return strings.TrimSuffix(host, def)
}
