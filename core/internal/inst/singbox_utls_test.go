// SPDX-License-Identifier: GPL-3.0-only

//go:build singbox_run

package inst

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"
)

// Регрессия T-02: без тега with_utls sing-box не создаёт исходящие с REALITY
// и с fp= («uTLS … is not included in this build»). Тест идёт теми же тегами,
// что и сборка ядра:
//
//	go test -tags "$(cat BUILD_TAGS)" ./internal/inst/ -run TestSingBoxRunnerStartsUTLSProfiles
func TestSingBoxRunnerStartsUTLSProfiles(t *testing.T) {
	profiles := map[string]string{
		"vless_reality_utls": `{"enabled": true, "server_name": "www.example.com",
  "utls": {"enabled": true, "fingerprint": "chrome"},
  "reality": {"enabled": true, "public_key": "UuMBgl7MXTPx9inmQp2UC7Jcnwc6XYbwDNebonM-FCc", "short_id": "0123456789abcdef"}}`,
		"vless_tls_fingerprint": `{"enabled": true, "server_name": "www.example.com",
  "utls": {"enabled": true, "fingerprint": "firefox"}}`,
	}
	for name, tls := range profiles {
		t.Run(name, func(t *testing.T) {
			// Исходящее не набирает номер при старте: адрес сервера любой.
			cfg := fmt.Sprintf(`{
  "log": {"level": "warn"},
  "inbounds": [{"type": "mixed", "tag": "in", "listen": "127.0.0.1", "listen_port": %d}],
  "outbounds": [{"type": "vless", "tag": "proxy", "server": "127.0.0.1", "server_port": 9,
    "uuid": "b831381d-6324-4d53-ad4f-8cda48b30818", "tls": %s}],
  "route": {"final": "proxy"}
}`, freeTCPPort(t), tls)

			runner := NewSingBoxRunner()
			if err := runner.Start(context.Background(), []byte(cfg)); err != nil {
				if strings.Contains(err.Error(), "is not included in this build") {
					t.Fatalf("ядро собрано без нужного тега (список — core/BUILD_TAGS): %v", err)
				}
				t.Fatalf("Start: %v", err)
			}
			stopCtx, stop := context.WithTimeout(context.Background(), 10*time.Second)
			defer stop()
			if err := runner.Stop(stopCtx); err != nil {
				t.Fatalf("Stop: %v", err)
			}
		})
	}
}
