// SPDX-License-Identifier: GPL-3.0-only

// svc_fixture_test.go — общая оснастка тестов сервисов ядра: Server на
// временном хранилище и FakeRunner, фальшивый «mixed-in» (HTTP-прокси).
package api

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/anatolijlaptev1991-ctrl/lavray-android/core/internal/inst"
	"github.com/anatolijlaptev1991-ctrl/lavray-android/core/internal/logs"
	"github.com/anatolijlaptev1991-ctrl/lavray-android/core/internal/model"
	"github.com/anatolijlaptev1991-ctrl/lavray-android/core/internal/store"
)

type svcFixture struct {
	srv    *Server
	runner *inst.FakeRunner
}

// newSvcFixture — сервер как в cmd/lavray-core/main.go (Store, Logs, Runner),
// но с FakeRunner и своим HTTP-клиентом для «прямого» канала (nil → по умолчанию).
func newSvcFixture(t *testing.T, direct *http.Client) *svcFixture {
	t.Helper()
	// Не t.TempDir(): гонка RemoveAll с -wal/-shm на Windows (см. newE2E).
	dir, err := os.MkdirTemp("", "lavray-svc-*")
	if err != nil {
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
	if err := st.CreateGroup(&model.Group{ID: 1, Name: "Default", Kind: model.GroupBasic}); err != nil {
		t.Fatal(err)
	}
	runner := &inst.FakeRunner{}
	return &svcFixture{
		srv:    NewServer(Deps{Store: st, Logs: logs.NewMux(1000), Runner: runner, HTTP: direct}),
		runner: runner,
	}
}

// trojan — сохранённый профиль trojan example.org:443.
func (f *svcFixture) trojan(t *testing.T) *model.Profile {
	t.Helper()
	p := &model.Profile{GroupID: 1, Type: model.TypeTrojan, Name: "p", Address: "example.org", Port: 443,
		Detail: &model.TrojanVlessDetail{Password: "secret"}}
	if err := f.srv.st.CreateProfile(p); err != nil {
		t.Fatal(err)
	}
	return p
}

// bringUpTunnel поднимает «инстанс» с конфигом, в котором mixed-in слушает port.
func (f *svcFixture) bringUpTunnel(t *testing.T, port int, mode string) {
	t.Helper()
	cfg := fmt.Sprintf(`{"inbounds":[{"tag":"mixed-in","type":"mixed","listen":"127.0.0.1","listen_port":%d}]}`, port)
	if err := f.srv.inst.Start(context.Background(), inst.RunInfo{Mode: mode, ConfigJSON: []byte(cfg)}); err != nil {
		t.Fatal(err)
	}
}

// rtFunc — http.RoundTripper из функции («прямой» канал в тестах).
type rtFunc func(*http.Request) (*http.Response, error)

func (f rtFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

// textRT отвечает 200 и телом body на любой запрос и считает обращения.
func textRT(body string, hits *[]string) rtFunc {
	var mu sync.Mutex
	return func(r *http.Request) (*http.Response, error) {
		mu.Lock()
		*hits = append(*hits, r.Method+" "+r.URL.String())
		mu.Unlock()
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(body)), Header: http.Header{}, Request: r}, nil
	}
}

// fakeProxy — HTTP-прокси, играющий mixed-in: на любой запрос отвечает body и
// запоминает запрошенные адреса.
type fakeProxy struct {
	srv  *httptest.Server
	mu   sync.Mutex
	seen []string
}

func startFakeProxy(t *testing.T, body string) *fakeProxy {
	t.Helper()
	fp := &fakeProxy{}
	fp.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fp.mu.Lock()
		fp.seen = append(fp.seen, r.Method+" "+r.URL.String())
		fp.mu.Unlock()
		_, _ = io.WriteString(w, body)
	}))
	t.Cleanup(fp.srv.Close)
	return fp
}

func (fp *fakeProxy) port() int {
	return fp.srv.Listener.Addr().(*net.TCPAddr).Port
}

func (fp *fakeProxy) requests() []string {
	fp.mu.Lock()
	defer fp.mu.Unlock()
	return append([]string(nil), fp.seen...)
}
