// SPDX-License-Identifier: GPL-3.0-only

package api

// Файл копируется scripts/build-core.sh в internal/api/ копии ядра вместе с
// патчем core-overlay/patches/0001-tunnel-in-process-dial.patch (субмодуль не
// меняется). На Android локального слушателя нет (mixed-in удалён раннером: ни
// одно приложение телефона не должно находить порт), поэтому «через туннель»
// ядро открывает соединения само: inst.Manager.DialProxy ведёт их в исходящее
// "proxy" работающего инстанса в этом же процессе, без сокета на localhost.

import (
	"context"
	"net"
	"net/http"
	"time"

	"github.com/anatolijlaptev1991-ctrl/lavray/core/internal/inst"
)

// tunnelInProcessClient — http.Client, чей транспорт набирает адреса через
// работающий инстанс. Имя хоста уходит на сервер профиля и разрешается там, а не
// на телефоне. Прокси из окружения не используется (Proxy nil). ok=false — туннель
// не поднят. Адреса с IP-литералом и доменом обрабатываются одинаково.
func (s *Server) tunnelInProcessClient() (*http.Client, bool) {
	if s.inst == nil || s.inst.Status().State != inst.StateReady {
		return nil, false
	}
	var timeout time.Duration
	if s.http != nil {
		timeout = s.http.Timeout
	}
	return &http.Client{
		Timeout: timeout,
		Transport: &http.Transport{
			Proxy:                 nil,
			DialContext:           s.dialThroughInstance,
			DisableKeepAlives:     true,
			TLSHandshakeTimeout:   10 * time.Second,
			ResponseHeaderTimeout: 15 * time.Second,
		},
	}, true
}

func (s *Server) dialThroughInstance(ctx context.Context, network, address string) (net.Conn, error) {
	return s.inst.DialProxy(ctx, network, address)
}

// directIsTunneled — попадают ли собственные сокеты ядра в туннель. На Android
// нет: приложение исключено из VPN (addDisallowedApplication или не входит в
// список разрешённых), поэтому прямой замер из ядра уходит по физической сети и
// достоверен.
func (s *Server) directIsTunneled() bool { return false }

// ipv6Measurable — можно ли замерить «прямой IPv6» из ядра. Нельзя: процесс
// исключён из VPN, и его прямой IPv6 есть всегда, где он есть у телефона, а
// вопрос утечки — в том, что делают остальные приложения. Замер не выполняется и
// попадает в unchecked, а не выдаётся за «в порядке».
func (s *Server) ipv6Measurable() bool { return false }

// hasLocalListener — есть ли у инстанса локальный порт (проверка «порт свободен»).
func (s *Server) hasLocalListener() bool { return false }
