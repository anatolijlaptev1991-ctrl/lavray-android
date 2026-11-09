// SPDX-License-Identifier: GPL-3.0-only

package mobile

import (
	"context"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/anatolijlaptev1991-ctrl/lavray/core/internal/api"
	"github.com/anatolijlaptev1991-ctrl/lavray/core/internal/store"
	"github.com/sagernet/sing-box/adapter"
	"github.com/sagernet/sing/service"
)

func TestInstallTestFactory(t *testing.T) {
	st, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	srv := api.NewServer(api.Deps{Store: st})
	h := newHost(fakePlatform{})
	if err := installTestFactory(srv, h.testFactory()); err != nil {
		t.Fatalf("поле newTestInst не найдено: %v", err)
	}
}

// Каждый экземпляр sing-box получает СВОЙ реестр служб, но платформа и
// реестр DNS переживают context.WithoutCancel (так их берёт inst).
func TestBoxContextIsolation(t *testing.T) {
	h := newHost(fakePlatform{})
	parent := context.Background()
	a, b := h.boxContext(parent), h.boxContext(parent)
	if service.RegistryFromContext(a) == service.RegistryFromContext(b) {
		t.Fatal("реестр служб общий для двух инстансов")
	}
	for _, ctx := range []context.Context{a, context.WithoutCancel(b)} {
		if service.FromContext[adapter.PlatformInterface](ctx) == nil {
			t.Error("платформа потеряна в контексте")
		}
		if service.FromContext[adapter.DNSTransportRegistry](ctx) == nil {
			t.Error("реестр DNS потерян в контексте")
		}
	}
	if service.FromContext[adapter.PlatformInterface](a) == service.FromContext[adapter.PlatformInterface](b) {
		t.Error("состояние платформы (NetworkManager, TUN) должно быть своим у каждого инстанса")
	}
}

func unixClient(sock string) *http.Client {
	return &http.Client{
		Timeout: 10 * time.Second,
		Transport: &http.Transport{DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			return (&net.Dialer{}).DialContext(ctx, "unix", sock)
		}},
	}
}

func TestStartStopLifecycle(t *testing.T) {
	root := t.TempDir()
	cwd, _ := os.Getwd()
	t.Cleanup(func() { _ = os.Chdir(cwd) }) // раньше удаления root (LIFO)
	o := &Options{
		DataDir: filepath.Join(root, "d"), TempDir: filepath.Join(root, "t"),
		SocketPath: filepath.Join(root, "s.sock"), AuthToken: "token-123", MemoryLimitMB: 256,
	}
	if err := Start(o, fakePlatform{}); err != nil {
		t.Fatalf("Start: %v", err)
	}
	t.Cleanup(Stop)
	if err := Start(o, fakePlatform{}); err == nil {
		t.Error("повторный Start должен вернуть ошибку")
	}
	c := unixClient(o.SocketPath)
	rpc := func(auth string) (int, string) {
		req, _ := http.NewRequest("POST", "http://core/lavray.v1.CoreService/GetStatus", strings.NewReader("{}"))
		req.Header.Set("Content-Type", "application/json")
		if auth != "" {
			req.Header.Set("Authorization", auth)
		}
		resp, err := c.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		body, _ := io.ReadAll(resp.Body)
		return resp.StatusCode, string(body)
	}
	if code, _ := rpc(""); code != http.StatusUnauthorized {
		t.Errorf("без токена код %d", code)
	}
	if code, body := rpc("Bearer token-123"); code != http.StatusOK || !strings.Contains(body, "coreVersion") {
		t.Errorf("GetStatus: %d %s", code, body)
	}
	if fi, err := os.Stat(o.SocketPath); err == nil && fi.Mode().Perm()&0o077 != 0 && !isWindows() {
		t.Errorf("права сокета %v", fi.Mode())
	}
	Stop()
	Stop() // идемпотентность
	if _, err := os.Stat(o.SocketPath); err == nil {
		t.Error("сокет не удалён после Stop")
	}
	if err := Start(o, fakePlatform{}); err != nil {
		t.Fatalf("перезапуск после Stop: %v", err)
	}
	Stop()
}

func isWindows() bool { return os.PathSeparator == '\\' }
