// SPDX-License-Identifier: GPL-3.0-only

//go:build singbox_run

package api

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"connectrpc.com/connect"
	lavrayv1 "github.com/anatolijlaptev1991-ctrl/lavray-android/core/internal/gen/lavrayv1"
	"github.com/anatolijlaptev1991-ctrl/lavray-android/core/internal/inst"
	"github.com/anatolijlaptev1991-ctrl/lavray-android/core/internal/presets"
	"github.com/anatolijlaptev1991-ctrl/lavray-android/core/internal/store"
)

// T-33 (B2-12, D1-04): строки sing-box попадают в журнал ядра. Реальный
// sing-box в процессе теста, без TUN и системных настроек: mixed-вход на
// свободном порту loopback.

func sbLogServer(t *testing.T) (*Server, int) {
	t.Helper()
	dir, err := os.MkdirTemp("", "lavray-sblog-*")
	if err != nil {
		t.Fatal(err)
	}
	// Пресет по умолчанию ссылается на встроенные .srs: ядро раскладывает их
	// при старте (main.go), тесту нужно то же.
	if err := presets.EnsureGeo(dir); err != nil {
		t.Fatal(err)
	}
	st, err := store.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = st.Close()
		for i := 0; i < 50; i++ {
			if os.RemoveAll(dir) == nil {
				return
			}
			time.Sleep(10 * time.Millisecond)
		}
	})
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := ln.Addr().(*net.TCPAddr).Port
	_ = ln.Close()
	// Порт 2080 занят рабочим NekoBox: слушатель на свободном.
	if err := st.SetSetting("inbound.port", strconv.Itoa(port)); err != nil {
		t.Fatal(err)
	}
	srv := NewServer(Deps{Store: st, Runner: inst.NewSingBoxRunner()})
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_, _ = srv.Disconnect(ctx, connect.NewRequest(&lavrayv1.DisconnectRequest{}))
	})
	return srv, port
}

func sbTail(t *testing.T, srv *Server) []*lavrayv1.LogLine {
	t.Helper()
	res, err := srv.GetTail(context.Background(), connect.NewRequest(&lavrayv1.GetTailRequest{Lines: 2000}))
	if err != nil {
		t.Fatal(err)
	}
	return res.Msg.Lines
}

func TestSingBoxOutputReachesGetTail(t *testing.T) {
	srv, _ := sbLogServer(t)
	_, err := srv.Connect(context.Background(), connect.NewRequest(&lavrayv1.ConnectRequest{
		InlineProfile: &lavrayv1.Profile{
			Type: lavrayv1.ProfileType_PROFILE_TYPE_SOCKS, Name: "inline", Address: "127.0.0.1", Port: 9,
			Detail: &lavrayv1.Profile_SocksHttp{SocksHttp: &lavrayv1.SocksHttpDetail{Proto: lavrayv1.SocksHttpDetail_PROTO_SOCKS5}},
		},
	}))
	if err != nil {
		t.Fatalf("Connect: %v", err)
	}

	var found *lavrayv1.LogLine
	for deadline := time.Now().Add(5 * time.Second); time.Now().Before(deadline) && found == nil; time.Sleep(20 * time.Millisecond) {
		for _, l := range sbTail(t, srv) {
			if l.Source == "singbox" && strings.Contains(l.Message, "sing-box started") {
				found = l
			}
		}
	}
	if found == nil {
		t.Fatalf("в GetTail нет строки sing-box «sing-box started»; хвост: %v", sbTail(t, srv))
	}
	if found.Level != lavrayv1.LogLevel_LOG_LEVEL_INFO {
		t.Errorf("уровень = %v, ожидался INFO", found.Level)
	}
	for _, l := range sbTail(t, srv) {
		if l.Source != "singbox" {
			continue
		}
		t.Logf("%v singbox: %s", l.Level, l.Message)
		if strings.Contains(l.Message, "\x1b") || strings.HasPrefix(l.Message, "INFO") {
			t.Errorf("строка sing-box с ANSI или меткой уровня: %q", l.Message)
		}
	}
}

// Секреты профиля не попадают в журнал даже на уровне trace: пароль trojan не
// печатает ни sing-box, ни ядро (Mux.Logf редактирует до буфера).
func TestSingBoxLogDoesNotLeakProfileSecret(t *testing.T) {
	const secret = "TOPSECRET-PW-123456789"
	srv, port := sbLogServer(t)
	_, err := srv.Connect(context.Background(), connect.NewRequest(&lavrayv1.ConnectRequest{
		InlineProfile: &lavrayv1.Profile{
			Type: lavrayv1.ProfileType_PROFILE_TYPE_TROJAN, Name: "inline", Address: "127.0.0.1", Port: 9,
			Detail:                &lavrayv1.Profile_TrojanVless{TrojanVless: &lavrayv1.TrojanVlessDetail{Password: secret}},
			CustomConfigJson:      strp(`{"log":{"level":"trace"}}`),
			CustomFieldsConfirmed: true,
		},
	}))
	if err != nil {
		t.Fatalf("Connect: %v", err)
	}
	// Запрос через mixed-вход: outbound пытается дозвониться до закрытого порта.
	proxy, _ := url.Parse(fmt.Sprintf("http://127.0.0.1:%d", port))
	client := &http.Client{Transport: &http.Transport{Proxy: http.ProxyURL(proxy)}, Timeout: 5 * time.Second}
	if resp, err := client.Get("http://example.invalid/"); err == nil {
		_ = resp.Body.Close()
	}
	time.Sleep(300 * time.Millisecond)

	sbLines := 0
	for _, l := range sbTail(t, srv) {
		if l.Source == "singbox" {
			sbLines++
		}
		if strings.Contains(l.Message, secret) {
			t.Fatalf("пароль профиля в журнале: %q", l.Message)
		}
	}
	if sbLines < 3 {
		t.Fatalf("на уровне trace ожидалось несколько строк sing-box, получено %d", sbLines)
	}
}
