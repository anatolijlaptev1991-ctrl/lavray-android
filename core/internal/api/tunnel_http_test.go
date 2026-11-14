// SPDX-License-Identifier: GPL-3.0-only

package api

// Настольные проверки пути через локальный прокси mixed-in (via, утечки, порт, IPv6, use_proxy
// подписки) убраны при переносе ядра в LAVRay Android: локального слушателя здесь нет, запросы
// «через туннель» открывает работающий инстанс в процессе. Это поведение проверяет
// tunnel_inproc_test.go.

import (
	"context"
	"net"
	"net/http"
	"testing"

	"connectrpc.com/connect"

	lavrayv1 "github.com/anatolijlaptev1991-ctrl/lavray-android/core/internal/gen/lavrayv1"
)

const (
	directIP = "203.0.113.7"  // что видит сервис IP при прямом запросе
	tunnelIP = "198.51.100.9" // что видит сервис IP при запросе через туннель
)

// ipFixture: «прямой» канал отвечает directIP, mixed-in (fakeProxy) — tunnelIP.
func ipFixture(t *testing.T) (*svcFixture, *fakeProxy, *[]string) {
	t.Helper()
	var direct []string
	f := newSvcFixture(t, &http.Client{Transport: textRT(directIP, &direct)})
	f.srv.ipURL = "http://ip.test/"
	f.srv.dial = func(context.Context, string, string) (net.Conn, error) { return nil, net.ErrClosed }
	return f, startFakeProxy(t, tunnelIP), &direct
}

func publicIP(t *testing.T, s *Server, via lavrayv1.GetPublicIpRequest_Via) (string, error) {
	t.Helper()
	r, err := s.GetPublicIp(context.Background(), connect.NewRequest(&lavrayv1.GetPublicIpRequest{Via: via}))
	if err != nil {
		return "", err
	}
	return r.Msg.GetIp(), nil
}

// Ответ сервиса IP проверяется: статус и формат (раньше любой HTML становился «IP»).
func TestGetPublicIpRejectsBadAnswers(t *testing.T) {
	var hits []string
	f := newSvcFixture(t, &http.Client{Transport: textRT("<html>blocked</html>", &hits)})
	f.srv.ipURL = "http://ip.test/"
	if ip, err := publicIP(t, f.srv, lavrayv1.GetPublicIpRequest_VIA_DIRECT); err == nil || connect.CodeOf(err) != connect.CodeUnavailable {
		t.Fatalf("HTML вместо адреса принят как IP %q (err=%v)", ip, err)
	}
}

func leaks(t *testing.T, s *Server) *lavrayv1.LeakReport {
	t.Helper()
	r, err := s.CheckLeaks(context.Background(), connect.NewRequest(&lavrayv1.Empty{}))
	if err != nil {
		t.Fatal(err)
	}
	return r.Msg
}
