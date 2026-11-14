// SPDX-License-Identifier: GPL-3.0-only

//go:build singbox_run

package inst

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// DialProxy настоящего раннера идёт через исходящее "proxy" (проверка связи
// после подключения): запрос к httptest-серверу получает ответ. Исходящее по
// умолчанию здесь — другое ("direct" первым в списке), чтобы тест отличил
// "proxy" от умолчания: через "proxy" имя proof.test резолвится в loopback
// (DNS hosts), через "direct" не резолвилось бы.
func TestSingBoxRunnerDialProxy(t *testing.T) {
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, "pong")
	}))
	defer target.Close()
	cfg := fmt.Sprintf(`{
  "log": {"level": "warn"},
  "dns": {"servers": [{"type": "hosts", "tag": "hosts", "predefined": {"proof.test": "127.0.0.1"}}]},
  "inbounds": [{"type": "mixed", "tag": "mixed-in", "listen": "127.0.0.1", "listen_port": %d}],
  "outbounds": [{"type": "block", "tag": "block"}, {"type": "direct", "tag": "proxy", "domain_resolver": "hosts"}],
  "route": {"final": "block"}
}`, freeTCPPort(t))

	runner := NewSingBoxRunner()
	if _, err := runner.DialProxy(context.Background(), "tcp", "proof.test:80"); !errors.Is(err, ErrNotRunning) {
		t.Fatalf("до старта: %v, ожидалось ErrNotRunning", err)
	}
	if err := runner.Start(context.Background(), []byte(cfg)); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = runner.Stop(ctx)
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	conn, err := runner.DialProxy(ctx, "tcp", fmt.Sprintf("proof.test:%d", listenerPort(t, target)))
	if err != nil {
		t.Fatalf("DialProxy: %v", err)
	}
	defer conn.Close()
	if _, err := fmt.Fprintf(conn, "GET / HTTP/1.0\r\nHost: proof.test\r\n\r\n"); err != nil {
		t.Fatal(err)
	}
	status, err := bufio.NewReader(conn).ReadString('\n')
	if err != nil || status[:12] != "HTTP/1.0 200" && status[:12] != "HTTP/1.1 200" {
		t.Fatalf("ответ: %q, %v", status, err)
	}
}
