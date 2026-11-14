// SPDX-License-Identifier: GPL-3.0-only

package api

// websession_kit_test.go — стенд для тестов веб-режима: сервер ядра за шлюзом
// web.Gate на httptest и «браузер» с cookie-банкой, который сам подставляет
// Origin и CSRF-заголовок.

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/anatolijlaptev1991-ctrl/lavray-android/core/internal/inst"
	"github.com/anatolijlaptev1991-ctrl/lavray-android/core/internal/web"
)

const (
	testCoreToken = "core-token-0123456789abcdef"
	goodWebPass   = "correct-horse-1"
	procPrefix    = "/lavray.v1."
)

// webClock — управляемое время сессий и лимитера.
type webClock struct {
	mu sync.Mutex
	t  time.Time
}

func (c *webClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.t
}

func (c *webClock) Advance(d time.Duration) {
	c.mu.Lock()
	c.t = c.t.Add(d)
	c.mu.Unlock()
}

// webFixture — сервер ядра с включённым веб-режимом за шлюзом.
type webFixture struct {
	t    *testing.T
	srv  *Server
	auth *web.Auth
	ts   *httptest.Server
	clk  *webClock
}

// newWebFixture поднимает шлюз; tlsMode — HTTPS (проверка Secure у cookie).
func newWebFixture(t *testing.T, tlsMode bool) *webFixture {
	t.Helper()
	return newWebFixtureCode(t, tlsMode, nil)
}

// newWebFixtureCode — то же с кодом первичной настройки (панель не на loopback).
func newWebFixtureCode(t *testing.T, tlsMode bool, code *web.SetupCode) *webFixture {
	t.Helper()
	f := &webFixture{t: t, clk: &webClock{t: time.Date(2026, 10, 31, 12, 0, 0, 0, time.UTC)}}
	newE2EServer(t, Deps{Runner: &inst.FakeRunner{}}, func(s *Server) {
		f.srv = s
		f.auth = s.EnableWeb(WebOptions{
			BindAddress: "127.0.0.1:0",
			TLSEnabled:  tlsMode,
			SetupCode:   code,
			Sessions:    web.NewSessions(f.clk.Now),
			Limiter:     web.NewLimiter(f.clk.Now, web.LimiterConfig{}),
		})
	})
	mux := http.NewServeMux()
	f.srv.Register(mux)
	gate := web.NewHandler(&web.Gate{Sessions: f.auth.Sessions, Bearer: testCoreToken, API: mux})
	if tlsMode {
		f.ts = httptest.NewTLSServer(gate)
	} else {
		f.ts = httptest.NewServer(gate)
	}
	t.Cleanup(f.ts.Close)
	return f
}

// rpcResult — разобранный ответ Connect (unary, JSON).
type rpcResult struct {
	Status int
	Header http.Header
	Body   map[string]any
	Code   string // код ошибки Connect; "" — успех
}

func (r rpcResult) str(key string) string {
	s, _ := r.Body[key].(string)
	return s
}

// browser — клиент с cookie-банкой; Origin и CSRF подставляет сам.
type browser struct {
	f    *webFixture
	hc   *http.Client
	csrf string
}

func (f *webFixture) newBrowser() *browser {
	jar, _ := cookiejar.New(nil)
	hc := *f.ts.Client() // копия: у httptest общий клиент, а банка у каждого «браузера» своя
	hc.Jar = jar
	return &browser{f: f, hc: &hc}
}

// call вызывает процедуру сервиса svc (например "WebSessionService/Login").
func (b *browser) call(proc, body string, mutate ...func(*http.Request)) rpcResult {
	b.f.t.Helper()
	return b.raw(procPrefix+proc, body, true, mutate...)
}

func (b *browser) raw(path, body string, browserHeaders bool, mutate ...func(*http.Request)) rpcResult {
	b.f.t.Helper()
	req, err := http.NewRequest(http.MethodPost, b.f.ts.URL+path, strings.NewReader(body))
	if err != nil {
		b.f.t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Connect-Protocol-Version", "1")
	if browserHeaders {
		req.Header.Set("Origin", b.f.ts.URL)
		if b.csrf != "" {
			req.Header.Set(web.CSRFHeader, b.csrf)
		}
	}
	for _, m := range mutate {
		m(req)
	}
	resp, err := b.hc.Do(req)
	if err != nil {
		b.f.t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	data, _ := io.ReadAll(resp.Body)
	res := rpcResult{Status: resp.StatusCode, Header: resp.Header, Body: map[string]any{}}
	if err := json.NewDecoder(bytes.NewReader(data)).Decode(&res.Body); err != nil && len(data) > 0 {
		b.f.t.Fatalf("%s: не JSON: %q", path, data)
	}
	if resp.StatusCode != http.StatusOK {
		res.Code, _ = res.Body["code"].(string)
	}
	return res
}

// login входит и запоминает CSRF-токен.
func (b *browser) login(user, pass string) rpcResult {
	b.f.t.Helper()
	res := b.call("WebSessionService/Login", `{"username":"`+user+`","password":"`+pass+`"}`)
	if res.Code == "" {
		b.csrf = res.str("csrfToken")
	}
	return res
}

// setup создаёт первого администратора.
func (b *browser) setup(user, pass string) rpcResult { return b.setupWithCode(user, pass, "") }

// setupWithCode — то же с кодом первичной настройки.
func (b *browser) setupWithCode(user, pass, code string) rpcResult {
	b.f.t.Helper()
	body := `{"username":"` + user + `","password":"` + pass + `"`
	if code != "" {
		body += `,"setupCode":"` + code + `"`
	}
	res := b.call("WebSessionService/SetupFirstAdmin", body+`}`)
	if res.Code == "" {
		b.csrf = res.str("csrfToken")
	}
	return res
}

// mustJSON — JSON тела для поиска подстрок.
func mustJSON(v any) []byte {
	out, _ := json.Marshal(v)
	return out
}
