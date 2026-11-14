// SPDX-License-Identifier: GPL-3.0-only

package api

import (
	"bufio"
	"context"
	"io"
	"net"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	lavrayv1 "github.com/anatolijlaptev1991-ctrl/lavray-android/core/internal/gen/lavrayv1"
	"github.com/anatolijlaptev1991-ctrl/lavray-android/core/internal/inst"
)

// recordedDial запоминает адреса, которые клиент просил открыть у инстанса, и
// отвечает на них как HTTP-сервер по net.Pipe: ни одного настоящего сокета.
type recordedDial struct {
	mu    sync.Mutex
	addrs []string
}

func (r *recordedDial) dial(_ context.Context, network, address string) (net.Conn, error) {
	r.mu.Lock()
	r.addrs = append(r.addrs, network+" "+address)
	r.mu.Unlock()
	client, server := net.Pipe()
	go func() {
		defer server.Close()
		br := bufio.NewReader(server)
		for {
			line, err := br.ReadString('\n')
			if err != nil || strings.TrimSpace(line) == "" {
				break
			}
		}
		const body = "203.0.113.7"
		_, _ = io.WriteString(server, "HTTP/1.1 200 OK\r\nContent-Length: 11\r\nConnection: close\r\n\r\n"+body)
	}()
	return client, nil
}

func startedServer(t *testing.T, r *inst.FakeRunner) *Server {
	t.Helper()
	m := inst.NewManager(r)
	if err := m.Start(context.Background(), inst.RunInfo{Mode: "tun", ConfigJSON: []byte(`{}`)}); err != nil {
		t.Fatal(err)
	}
	return &Server{inst: m, http: &http.Client{Timeout: 3 * time.Second}, ipURL: "http://ip.example.test/"}
}

func TestTunnelClientDialsThroughInstanceWithoutListener(t *testing.T) {
	t.Setenv("HTTP_PROXY", "http://127.0.0.1:9")
	t.Setenv("http_proxy", "http://127.0.0.1:9")
	rec := &recordedDial{}
	s := startedServer(t, &inst.FakeRunner{ProxyDial: rec.dial})

	c, ok := s.tunnelClient()
	if !ok || c == nil {
		t.Fatal("туннель поднят, клиент обязан быть")
	}
	if c.Transport.(*http.Transport).Proxy != nil {
		t.Fatal("прокси в окружении не допускается: локального порта нет")
	}
	ip, err := s.fetchPublicIP(context.Background(), c)
	if err != nil || ip.String() != "203.0.113.7" {
		t.Fatalf("публичный IP через туннель: %v %v", ip, err)
	}
	rec.mu.Lock()
	defer rec.mu.Unlock()
	if len(rec.addrs) != 1 || rec.addrs[0] != "tcp ip.example.test:80" {
		t.Fatalf("инстанс получил %v, ожидалось одно имя хоста без разрешения на телефоне", rec.addrs)
	}
}

func TestTunnelClientAbsentWhenNotRunning(t *testing.T) {
	m := inst.NewManager(&inst.FakeRunner{})
	s := &Server{inst: m, http: &http.Client{}}
	if c, ok := s.tunnelClient(); ok || c != nil {
		t.Fatal("без запущенного инстанса клиента «через туннель» нет")
	}
	if _, err := s.httpVia(lavrayv1.GetPublicIpRequest_VIA_THROUGH_TUNNEL); err == nil {
		t.Fatal("THROUGH_TUNNEL без туннеля должен вернуть ошибку")
	}
}

func TestDialErrorIsReportedNotFallenBack(t *testing.T) {
	s := startedServer(t, &inst.FakeRunner{}) // ProxyDial nil: ErrDialUnsupported
	c, ok := s.tunnelClient()
	if !ok {
		t.Fatal("клиент обязан быть")
	}
	if _, err := s.fetchPublicIP(context.Background(), c); err == nil {
		t.Fatal("ошибка набора должна дойти до вызывающего, а не уйти в прямое соединение")
	}
}

func TestAndroidTunnelPredicates(t *testing.T) {
	s := &Server{inst: inst.NewManager(&inst.FakeRunner{}), http: &http.Client{}}
	if !s.tunnelIsTUN() {
		t.Fatal("tunnelIsTUN: на Android всегда TUN, и до подключения")
	}
	if s.directIsTunneled() {
		t.Fatal("собственные сокеты ядра исключены из VPN")
	}
	if c, err := s.httpVia(lavrayv1.GetPublicIpRequest_VIA_DIRECT); err != nil || c != s.http {
		t.Fatalf("прямой замер должен идти обычным клиентом: %v", err)
	}
	if s.ipv6Measurable() || s.hasLocalListener() {
		t.Fatal("IPv6 из процесса не измеряется, локального порта нет")
	}
	if r := s.checkPort(context.Background()); r.Status != ckSkip {
		t.Fatalf("проверка порта на Android не применима, получено %v", r.Status)
	}
}
