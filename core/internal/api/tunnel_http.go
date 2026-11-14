// SPDX-License-Identifier: GPL-3.0-only

// tunnel_http.go — HTTP-клиент «через туннель» (T-16): подписки, публичный IP
// и проверки идут через mixed-вход РАБОТАЮЩЕГО инстанса, когда он поднят.
package api

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strconv"
	"strings"
	"time"

	"connectrpc.com/connect"

	lavrayv1 "github.com/anatolijlaptev1991-ctrl/lavray-android/core/internal/gen/lavrayv1"
	"github.com/anatolijlaptev1991-ctrl/lavray-android/core/internal/inst"
)

const (
	// publicIPURL — сервис «какой у меня адрес наружу» (Server.ipURL подменяют тесты).
	publicIPURL = "https://api.ipify.org/"
	// ipProbeTimeout — предел одного замера IP.
	ipProbeTimeout = 5 * time.Second
	// ipv6ProbeAddr — публичный IPv6-адрес для проверки «есть ли прямой IPv6».
	ipv6ProbeAddr = "[2606:4700:4700::1111]:443"
)

// tunnelProxyURL — адрес mixed-in работающего инстанса. Адрес, порт и логин
// берутся из конфига ЗАПУЩЕННОГО инстанса (inst.Status().LastConfig), а не из
// настроек: порт могли изменить уже после старта. ok=false — туннель не поднят
// или в его конфиге нет mixed-in (например, порт настроен неверно).
func (s *Server) tunnelProxyURL() (*url.URL, bool) {
	st := s.inst.Status()
	if st.State != inst.StateReady || len(st.LastConfig) == 0 {
		return nil, false
	}
	var cfg struct {
		Inbounds []struct {
			Tag    string `json:"tag"`
			Listen string `json:"listen"`
			Port   int    `json:"listen_port"`
			Users  []struct {
				Username string `json:"username"`
				Password string `json:"password"`
			} `json:"users"`
		} `json:"inbounds"`
	}
	if json.Unmarshal(st.LastConfig, &cfg) != nil {
		return nil, false
	}
	for _, in := range cfg.Inbounds {
		if in.Tag != "mixed-in" || in.Port <= 0 || in.Port > 65535 {
			continue
		}
		host := strings.TrimSpace(in.Listen)
		if ip, err := netip.ParseAddr(host); host == "" || (err == nil && ip.IsUnspecified()) {
			host = "127.0.0.1" // 0.0.0.0 / :: слушают и на loopback
		}
		u := &url.URL{Scheme: "http", Host: net.JoinHostPort(host, strconv.Itoa(in.Port))}
		if len(in.Users) > 0 && in.Users[0].Username != "" {
			u.User = url.UserPassword(in.Users[0].Username, in.Users[0].Password)
		}
		return u, true
	}
	return nil, false
}

// tunnelClient — http.Client «через туннель». LAVRay Android: без mixed-in,
// соединения открывает работающий инстанс в этом же процессе (inst.Manager.DialProxy),
// локального порта нет (tunnel_inproc.go).
func (s *Server) tunnelClient() (*http.Client, bool) {
	return s.tunnelInProcessClient()
}

// tunnelIsTUN — режим подключения TUN. LAVRay Android запускает ядро только с
// tun-inbound, поэтому ответ всегда «да», в том числе до подключения: проверка
// маршрута строится по тому же конфигу, который получит инстанс. Годен ли
// «прямой» замер из ядра, решает directIsTunneled (tunnel_inproc.go).
func (s *Server) tunnelIsTUN() bool { return true }

// httpForSubscriptions — клиент для подписок: через туннель, если он поднят
// и use_proxy включён (настройка sub.use_proxy, по умолчанию true; разовое
// переопределение — UpdateSubscriptionRequest.use_proxy), иначе напрямую.
// Откат на прямое соединение при ошибке туннеля НЕ делается: он раскрыл бы
// реальный IP серверу подписки, от чего use_proxy и защищает.
func (s *Server) httpForSubscriptions(useProxy *bool) *http.Client {
	use := s.settingBool("sub.use_proxy")
	if useProxy != nil {
		use = *useProxy
	}
	if use {
		if c, ok := s.tunnelClient(); ok {
			return c
		}
	}
	return s.http
}

// httpVia — клиент для замера публичного IP по полю via.
func (s *Server) httpVia(via lavrayv1.GetPublicIpRequest_Via) (*http.Client, error) {
	tunnel, up := s.tunnelClient()
	switch via {
	case lavrayv1.GetPublicIpRequest_VIA_THROUGH_TUNNEL:
		if !up {
			return nil, failedPrecondition("туннель не поднят: подключитесь, чтобы узнать адрес через туннель")
		}
		return tunnel, nil
	case lavrayv1.GetPublicIpRequest_VIA_DIRECT:
		if s.directIsTunneled() {
			return nil, failedPrecondition("в режиме TUN весь трафик ядра идёт в туннель: прямой адрес измерить нельзя")
		}
		return s.http, nil
	}
	if up { // VIA_UNSPECIFIED = через туннель, если соединение активно
		return tunnel, nil
	}
	return s.http, nil
}

// fetchPublicIP — адрес, который видит сервис IP. Проверяет статус ответа и
// формат: HTML-заглушка или ошибка сервиса не должны становиться «IP».
func (s *Server) fetchPublicIP(ctx context.Context, c *http.Client) (netip.Addr, error) {
	cctx, cancel := context.WithTimeout(ctx, ipProbeTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(cctx, http.MethodGet, s.ipURL, nil)
	if err != nil {
		return netip.Addr{}, err
	}
	resp, err := c.Do(req)
	if err != nil {
		return netip.Addr{}, fmt.Errorf("публичный IP: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return netip.Addr{}, fmt.Errorf("публичный IP: сервис ответил HTTP %d", resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 64))
	if err != nil {
		return netip.Addr{}, fmt.Errorf("публичный IP: %w", err)
	}
	ip, err := netip.ParseAddr(strings.TrimSpace(string(body)))
	if err != nil {
		return netip.Addr{}, fmt.Errorf("публичный IP: ответ сервиса не является адресом")
	}
	return ip, nil
}

// ipv6DirectAvailable — доступен ли публичный IPv6 напрямую (замер: TCP на
// известный IPv6-адрес). true при поднятом туннеле = риск утечки IPv6.
func (s *Server) ipv6DirectAvailable(ctx context.Context) bool {
	cctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	conn, err := s.dial(cctx, "tcp6", ipv6ProbeAddr)
	if err != nil {
		return false
	}
	_ = conn.Close()
	return true
}

// unavailable — сетевая ошибка замера: клиент увидит UNAVAILABLE, а не INTERNAL.
func unavailable(err error) error { return connect.NewError(connect.CodeUnavailable, err) }
