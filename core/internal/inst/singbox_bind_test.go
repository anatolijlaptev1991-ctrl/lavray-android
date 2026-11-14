// SPDX-License-Identifier: GPL-3.0-only

//go:build singbox_run

package inst

import (
	"context"
	"fmt"
	"net"
	"testing"
)

// T-55 (B1-36): настоящий sing-box на занятом порту возвращает ошибку, которую
// IsAddrInUse распознаёт по коду ОС в цепочке обёрток. Порт занимает сам тест
// (свободный, не 2080), чужие порты не затрагиваются.
func TestSingBoxRunnerReportsBusyPortAsAddrInUse(t *testing.T) {
	busy, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer busy.Close()
	port := busy.Addr().(*net.TCPAddr).Port

	cfg := fmt.Sprintf(`{
  "log": {"level": "warn"},
  "inbounds": [{"type": "mixed", "tag": "mixed-in", "listen": "127.0.0.1", "listen_port": %d}],
  "outbounds": [{"type": "direct", "tag": "direct"}],
  "route": {"final": "direct"}
}`, port)

	runner := NewSingBoxRunner()
	err = runner.Start(context.Background(), []byte(cfg))
	if err == nil {
		_ = runner.Stop(context.Background())
		t.Fatalf("sing-box запустился на занятом порту %d", port)
	}
	t.Logf("ошибка sing-box: %v", err)
	if !IsAddrInUse(err) {
		t.Fatalf("ошибка о занятом порту не распознана IsAddrInUse: %v", err)
	}
}
