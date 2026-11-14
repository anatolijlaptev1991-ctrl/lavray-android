// SPDX-License-Identifier: GPL-3.0-only

//go:build singbox_run

package inst

// traffic_singbox_test.go — источник соединений на настоящем sing-box (T-79).
// Только loopback и свободные порты; порт 2080 (NekoBox) не используется.

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/anatolijlaptev1991-ctrl/lavray-android/core/internal/testsocks"
	"github.com/anatolijlaptev1991-ctrl/lavray-android/core/internal/traffic"
)

// trafficConfig — mixed-вход на port и direct-выход.
func trafficConfig(port int, level string) []byte {
	return []byte(fmt.Sprintf(`{
  "log": {"level": %q},
  "inbounds": [{"type": "mixed", "tag": "mixed-in", "listen": "127.0.0.1", "listen_port": %d}],
  "outbounds": [{"type": "direct", "tag": "direct"}],
  "route": {"final": "direct"}
}`, level, port))
}

// startTrafficRunner запускает настоящий раннер и останавливает его в конце теста.
func startTrafficRunner(t *testing.T) (*SingBoxRunner, int) {
	t.Helper()
	port := freeTCPPort(t)
	r := NewSingBoxRunner()
	if err := r.Start(context.Background(), trafficConfig(port, "info")); err != nil {
		t.Fatalf("Start: %v", err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = r.Stop(ctx)
	})
	return r, port
}

// webTarget — HTTP-сервер, отвечающий 4 КБ.
func webTarget(t *testing.T) string {
	t.Helper()
	body := strings.Repeat("x", 4096)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, body)
	}))
	t.Cleanup(srv.Close)
	return srv.Listener.Addr().String()
}

// openTunnel открывает туннель CONNECT через mixed-вход и делает один запрос.
func openTunnel(t *testing.T, proxyPort int, target string) net.Conn {
	t.Helper()
	conn, err := net.DialTimeout("tcp", fmt.Sprintf("127.0.0.1:%d", proxyPort), 5*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	_ = conn.SetDeadline(time.Now().Add(10 * time.Second))
	br := bufio.NewReader(conn)
	fmt.Fprintf(conn, "CONNECT %s HTTP/1.1\r\nHost: %s\r\n\r\n", target, target)
	status, err := br.ReadString('\n')
	if err != nil || !strings.Contains(status, "200") {
		t.Fatalf("CONNECT: %q err=%v", status, err)
	}
	for { // заголовки ответа до пустой строки
		line, err := br.ReadString('\n')
		if err != nil {
			t.Fatal(err)
		}
		if line == "\r\n" {
			break
		}
	}
	fmt.Fprintf(conn, "GET / HTTP/1.1\r\nHost: %s\r\n\r\n", target)
	resp, err := http.ReadResponse(br, nil)
	if err != nil {
		t.Fatalf("ответ цели: %v", err)
	}
	if _, err := io.Copy(io.Discard, resp.Body); err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	_ = conn.SetDeadline(time.Time{})
	return conn
}

func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("не дождались: %s", what)
}

func TestTrafficManagerAvailableViaPlatformWriter(t *testing.T) {
	r, port := startTrafficRunner(t)
	src := r.TrafficSource()
	if src == nil {
		t.Fatal("менеджер соединений не создан: PlatformLogWriter не передан?")
	}
	events, cancel := src.Subscribe()
	defer cancel()
	target := webTarget(t)
	conn := openTunnel(t, port, target)

	var active []traffic.ConnInfo
	waitFor(t, "активное соединение с байтами", func() bool {
		active = src.Active()
		return len(active) == 1 && active[0].Up > 0 && active[0].Down >= 4096
	})
	c := active[0]
	if c.Destination != target || c.Outbound != "direct" || c.Network != "tcp" || c.Inbound != "mixed-in" ||
		c.ID == "" || c.StartMs == 0 || c.EndMs != 0 {
		t.Fatalf("метаданные соединения: %+v", c)
	}
	if up, down := src.Total(); up < c.Up || down < c.Down {
		t.Fatalf("Total ↑%d ↓%d меньше счётчиков соединения ↑%d ↓%d", up, down, c.Up, c.Down)
	}
	_ = conn.Close()
	waitFor(t, "соединение в списке закрытых", func() bool {
		cl := src.Closed()
		return len(src.Active()) == 0 && len(cl) == 1 && cl[0].ID == c.ID && cl[0].EndMs >= cl[0].StartMs && cl[0].Down >= 4096
	})
	var kinds []traffic.EventKind
	for len(kinds) < 2 {
		select {
		case ev := <-events:
			if ev.ID == c.ID {
				kinds = append(kinds, ev.Kind)
			}
		case <-time.After(5 * time.Second):
			t.Fatalf("события соединения: %v", kinds)
		}
	}
	if kinds[0] != traffic.EventOpened || kinds[1] != traffic.EventClosed {
		t.Fatalf("порядок событий: %v", kinds)
	}
}

func TestRunnerTrafficNilAfterStop(t *testing.T) {
	r := NewSingBoxRunner()
	if r.TrafficSource() != nil {
		t.Fatal("до Start источник должен быть nil")
	}
	port := freeTCPPort(t)
	if err := r.Start(context.Background(), trafficConfig(port, "error")); err != nil {
		t.Fatal(err)
	}
	if r.TrafficSource() == nil {
		t.Fatal("после Start источника нет")
	}
	if err := r.Stop(context.Background()); err != nil {
		t.Fatal(err)
	}
	if r.TrafficSource() != nil {
		t.Fatal("после Stop источник должен быть nil")
	}
	if err := r.Stop(context.Background()); err != nil || r.TrafficSource() != nil {
		t.Fatalf("повторный Stop: %v", err)
	}
}

// Новый запуск начинает с пустой истории: прежние соединения и итоги не переходят.
func TestRunnerRestartClearsHistory(t *testing.T) {
	r := NewSingBoxRunner()
	port := freeTCPPort(t)
	target := webTarget(t)
	start := func() traffic.Source {
		if err := r.Start(context.Background(), trafficConfig(port, "error")); err != nil {
			t.Fatal(err)
		}
		return r.TrafficSource()
	}
	first := start()
	conn := openTunnel(t, port, target)
	_ = conn.Close()
	waitFor(t, "закрытое соединение", func() bool { return len(first.Closed()) == 1 })
	if up, down := first.Total(); up == 0 || down == 0 {
		t.Fatal("первый запуск не посчитал трафик")
	}
	if err := r.Stop(context.Background()); err != nil {
		t.Fatal(err)
	}

	second := start()
	t.Cleanup(func() { _ = r.Stop(context.Background()) })
	if len(second.Active()) != 0 || len(second.Closed()) != 0 {
		t.Fatalf("история пережила перезапуск: active=%d closed=%d", len(second.Active()), len(second.Closed()))
	}
	if up, down := second.Total(); up != 0 || down != 0 {
		t.Fatalf("итог пережил перезапуск: ↑%d ↓%d", up, down)
	}
}

// Close по id закрывает настоящий сокет клиента, а не только запись в списке.
func TestCloseConnectionClosesRealSocket(t *testing.T) {
	r, port := startTrafficRunner(t)
	src := r.TrafficSource()
	conn := openTunnel(t, port, webTarget(t))
	defer conn.Close()
	var id string
	waitFor(t, "активное соединение", func() bool {
		a := src.Active()
		if len(a) == 1 {
			id = a[0].ID
		}
		return id != ""
	})

	if err := src.Close(id); err != nil {
		t.Fatalf("Close: %v", err)
	}
	_ = conn.SetReadDeadline(time.Now().Add(5 * time.Second))
	if n, err := conn.Read(make([]byte, 1)); err == nil || n != 0 {
		t.Fatalf("клиентский сокет жив после Close: n=%d err=%v", n, err)
	} else if ne, ok := err.(net.Error); ok && ne.Timeout() {
		t.Fatal("клиентский сокет не закрыт: чтение завершилось по таймауту")
	}
	waitFor(t, "соединение закрыто", func() bool { return len(src.Active()) == 0 })
	if err := src.Close(id); err != traffic.ErrNotFound {
		t.Fatalf("повторное закрытие: %v, ожидалась ErrNotFound", err)
	}
	if err := src.Close("00000000-0000-0000-0000-000000000000"); err != traffic.ErrNotFound {
		t.Fatalf("неизвестный id: %v", err)
	}

	// UUID сравнивается без учёта регистра: клиент может прислать его заглавным.
	second := openTunnel(t, port, webTarget(t))
	defer second.Close()
	var id2 string
	waitFor(t, "второе активное соединение", func() bool {
		if a := src.Active(); len(a) == 1 {
			id2 = a[0].ID
		}
		return id2 != ""
	})
	if err := src.Close(strings.ToUpper(id2)); err != nil {
		t.Fatalf("Close заглавным UUID: %v", err)
	}
}

// Источник читают и подписываются на него, пока соединения открываются,
// закрываются и инстанс останавливается (смысл — прогон под -race в CI).
func TestTrafficSourceRace(t *testing.T) {
	r, port := startTrafficRunner(t)
	src := r.TrafficSource()
	target := webTarget(t)
	stop := make(chan struct{})
	var readers sync.WaitGroup
	for i := 0; i < 4; i++ {
		readers.Add(1)
		go func() {
			defer readers.Done()
			for {
				select {
				case <-stop:
					return
				default:
				}
				_ = src.Active()
				_ = src.Closed()
				_, _ = src.Total()
				_, cancel := src.Subscribe()
				cancel()
			}
		}()
	}
	var clients sync.WaitGroup
	for i := 0; i < 4; i++ {
		clients.Add(1)
		go func() {
			defer clients.Done()
			for j := 0; j < 5; j++ {
				c, err := net.DialTimeout("tcp", fmt.Sprintf("127.0.0.1:%d", port), 5*time.Second)
				if err != nil {
					return
				}
				fmt.Fprintf(c, "CONNECT %s HTTP/1.1\r\nHost: %s\r\n\r\n", target, target)
				_, _ = bufio.NewReader(c).ReadString('\n')
				_ = c.Close()
			}
		}()
	}
	clients.Wait()
	for _, c := range src.Active() { // закрытие по id параллельно с чтением
		_ = src.Close(c.ID)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := r.Stop(ctx); err != nil {
		t.Fatalf("Stop: %v", err)
	}
	close(stop)
	readers.Wait()
	_, _ = src.Total() // чтение после остановки не должно падать
}

// Временный инстанс теста задержки не получает PlatformLogWriter, а значит, и
// cache_file: в рабочем каталоге не появляется ни одного файла. Контроль:
// основной раннер с тем же конфигом без cache_file файл создаёт — без него тест
// ничего бы не доказывал.
func TestTestInstanceCreatesNoCacheFile(t *testing.T) {
	t.Chdir(t.TempDir())
	socks := testsocks.Start(t)
	ti, err := NewTestInstance(context.Background(), socksTestConfig(t, socks.Port()))
	if err != nil {
		t.Fatalf("NewTestInstance: %v", err)
	}
	if err := ti.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if entries, _ := os.ReadDir("."); len(entries) != 0 {
		t.Fatalf("тестовый инстанс оставил файлы: %v", entries)
	}

	r := NewSingBoxRunner()
	if err := r.Start(context.Background(), trafficConfig(freeTCPPort(t), "error")); err != nil {
		t.Fatal(err)
	}
	if err := r.Stop(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(".", "cache.db")); err != nil {
		t.Fatalf("контроль: основной раннер не создал cache.db (%v) — проверка выше бессмысленна", err)
	}
}
