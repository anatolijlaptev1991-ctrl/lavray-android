// SPDX-License-Identifier: GPL-3.0-only

//go:build singbox_run

package inst

import (
	"encoding/base64"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

// Сквозные тесты протоколов (0.2.1): сервер sing-box (inbound) и клиент LAVRay
// по конфигу cfgbuild.Build из share-ссылки; запрос идёт через mixed-вход.
// Запуск тем же набором тегов, что и сборка ядра:
//
//	go test -tags "$(cat BUILD_TAGS)" ./internal/inst/ -run TestE2E

func vlessUser(flow string) []any {
	user := map[string]any{"name": "u", "uuid": e2eUUID}
	if flow != "" {
		user["flow"] = flow
	}
	return []any{user}
}

// Живой отказ 0.2.0: провайдер отдаёт vmess+ws+TLS с alpn=h2&fp=firefox, сервер
// при предложении h2 выбирает h2, а клиент WebSocket поднимает апгрейд как
// HTTP/1.1, и сервер рвёт соединение. Ядро при этом стартует, и интерфейс
// пишет «Подключено». Тест ходит тем же путём: ссылка → Build → клиент.
// Фронт — TLS с ALPN ["h2","http/1.1"], как у провайдера (startALPNFront).
func TestE2EVMessWebSocketTLSALPNh2(t *testing.T) {
	env := newE2EEnv(t)
	backend := env.serve(map[string]any{
		"type":      "vmess",
		"users":     []any{map[string]any{"name": "u", "uuid": e2eUUID}},
		"transport": map[string]any{"type": "ws", "path": "/api/v1/gpu-stream/socket"},
	}, "tcp")
	port := startALPNFront(t, backend, "h2", "http/1.1")
	link := vmessLink(map[string]any{
		"add": "localhost", "port": fmt.Sprint(port), "net": "ws", "host": "localhost",
		"path": "/api/v1/gpu-stream/socket", "tls": "tls", "sni": "localhost", "alpn": "h2", "fp": "firefox",
	})
	env.mustFetch(env.connect(link, clientOptions{allowInsecure: true}))
}

// То же для vless на ws и на httpupgrade: апгрейд HTTP/1.1 не должен идти по h2.
func TestE2EUpgradeTransportsIgnoreALPNh2(t *testing.T) {
	for _, network := range []string{"ws", "httpupgrade"} {
		t.Run("vless_"+network, func(t *testing.T) {
			env := newE2EEnv(t)
			backend := env.serve(map[string]any{
				"type":      "vless",
				"users":     vlessUser(""),
				"transport": map[string]any{"type": network, "path": "/up"},
			}, "tcp")
			port := startALPNFront(t, backend, "h2", "http/1.1")
			link := fmt.Sprintf("vless://%s@localhost:%d?type=%s&security=tls&path=/up&host=localhost&sni=localhost"+
				"&alpn=h2,http/1.1&fp=chrome&encryption=none#e2e", e2eUUID, port, network)
			env.mustFetch(env.connect(link, clientOptions{allowInsecure: true}))
		})
	}
}

// (б) vless + gRPC + TLS с alpn=h2: gRPC обязан идти по h2.
func TestE2EVlessGRPCTLSALPNh2(t *testing.T) {
	env := newE2EEnv(t)
	port := env.serve(map[string]any{
		"type":      "vless",
		"users":     vlessUser(""),
		"tls":       serverTLS(t, "h2"),
		"transport": map[string]any{"type": "grpc", "service_name": "api.v1.stream"},
	}, "tcp")
	link := fmt.Sprintf("vless://%s@localhost:%d?type=grpc&security=tls&alpn=h2&fp=firefox"+
		"&serviceName=api.v1.stream&sni=localhost&encryption=none#e2e", e2eUUID, port)
	env.mustFetch(env.connect(link, clientOptions{allowInsecure: true}))
}

// gRPC с ALPN без h2 (http/1.1) не должен ломаться: клиент идёт по h2.
func TestE2EVlessGRPCTLSALPNHTTP11(t *testing.T) {
	env := newE2EEnv(t)
	port := env.serve(map[string]any{
		"type":      "vless",
		"users":     vlessUser(""),
		"tls":       serverTLS(t, "h2", "http/1.1"),
		"transport": map[string]any{"type": "grpc", "service_name": "svc"},
	}, "tcp")
	link := fmt.Sprintf("vless://%s@localhost:%d?type=grpc&security=tls&alpn=http/1.1&fp=chrome"+
		"&serviceName=svc&sni=localhost&encryption=none#e2e", e2eUUID, port)
	env.mustFetch(env.connect(link, clientOptions{allowInsecure: true}))
}

// (в) vless + REALITY + xtls-rprx-vision; цель рукопожатия — локальный TLS-сервер.
func TestE2EVlessRealityVision(t *testing.T) {
	env := newE2EEnv(t)
	handshake := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	handshake.StartTLS()
	t.Cleanup(handshake.Close)
	private, public := realityKeys(t)
	const shortID = "0123456789abcdef"
	port := env.serve(map[string]any{
		"type":  "vless",
		"users": vlessUser("xtls-rprx-vision"),
		"tls": map[string]any{
			"enabled":     true,
			"server_name": "localhost",
			"reality": map[string]any{
				"enabled": true,
				"handshake": map[string]any{
					"server":      "127.0.0.1",
					"server_port": listenerPort(t, handshake),
				},
				"private_key": private,
				"short_id":    []any{shortID},
			},
		},
	}, "tcp")
	link := fmt.Sprintf("vless://%s@localhost:%d?type=tcp&security=reality&pbk=%s&sid=%s&sni=localhost"+
		"&fp=chrome&flow=xtls-rprx-vision&encryption=none#e2e", e2eUUID, port, public, shortID)
	env.mustFetch(env.connect(link, clientOptions{}))
}

// (г) trojan + TLS.
func TestE2ETrojanTLS(t *testing.T) {
	env := newE2EEnv(t)
	port := env.serve(map[string]any{
		"type":  "trojan",
		"users": []any{map[string]any{"name": "u", "password": "e2e-password"}},
		"tls":   serverTLS(t),
	}, "tcp")
	link := fmt.Sprintf("trojan://e2e-password@localhost:%d?security=tls&sni=localhost#e2e", port)
	env.mustFetch(env.connect(link, clientOptions{allowInsecure: true}))
}

// (д) shadowsocks.
func TestE2EShadowsocks(t *testing.T) {
	env := newE2EEnv(t)
	port := env.serve(map[string]any{
		"type": "shadowsocks", "method": "chacha20-ietf-poly1305", "password": "e2e-password",
	}, "tcp")
	userinfo := base64.URLEncoding.EncodeToString([]byte("chacha20-ietf-poly1305:e2e-password"))
	env.mustFetch(env.connect(fmt.Sprintf("ss://%s@localhost:%d#e2e", userinfo, port), clientOptions{}))
}

// (е) hysteria2 (QUIC, UDP).
func TestE2EHysteria2(t *testing.T) {
	env := newE2EEnv(t)
	port := env.serve(map[string]any{
		"type":  "hysteria2",
		"users": []any{map[string]any{"name": "u", "password": "e2e-password"}},
		"tls":   serverTLS(t, "h3"),
	}, "udp")
	link := fmt.Sprintf("hy2://e2e-password@localhost:%d?insecure=1&sni=localhost#e2e", port)
	env.mustFetchRetry(env.connect(link, clientOptions{}))
}

// (ж) tuic (QUIC, UDP).
func TestE2ETUIC(t *testing.T) {
	env := newE2EEnv(t)
	port := env.serve(map[string]any{
		"type":               "tuic",
		"users":              []any{map[string]any{"name": "u", "uuid": e2eUUID, "password": "e2e-password"}},
		"congestion_control": "bbr",
		"tls":                serverTLS(t, "h3"),
	}, "udp")
	link := fmt.Sprintf("tuic://%s:e2e-password@localhost:%d?congestion_control=bbr&udp_relay_mode=native"+
		"&alpn=h3&sni=localhost&allow_insecure=1#e2e", e2eUUID, port)
	env.mustFetchRetry(env.connect(link, clientOptions{}))
}

// (з) vmess и vless на WebSocket без ALPN в ссылке: без TLS и с TLS.
func TestE2EWebSocketWithoutALPN(t *testing.T) {
	cases := []struct {
		name  string
		proto string
		tls   bool
	}{
		{"vmess_ws", "vmess", false},
		{"vless_ws", "vless", false},
		{"vmess_ws_tls", "vmess", true},
		{"vless_ws_tls", "vless", true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			env := newE2EEnv(t)
			inbound := map[string]any{
				"type":      c.proto,
				"transport": map[string]any{"type": "ws", "path": "/ws"},
			}
			if c.proto == "vmess" {
				inbound["users"] = []any{map[string]any{"name": "u", "uuid": e2eUUID}}
			} else {
				inbound["users"] = vlessUser("")
			}
			if c.tls {
				inbound["tls"] = serverTLS(t, "h2", "http/1.1")
			}
			port := env.serve(inbound, "tcp")
			var link string
			if c.proto == "vmess" {
				fields := map[string]any{"add": "localhost", "port": fmt.Sprint(port), "net": "ws", "host": "localhost", "path": "/ws"}
				if c.tls {
					fields["tls"], fields["sni"] = "tls", "localhost"
				}
				link = vmessLink(fields)
			} else {
				link = fmt.Sprintf("vless://%s@localhost:%d?type=ws&path=/ws&host=localhost&encryption=none", e2eUUID, port)
				if c.tls {
					link += "&security=tls&sni=localhost"
				}
				link += "#e2e"
			}
			env.mustFetch(env.connect(link, clientOptions{allowInsecure: c.tls}))
		})
	}
}
