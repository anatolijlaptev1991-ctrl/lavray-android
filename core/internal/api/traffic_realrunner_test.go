// SPDX-License-Identifier: GPL-3.0-only

//go:build singbox_run

package api

// traffic_realrunner_test.go — соединения и статистика на настоящем sing-box
// (T-79). «Сервер профиля» — testsocks на loopback, цель — httptest на
// loopback; порт 2080 (NekoBox) не используется, наружу ничего не уходит.

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"connectrpc.com/connect"

	"github.com/anatolijlaptev1991-ctrl/lavray-android/core/internal/gen/lavrayv1"
	"github.com/anatolijlaptev1991-ctrl/lavray-android/core/internal/gen/lavrayv1connect"
	"github.com/anatolijlaptev1991-ctrl/lavray-android/core/internal/testsocks"
)

func TestRealRunnerConnectionsAndStats(t *testing.T) {
	const bodySize = 64 << 10
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/big" {
			_, _ = io.WriteString(w, strings.Repeat("x", bodySize))
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer target.Close()
	socks := testsocks.Start(t)
	s, pid := realRunnerEnv(t, socks.Port())
	setStrSetting(t, s, "test.url", target.URL+"/generate_204")
	api := httptest.NewServer(s.NewHandler())
	defer api.Close()
	conns := lavrayv1connect.NewConnectionServiceClient(api.Client(), api.URL)
	stats := lavrayv1connect.NewStatsServiceClient(api.Client(), api.URL)
	core := lavrayv1connect.NewCoreServiceClient(api.Client(), api.URL)

	// До подключения — пустой список, не ошибка.
	if res, err := conns.ListConnections(context.Background(), connect.NewRequest(&lavrayv1.ListConnectionsRequest{})); err != nil || len(res.Msg.Connections) != 0 {
		t.Fatalf("до подключения: %v err=%v", res, err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	// Подписка до подключения: поток ждёт инстанс; заголовки ответа уходят с
	// первым кадром, поэтому открывать его нужно не в потоке теста.
	var serr error
	stream := frames(func() (*connect.ServerStreamForClient[lavrayv1.StatsSnapshot], error) {
		return stats.Subscribe(ctx, connect.NewRequest(&lavrayv1.SubscribeStatsRequest{IntervalMs: int32p(250)}))
	}, &serr)

	if _, err := core.Connect(ctx, connect.NewRequest(&lavrayv1.ConnectRequest{ProfileId: &pid})); err != nil {
		t.Fatalf("Connect: %v", err)
	}
	proxyURL, _ := url.Parse(fmt.Sprintf("http://127.0.0.1:%d", s.settingInt("inbound.port")))
	client := &http.Client{Transport: &http.Transport{Proxy: http.ProxyURL(proxyURL)}, Timeout: 10 * time.Second}
	defer client.CloseIdleConnections()
	resp, err := client.Get(target.URL + "/big")
	if err != nil {
		t.Fatalf("запрос через туннель: %v", err)
	}
	if n, _ := io.Copy(io.Discard, resp.Body); n != bodySize {
		t.Fatalf("получено %d байт из %d", n, bodySize)
	}
	_ = resp.Body.Close()

	// Соединение видно со счётчиками. Запрос через HTTP-вход sing-box — одно
	// соединение на запрос, к этому моменту оно может быть уже закрыто.
	var found *lavrayv1.Connection
	waitUntil(t, "соединение со счётчиками", func() bool {
		res, err := conns.ListConnections(ctx, connect.NewRequest(&lavrayv1.ListConnectionsRequest{
			DestinationFilter: strp(target.Listener.Addr().String())}))
		if err != nil {
			return false
		}
		for _, c := range res.Msg.Connections {
			if c.DownloadBytes > 4096 && c.UploadBytes > 0 {
				found = c
			}
		}
		return found != nil
	})
	if found.Outbound != "proxy" || found.Network != "tcp" || found.GetInbound() != "mixed-in" || len(found.Chain) == 0 {
		t.Fatalf("метаданные соединения: %+v", found)
	}

	// Поток статистики: скорость и итоги появились.
	var frame, last *lavrayv1.StatsSnapshot
	deadline := time.After(10 * time.Second)
	for frame == nil {
		select {
		case f := <-stream:
			last = f
			if f != nil && !f.Final && f.Session != nil && f.Session.DownlinkTotal > 4096 {
				frame = f
			}
		case <-deadline:
			t.Fatalf("кадр статистики с трафиком не пришёл (последний: %v, ошибка потока: %v)", last, serr)
		}
	}
	// Разбивки по тегам здесь не требуем: sing-box очищает список закрытых при
	// сборке мусора, и соединение, жившее миллисекунды, может не попасть в кадр
	// (скорость при этом идёт в тег other, итоги сессии точные).
	if frame.Session.GetProfileId() != pid {
		t.Fatalf("кадр: %+v", frame)
	}

	// Закрытие по id закрывает сокет клиента: туннель CONNECT держит соединение
	// активным, пока его не закроют.
	tunnel := openConnectTunnel(t, s.settingInt("inbound.port"), target.Listener.Addr().String())
	defer tunnel.Close()
	var active *lavrayv1.Connection
	waitUntil(t, "активное соединение туннеля", func() bool {
		res, err := conns.ListConnections(ctx, connect.NewRequest(&lavrayv1.ListConnectionsRequest{
			DestinationFilter: strp(target.Listener.Addr().String())}))
		if err != nil {
			return false
		}
		for _, c := range res.Msg.Connections {
			if c.Status == lavrayv1.Connection_STATUS_ACTIVE {
				active = c
			}
		}
		return active != nil
	})
	if _, err := conns.CloseConnection(ctx, connect.NewRequest(&lavrayv1.CloseConnectionRequest{Id: active.Id})); err != nil {
		t.Fatalf("CloseConnection: %v", err)
	}
	_ = tunnel.SetReadDeadline(time.Now().Add(5 * time.Second))
	if _, err := tunnel.Read(make([]byte, 1)); err == nil {
		t.Fatal("сокет клиента жив после CloseConnection")
	} else if ne, ok := err.(net.Error); ok && ne.Timeout() {
		t.Fatal("сокет клиента не закрыт: чтение завершилось по таймауту")
	}
	if _, err := conns.CloseConnection(ctx, connect.NewRequest(&lavrayv1.CloseConnectionRequest{Id: active.Id})); !isCode(err, connect.CodeNotFound) {
		t.Fatalf("повторное закрытие: %v, ожидался NOT_FOUND", err)
	}

	// Disconnect несёт итоги, профиль получил счётчики.
	res, err := core.Disconnect(ctx, connect.NewRequest(&lavrayv1.DisconnectRequest{}))
	if err != nil {
		t.Fatal(err)
	}
	fs := res.Msg.FinalStats
	if fs == nil || !fs.Final || fs.Session == nil || fs.Session.DownlinkTotal <= 4096 || fs.Session.UplinkTotal == 0 {
		t.Fatalf("final_stats: %+v", fs)
	}
	totals, err := stats.GetTrafficTotals(ctx, connect.NewRequest(&lavrayv1.GetTrafficTotalsRequest{ProfileIds: []int64{pid}}))
	if err != nil || totals.Msg.Totals[pid].DownlinkBytes != fs.Session.DownlinkTotal {
		t.Fatalf("итоги профиля %v (err=%v), ожидалось %d", totals, err, fs.Session.DownlinkTotal)
	}
	if dests := socks.TCPDestinations(); len(dests) == 0 {
		t.Fatal("трафик не прошёл через сервер профиля")
	}
}

// openConnectTunnel открывает туннель CONNECT через HTTP-вход ядра и возвращает
// сокет после ответа 200.
func openConnectTunnel(t *testing.T, proxyPort int, target string) net.Conn {
	t.Helper()
	conn, err := net.DialTimeout("tcp", fmt.Sprintf("127.0.0.1:%d", proxyPort), 5*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	_ = conn.SetDeadline(time.Now().Add(10 * time.Second))
	fmt.Fprintf(conn, "CONNECT %s HTTP/1.1\r\nHost: %s\r\n\r\n", target, target)
	br := bufio.NewReader(conn)
	status, err := br.ReadString('\n')
	if err != nil || !strings.Contains(status, "200") {
		t.Fatalf("CONNECT: %q err=%v", status, err)
	}
	_ = conn.SetDeadline(time.Time{})
	return conn
}
