// SPDX-License-Identifier: GPL-3.0-only

//go:build singbox_run

package inst

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/anatolijlaptev1991-ctrl/lavray-android/core/internal/cfgbuild"
	"github.com/anatolijlaptev1991-ctrl/lavray-android/core/internal/model"
	"github.com/anatolijlaptev1991-ctrl/lavray-android/core/internal/testsocks"
)

// socksTestConfig — тестовый конфиг (cfgbuild.Build с ForTest) для SOCKS5-профиля
// на 127.0.0.1:port: ровно тот, что собирает ядро для URL- и полного теста.
func socksTestConfig(t *testing.T, port int) []byte {
	t.Helper()
	res, err := cfgbuild.Build(cfgbuild.BuildRequest{
		Profile: &model.Profile{
			ID: 1, Type: model.TypeSOCKS, Name: "t", Address: "127.0.0.1", Port: port,
			Detail: &model.SocksHTTPDetail{Proto: "socks5"},
		},
		ForTest: true,
	})
	if err != nil {
		t.Fatalf("cfgbuild: %v", err)
	}
	raw, err := cfgbuild.MarshalIndent(res.Config)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

// Запрос через временный инстанс уходит в SOCKS-сервер профиля и приходит к цели.
func TestTestInstanceDialsThroughOutbound(t *testing.T) {
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	defer target.Close()
	socks := testsocks.Start(t)

	ti, err := NewTestInstance(context.Background(), socksTestConfig(t, socks.Port()))
	if err != nil {
		t.Fatalf("NewTestInstance: %v", err)
	}
	defer ti.Close()

	conn, err := ti.DialContext(context.Background(), "tcp", target.Listener.Addr().String())
	if err != nil {
		t.Fatalf("DialContext: %v", err)
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(5 * time.Second))
	if _, err := fmt.Fprintf(conn, "GET / HTTP/1.1\r\nHost: x\r\nConnection: close\r\n\r\n"); err != nil {
		t.Fatal(err)
	}
	line, err := bufio.NewReader(conn).ReadString('\n')
	if err != nil || !strings.Contains(line, "204") {
		t.Fatalf("ответ цели: %q err=%v", line, err)
	}
	dests := socks.TCPDestinations()
	if len(dests) != 1 || dests[0] != target.Listener.Addr().String() {
		t.Fatalf("SOCKS-сервер профиля увидел цели %v, ожидалась %s", dests, target.Listener.Addr())
	}
}

// Регрессия T-84: у тестового инстанса нет входящих, поэтому он не открывает
// ни одного слушающего сокета (TCP LISTENING и UDP) ни при старте, ни при обмене.
func TestTestInstanceDoesNotListen(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("список сокетов процесса читается через netstat -ano (Windows)")
	}
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {}))
	defer target.Close()
	socks := testsocks.Start(t)
	cfg := socksTestConfig(t, socks.Port())

	before := ownSockets(t)
	// Проверка самого измерения: свой SOCKS-сервер теста должен быть виден.
	if want := "tcp 127.0.0.1:" + strconv.Itoa(socks.Port()); !contains(before, want) {
		t.Fatalf("netstat не показал слушающий сокет %s: %v", want, before)
	}
	ti, err := NewTestInstance(context.Background(), cfg)
	if err != nil {
		t.Fatalf("NewTestInstance: %v", err)
	}
	defer ti.Close()
	conn, err := ti.DialContext(context.Background(), "tcp", target.Listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	_ = conn.Close()
	after := ownSockets(t)

	if extra := difference(after, before); len(extra) != 0 {
		t.Fatalf("тестовый инстанс открыл сокеты: %v", extra)
	}
}

// ownSockets — слушающие TCP и привязанные UDP сокеты текущего процесса.
func ownSockets(t *testing.T) []string {
	t.Helper()
	out, err := exec.Command("netstat", "-ano").Output()
	if err != nil {
		t.Fatalf("netstat: %v", err)
	}
	pid := strconv.Itoa(os.Getpid())
	var socks []string
	for _, line := range strings.Split(string(out), "\n") {
		f := strings.Fields(line)
		if len(f) < 4 || f[len(f)-1] != pid {
			continue
		}
		switch {
		case f[0] == "TCP" && len(f) >= 5 && f[3] == "LISTENING":
			socks = append(socks, "tcp "+f[1])
		case f[0] == "UDP":
			socks = append(socks, "udp "+f[1])
		}
	}
	sort.Strings(socks)
	return socks
}

func contains(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}

// difference — элементы a, которых нет в b.
func difference(a, b []string) []string {
	have := map[string]bool{}
	for _, s := range b {
		have[s] = true
	}
	var out []string
	for _, s := range a {
		if !have[s] {
			out = append(out, s)
		}
	}
	return out
}

// Конфиг с входящими не принимается: тестовый инстанс слушать не должен.
func TestTestInstanceRejectsInbounds(t *testing.T) {
	port := freeTCPPort(t)
	cfg := fmt.Sprintf(`{
  "inbounds": [{"type": "mixed", "tag": "mixed-in", "listen": "127.0.0.1", "listen_port": %d}],
  "outbounds": [{"type": "direct", "tag": "proxy"}]
}`, port)
	if ti, err := NewTestInstance(context.Background(), []byte(cfg)); err == nil {
		_ = ti.Close()
		t.Fatal("конфиг с входящими принят")
	}
	// Порт остался свободным: до listen дело не дошло.
	ln, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", port))
	if err != nil {
		t.Fatalf("порт занят после отказа: %v", err)
	}
	_ = ln.Close()
}

// experimental (cache_file, clash_api) вырезается: кэш не создаёт файл, API не
// занимает порт, а исходящее по умолчанию — «proxy», а не первое в списке.
func TestTestInstanceDropsExperimentalAndPinsProxy(t *testing.T) {
	dir := t.TempDir()
	cachePath := filepath.Join(dir, "cache.db")
	apiPort := freeTCPPort(t)
	socks := testsocks.Start(t)
	var cfg map[string]any
	if err := json.Unmarshal(socksTestConfig(t, socks.Port()), &cfg); err != nil {
		t.Fatal(err)
	}
	cfg["experimental"] = map[string]any{
		"cache_file": map[string]any{"enabled": true, "path": filepath.ToSlash(cachePath)},
		"clash_api":  map[string]any{"external_controller": fmt.Sprintf("127.0.0.1:%d", apiPort)},
	}
	// «direct» первым: без route.final тест пошёл бы мимо профиля.
	outs := cfg["outbounds"].([]any)
	cfg["outbounds"] = append([]any{map[string]any{"type": "direct", "tag": "first-direct"}}, outs...)
	raw, err := json.Marshal(cfg)
	if err != nil {
		t.Fatal(err)
	}

	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {}))
	defer target.Close()
	ti, err := NewTestInstance(context.Background(), raw)
	if err != nil {
		t.Fatalf("NewTestInstance: %v", err)
	}
	conn, err := ti.DialContext(context.Background(), "tcp", target.Listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	_ = conn.Close()
	if err := ti.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	if _, err := os.Stat(cachePath); err == nil {
		t.Fatal("cache_file создан")
	}
	ln, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", apiPort))
	if err != nil {
		t.Fatalf("clash_api занял порт: %v", err)
	}
	_ = ln.Close()
	if d := socks.TCPDestinations(); len(d) != 1 {
		t.Fatalf("трафик не пошёл через исходящее профиля: %v", d)
	}
}

func TestTestInstanceCloseIsIdempotent(t *testing.T) {
	socks := testsocks.Start(t)
	ti, err := NewTestInstance(context.Background(), socksTestConfig(t, socks.Port()))
	if err != nil {
		t.Fatal(err)
	}
	first := ti.Close()
	if second := ti.Close(); second != first {
		t.Fatalf("повторный Close: %v, первый: %v", second, first)
	}
	if _, err := ti.DialContext(context.Background(), "tcp", "127.0.0.1:1"); err == nil {
		t.Fatal("DialContext после Close не вернул ошибку")
	}
}

// Отменённый контекст не поднимает инстанс.
func TestTestInstanceCancelledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if ti, err := NewTestInstance(ctx, []byte(`{}`)); err == nil {
		_ = ti.Close()
		t.Fatal("инстанс поднят с отменённым контекстом")
	}
}

// UDP через ListenPacket: цель сообщается серверу профиля при ASSOCIATE.
func TestTestInstanceListenPacketReachesTarget(t *testing.T) {
	echo, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer echo.Close()
	go func() {
		buf := make([]byte, 1500)
		for {
			n, from, err := echo.ReadFrom(buf)
			if err != nil {
				return
			}
			_, _ = echo.WriteTo(buf[:n], from)
		}
	}()
	socks := testsocks.Start(t)
	ti, err := NewTestInstance(context.Background(), socksTestConfig(t, socks.Port()))
	if err != nil {
		t.Fatal(err)
	}
	defer ti.Close()

	dest := echo.LocalAddr().String()
	pc, err := ti.ListenPacket(context.Background(), dest)
	if err != nil {
		t.Fatalf("ListenPacket: %v", err)
	}
	defer pc.Close()
	_ = pc.SetDeadline(time.Now().Add(5 * time.Second))
	udp, _ := net.ResolveUDPAddr("udp", dest)
	if _, err := pc.WriteTo([]byte("ping"), udp); err != nil {
		t.Fatal(err)
	}
	buf := make([]byte, 64)
	n, _, err := pc.ReadFrom(buf)
	if err != nil || string(buf[:n]) != "ping" {
		t.Fatalf("эхо: %q err=%v", buf[:n], err)
	}
	if d := socks.UDPDestinations(); len(d) == 0 || d[0] != dest {
		t.Fatalf("SOCKS-сервер увидел UDP-цели %v, ожидалась %s", d, dest)
	}
}
