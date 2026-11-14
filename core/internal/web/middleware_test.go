// SPDX-License-Identifier: GPL-3.0-only

package web

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

const (
	procLogin  = "/lavray.v1.WebSessionService/Login"
	procWhoAmI = "/lavray.v1.WebSessionService/WhoAmI"
	procStatus = "/lavray.v1.CoreService/GetStatus"
)

// gateFixture — шлюз с заглушкой API, которая запоминает контекст запроса.
type gateFixture struct {
	h       http.Handler
	sess    *Sessions
	last    RequestInfo
	hasInfo bool
	token   string
	csrf    string
}

func newGate(t *testing.T) *gateFixture {
	t.Helper()
	f := &gateFixture{sess: NewSessions(newFakeClock().Now)}
	api := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.last, f.hasInfo = InfoFrom(r.Context())
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{}`))
	})
	f.h = NewHandler(&Gate{Sessions: f.sess, Bearer: "core-token-0123456789", API: api})
	tok, s, err := f.sess.Create(admin, "test", "10.0.0.1")
	if err != nil {
		t.Fatal(err)
	}
	f.token, f.csrf = tok, s.CSRF
	return f
}

// rpc строит POST к процедуре; origin и csrf опциональны.
func (f *gateFixture) rpc(proc string, mutate func(*http.Request)) *httptest.ResponseRecorder {
	r := httptest.NewRequest(http.MethodPost, "http://router.lan:8443"+proc, strings.NewReader(`{}`))
	r.Host = "router.lan:8443"
	r.RemoteAddr = "192.168.1.77:40000"
	r.Header.Set("Content-Type", "application/json")
	if mutate != nil {
		mutate(r)
	}
	rec := httptest.NewRecorder()
	f.h.ServeHTTP(rec, r)
	return rec
}

func (f *gateFixture) cookie(r *http.Request) {
	r.AddCookie(&http.Cookie{Name: CookieName, Value: f.token})
}

// TestRPCNeedsSessionAndCSRF — RPC без сессии 401, с сессией без CSRF 403 (S).
func TestRPCNeedsSessionAndCSRF(t *testing.T) {
	f := newGate(t)
	good := func(r *http.Request) {
		f.cookie(r)
		r.Header.Set(CSRFHeader, f.csrf)
		r.Header.Set("Origin", "https://router.lan:8443")
	}
	cases := []struct {
		name string
		mut  func(*http.Request)
		code int
	}{
		{"без cookie", nil, http.StatusUnauthorized},
		{"cookie с чужим токеном", func(r *http.Request) { r.AddCookie(&http.Cookie{Name: CookieName, Value: "nope"}) }, http.StatusUnauthorized},
		{"cookie без CSRF", func(r *http.Request) { f.cookie(r); r.Header.Set("Origin", "https://router.lan:8443") }, http.StatusForbidden},
		{"неверный CSRF", func(r *http.Request) { good(r); r.Header.Set(CSRFHeader, f.csrf+"x") }, http.StatusForbidden},
		{"пустой CSRF", func(r *http.Request) { good(r); r.Header.Set(CSRFHeader, "") }, http.StatusForbidden},
		{"без Origin", func(r *http.Request) { f.cookie(r); r.Header.Set(CSRFHeader, f.csrf) }, http.StatusForbidden},
		{"всё верно", good, http.StatusOK},
	}
	for _, c := range cases {
		if rec := f.rpc(procStatus, c.mut); rec.Code != c.code {
			t.Errorf("%s: код %d, ждали %d (%s)", c.name, rec.Code, c.code, rec.Body.String())
		}
	}
	f.rpc(procStatus, good)
	if !f.hasInfo || f.last.Session == nil || f.last.Session.Username != "admin" || f.last.Bearer || f.last.IP != "192.168.1.77" {
		t.Fatalf("API не получил сведения о сессии: %+v", f.last)
	}
}

// TestOriginMismatchRejectedByGate — правильные cookie и CSRF с чужим Origin не проходят (S).
func TestOriginMismatchRejectedByGate(t *testing.T) {
	f := newGate(t)
	rec := f.rpc(procStatus, func(r *http.Request) {
		f.cookie(r)
		r.Header.Set(CSRFHeader, f.csrf)
		r.Header.Set("Origin", "https://evil.example")
	})
	if rec.Code != http.StatusForbidden || !strings.Contains(rec.Body.String(), `"permission_denied"`) {
		t.Fatalf("чужой Origin: %d %s", rec.Code, rec.Body.String())
	}
	// Чужой Origin на открытом Login тоже отклоняется (login CSRF).
	rec = f.rpc(procLogin, func(r *http.Request) { r.Header.Set("Origin", "https://evil.example") })
	if rec.Code != http.StatusForbidden {
		t.Fatalf("Login с чужим Origin: %d", rec.Code)
	}
}

func TestPublicProceduresNeedNoSession(t *testing.T) {
	f := newGate(t)
	for _, p := range []string{procLogin, "/lavray.v1.WebSessionService/SetupFirstAdmin", "/lavray.v1.WebSessionService/GetWebModeStatus"} {
		rec := f.rpc(p, nil) // без Origin: curl и скрипты
		if rec.Code != http.StatusOK {
			t.Errorf("%s: %d", p, rec.Code)
		}
		if !f.hasInfo || f.last.Session != nil || f.last.IP != "192.168.1.77" {
			t.Errorf("%s: info=%+v", p, f.last)
		}
	}
}

// TestWhoAmIWorksWithoutCSRF — после перезагрузки страницы CSRF-токена у клиента
// ещё нет, WhoAmI его выдаёт; Origin по-прежнему обязателен.
func TestWhoAmIWorksWithoutCSRF(t *testing.T) {
	f := newGate(t)
	ok := func(r *http.Request) { f.cookie(r); r.Header.Set("Origin", "https://router.lan:8443") }
	if rec := f.rpc(procWhoAmI, ok); rec.Code != http.StatusOK {
		t.Fatalf("WhoAmI: %d", rec.Code)
	}
	if rec := f.rpc(procWhoAmI, func(r *http.Request) { f.cookie(r) }); rec.Code != http.StatusForbidden {
		t.Fatalf("WhoAmI без Origin: %d", rec.Code)
	}
	if rec := f.rpc(procWhoAmI, nil); rec.Code != http.StatusUnauthorized {
		t.Fatalf("WhoAmI без cookie: %d", rec.Code)
	}
}

// TestBearerStillWorksHeadless — токен ядра работает без cookie, CSRF и Origin;
// неверный Bearer не спасает даже действующая cookie.
func TestBearerStillWorksHeadless(t *testing.T) {
	f := newGate(t)
	bearer := func(tok string) func(*http.Request) {
		return func(r *http.Request) { r.Header.Set("Authorization", "Bearer "+tok) }
	}
	if rec := f.rpc(procStatus, bearer("core-token-0123456789")); rec.Code != http.StatusOK {
		t.Fatalf("Bearer: %d", rec.Code)
	}
	if !f.last.Bearer || f.last.Session != nil {
		t.Fatalf("сведения Bearer: %+v", f.last)
	}
	for name, mut := range map[string]func(*http.Request){
		"неверный токен":      bearer("wrong"),
		"пустой токен":        bearer(""),
		"голый токен":         func(r *http.Request) { r.Header.Set("Authorization", "core-token-0123456789") },
		"верная cookie+мусор": func(r *http.Request) { f.cookie(r); r.Header.Set("Authorization", "Bearer wrong") },
	} {
		if rec := f.rpc(procStatus, mut); rec.Code != http.StatusUnauthorized {
			t.Errorf("%s: %d, ждали 401", name, rec.Code)
		}
	}
	// Ядро без Bearer-токена (пустой) не пускает по пустому заголовку.
	g := NewHandler(&Gate{Sessions: f.sess, Bearer: "", API: http.NotFoundHandler()})
	req := httptest.NewRequest(http.MethodPost, procStatus, nil)
	req.Header.Set("Authorization", "Bearer ")
	rec := httptest.NewRecorder()
	g.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("пустой токен ядра пропустил запрос: %d", rec.Code)
	}
}

func TestRPCRejectsNonPost(t *testing.T) {
	f := newGate(t)
	r := httptest.NewRequest(http.MethodGet, procStatus, nil)
	rec := httptest.NewRecorder()
	f.h.ServeHTTP(rec, r)
	if rec.Code != http.StatusMethodNotAllowed || rec.Header().Get("Allow") != "POST" {
		t.Fatalf("GET на RPC: %d allow=%q", rec.Code, rec.Header().Get("Allow"))
	}
}

// TestSecurityHeaders — защитные заголовки есть на всех ответах слушателя (S).
func TestSecurityHeaders(t *testing.T) {
	f := newGate(t)
	want := map[string]string{
		"X-Frame-Options":        "DENY",
		"X-Content-Type-Options": "nosniff",
		"Referrer-Policy":        "no-referrer",
	}
	check := func(what string, rec *httptest.ResponseRecorder) {
		for k, v := range want {
			if got := rec.Header().Get(k); got != v {
				t.Errorf("%s: %s=%q, ждали %q", what, k, got, v)
			}
		}
		csp := rec.Header().Get("Content-Security-Policy")
		for _, part := range []string{"default-src 'self'", "frame-ancestors 'none'", "object-src 'none'"} {
			if !strings.Contains(csp, part) {
				t.Errorf("%s: в CSP нет %q: %q", what, part, csp)
			}
		}
		if strings.Contains(csp, "unsafe-eval") || strings.Contains(csp, "script-src 'self' 'unsafe-inline'") {
			t.Errorf("%s: CSP разрешает небезопасные скрипты: %q", what, csp)
		}
	}
	check("RPC 401", f.rpc(procStatus, nil))
	rec := httptest.NewRecorder()
	f.h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	check("корень", rec)
	rec = httptest.NewRecorder()
	f.h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/healthz", nil))
	check("healthz", rec)
	if rec.Code != http.StatusOK {
		t.Fatalf("healthz: %d", rec.Code)
	}
	if cc := f.rpc(procStatus, nil).Header().Get("Cache-Control"); cc != "no-store" {
		t.Errorf("RPC без Cache-Control: no-store (%q)", cc)
	}
}

func TestStaticIsPublic(t *testing.T) {
	f := newGate(t)
	rec := httptest.NewRecorder()
	f.h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/login", nil))
	if rec.Code != http.StatusOK || !strings.HasPrefix(rec.Header().Get("Content-Type"), "text/html") {
		t.Fatalf("/login: %d %q", rec.Code, rec.Header().Get("Content-Type"))
	}
}

// TestPublicProceduresLimitBody — до входа тело запроса ограничено килобайтами (S).
func TestPublicProceduresLimitBody(t *testing.T) {
	api := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if _, err := io.ReadAll(r.Body); err != nil {
			http.Error(w, "too large", http.StatusRequestEntityTooLarge)
			return
		}
		w.WriteHeader(http.StatusOK)
	})
	h := NewHandler(&Gate{Sessions: NewSessions(nil), Bearer: "tok", API: api})
	post := func(proc string, size int) int {
		r := httptest.NewRequest(http.MethodPost, proc, strings.NewReader(strings.Repeat("x", size)))
		r.Host = "router.lan"
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, r)
		return rec.Code
	}
	if code := post(procLogin, 1024); code != http.StatusOK {
		t.Fatalf("тело в 1 КиБ: %d", code)
	}
	if code := post(procLogin, 64<<10); code != http.StatusRequestEntityTooLarge {
		t.Fatalf("тело в 64 КиБ: %d, ждали 413", code)
	}
	// Закрытые процедуры лимит публичных не получают: у них свой (16 МиБ в api).
	r := httptest.NewRequest(http.MethodPost, procStatus, strings.NewReader(strings.Repeat("x", 64<<10)))
	r.Header.Set("Authorization", "Bearer tok")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, r)
	if rec.Code != http.StatusOK {
		t.Fatalf("закрытая процедура с телом 64 КиБ: %d", rec.Code)
	}
}
