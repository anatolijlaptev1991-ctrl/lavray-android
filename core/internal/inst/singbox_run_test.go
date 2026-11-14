// SPDX-License-Identifier: GPL-3.0-only

//go:build singbox_run

package inst

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"
)

// Регрессия T-01: Manager.Start отменяет start-контекст сразу после
// успешного старта. Если инстанс sing-box живёт на этом контексте, любое
// соединение через туннель обрывается («operation was canceled»), хотя
// статус показывает «Подключено». Тест повторяет порядок вызовов менеджера.
func TestSingBoxRunnerSurvivesStartContextCancel(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	defer upstream.Close()

	port := freeTCPPort(t)
	cfg := fmt.Sprintf(`{
  "log": {"level": "warn"},
  "inbounds": [{"type": "mixed", "tag": "mixed-in", "listen": "127.0.0.1", "listen_port": %d}],
  "outbounds": [{"type": "direct", "tag": "direct"}],
  "route": {"final": "direct"}
}`, port)

	runner := NewSingBoxRunner()
	startCtx, cancel := context.WithCancel(context.Background())
	if err := runner.Start(startCtx, []byte(cfg)); err != nil {
		t.Fatalf("Start: %v", err)
	}
	cancel() // ровно так делает Manager.Start после успешного runner.Start
	defer func() {
		stopCtx, stop := context.WithTimeout(context.Background(), 10*time.Second)
		defer stop()
		_ = runner.Stop(stopCtx)
	}()

	proxyURL, err := url.Parse(fmt.Sprintf("http://127.0.0.1:%d", port))
	if err != nil {
		t.Fatal(err)
	}
	client := &http.Client{
		Transport: &http.Transport{Proxy: http.ProxyURL(proxyURL)},
		Timeout:   10 * time.Second,
	}
	resp, err := client.Get(upstream.URL)
	if err != nil {
		t.Fatalf("запрос через туннель после отмены start-контекста: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("статус ответа через туннель = %d, ожидался 204", resp.StatusCode)
	}
}

func freeTCPPort(t *testing.T) int {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	return ln.Addr().(*net.TCPAddr).Port
}
