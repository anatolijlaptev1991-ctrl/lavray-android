// SPDX-License-Identifier: GPL-3.0-only

// update_net.go — сетевой путь обновлений: сначала через туннель (mixed-in
// работающего инстанса, как для подписок и проверки IP), при его отказе —
// напрямую. Туннель не поднят — сразу напрямую. Прямой путь использует
// таймауты 3 с на соединение и 5 с до первого байта.
package api

import (
	"net"
	"net/http"
	"time"

	"github.com/anatolijlaptev1991-ctrl/lavray-android/core/internal/updates"
)

// updateTunnelHeaderTimeout — ожидание первого байта через туннель.
const updateTunnelHeaderTimeout = 10 * time.Second

// updateHTTPClient — клиент обновлений: туннель, затем напрямую. Таймаута на
// весь запрос нет (артефакт качается минутами); редиректы, лимиты и таймауты
// чтения ограничивает updates.HTTPSource.
func (s *Server) updateHTTPClient() *http.Client {
	return &http.Client{Transport: &tunnelFirstTransport{s: s}}
}

type tunnelFirstTransport struct{ s *Server }

func (t *tunnelFirstTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if u, ok := t.s.tunnelProxyURL(); ok {
		tr := &http.Transport{
			Proxy: http.ProxyURL(u), DisableKeepAlives: true,
			ResponseHeaderTimeout: updateTunnelHeaderTimeout,
		}
		resp, err := tr.RoundTrip(req.Clone(req.Context()))
		if err == nil || req.Context().Err() != nil {
			return resp, err
		}
		// туннель не ответил (порт закрыт, сервер недоступен): пробуем напрямую
	}
	return t.s.updateDirect().RoundTrip(req)
}

// updateDirect — прямой транспорт: тестовый клиент сервера, если задан, иначе
// транспорт с таймаутами по умолчанию для обновлений.
func (s *Server) updateDirect() http.RoundTripper {
	if s.http != nil && s.http.Transport != nil {
		return s.http.Transport
	}
	return directUpdateTransport
}

var directUpdateTransport http.RoundTripper = &http.Transport{
	Proxy:                 http.ProxyFromEnvironment,
	DialContext:           (&net.Dialer{Timeout: updates.DefaultConnectTimeout}).DialContext,
	TLSHandshakeTimeout:   updates.DefaultConnectTimeout,
	ResponseHeaderTimeout: updates.DefaultFirstByteTimeout,
	DisableKeepAlives:     true,
}
