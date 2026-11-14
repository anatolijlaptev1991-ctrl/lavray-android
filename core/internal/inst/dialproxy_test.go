// SPDX-License-Identifier: GPL-3.0-only

package inst

import (
	"context"
	"errors"
	"net"
	"testing"
)

// DialProxy менеджера: без работающего инстанса — ErrNotRunning, раннер без
// возможности — ErrDialUnsupported, иначе вызов уходит раннеру.
func TestManagerDialProxy(t *testing.T) {
	ctx := context.Background()
	r := &FakeRunner{}
	m := NewManager(r)
	if _, err := m.DialProxy(ctx, "tcp", "example.com:443"); !errors.Is(err, ErrNotRunning) {
		t.Fatalf("до старта: %v, ожидалось ErrNotRunning", err)
	}
	if err := m.Start(ctx, RunInfo{ConfigJSON: []byte(`{}`)}); err != nil {
		t.Fatal(err)
	}
	if _, err := m.DialProxy(ctx, "tcp", "example.com:443"); !errors.Is(err, ErrDialUnsupported) {
		t.Fatalf("раннер без ProxyDial: %v, ожидалось ErrDialUnsupported", err)
	}
	var gotNetwork, gotAddr string
	r.ProxyDial = func(_ context.Context, network, address string) (net.Conn, error) {
		gotNetwork, gotAddr = network, address
		c, _ := net.Pipe()
		return c, nil
	}
	conn, err := m.DialProxy(ctx, "tcp", "example.com:443")
	if err != nil {
		t.Fatalf("DialProxy: %v", err)
	}
	_ = conn.Close()
	if gotNetwork != "tcp" || gotAddr != "example.com:443" {
		t.Fatalf("раннер получил %q %q", gotNetwork, gotAddr)
	}
	if err := m.Stop(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := m.DialProxy(ctx, "tcp", "example.com:443"); !errors.Is(err, ErrNotRunning) {
		t.Fatalf("после остановки: %v, ожидалось ErrNotRunning", err)
	}
}

// NopRunner не умеет открывать соединения: проверка связи его пропускает.
func TestNopRunnerIsNotProxyDialer(t *testing.T) {
	if _, ok := any(NopRunner{}).(ProxyDialer); ok {
		t.Fatal("NopRunner не должен быть ProxyDialer")
	}
}
