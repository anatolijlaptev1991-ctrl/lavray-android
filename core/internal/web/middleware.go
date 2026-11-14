// SPDX-License-Identifier: GPL-3.0-only

package web

// middleware.go — шлюз веб-слушателя. До авторизации открыты: /healthz,
// статика интерфейса и три метода входа. Остальной RPC принимается так:
//
//   - Authorization: Bearer <токен ядра> (headless: скрипты, curl) — без cookie,
//     CSRF и Origin;
//   - либо cookie сессии + для изменяющих запросов заголовок X-Lavray-CSRF +
//     совпадающий Origin (WhoAmI от CSRF освобождён: после перезагрузки страницы
//     именно он выдаёт CSRF-токен).
//
// Если Authorization прислан, но неверен, cookie не рассматривается.

import (
	"crypto/subtle"
	"net"
	"net/http"
	"strings"
)

// Процедуры, доступные до входа, и процедура без требования CSRF. Пути проверяются
// в api тестом на соответствие сгенерированным константам.
const (
	rpcPrefix            = "/lavray.v1."
	ProcGetWebModeStatus = "/lavray.v1.WebSessionService/GetWebModeStatus"
	ProcSetupFirstAdmin  = "/lavray.v1.WebSessionService/SetupFirstAdmin"
	ProcLogin            = "/lavray.v1.WebSessionService/Login"
	ProcWhoAmI           = "/lavray.v1.WebSessionService/WhoAmI"
)

// publicBodyLimit — потолок тела запроса до входа: вход, настройка и статус
// укладываются в килобайты, а общий лимит RPC (16 МиБ) открывал бы память роутера
// любому клиенту LAN.
const publicBodyLimit = 8 << 10

var publicProcedures = map[string]bool{ProcGetWebModeStatus: true, ProcSetupFirstAdmin: true, ProcLogin: true}

// Gate — шлюз: API — мультиплексор RPC ядра, UI — раздача интерфейса
// (nil — WebUIHandler), Bearer — токен ядра для headless ("" — Bearer выключен).
type Gate struct {
	Sessions *Sessions
	Bearer   string
	API      http.Handler
	UI       http.Handler
}

// NewHandler оборачивает шлюз защитными заголовками.
func NewHandler(g *Gate) http.Handler {
	if g.UI == nil {
		g.UI = WebUIHandler()
	}
	return SecurityHeaders(g)
}

func (g *Gate) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	switch {
	case r.URL.Path == "/healthz":
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		_, _ = w.Write([]byte("ok"))
	case strings.HasPrefix(r.URL.Path, rpcPrefix):
		g.serveRPC(w, r)
	default:
		g.UI.ServeHTTP(w, r)
	}
}

func (g *Gate) serveRPC(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		rejectRPC(w, r, http.StatusMethodNotAllowed, "unimplemented", "RPC принимает только POST")
		return
	}
	info := RequestInfo{IP: clientIP(r), Secure: r.TLS != nil}
	switch {
	case publicProcedures[r.URL.Path]:
		if CheckOrigin(r, false) != nil {
			rejectRPC(w, r, http.StatusForbidden, "permission_denied", "Origin не совпадает с Host")
			return
		}
		r.Body = http.MaxBytesReader(w, r.Body, publicBodyLimit)
	case r.Header.Get("Authorization") != "":
		if !g.bearerOK(r) {
			rejectRPC(w, r, http.StatusUnauthorized, "unauthenticated", "неверный токен")
			return
		}
		info.Bearer = true
	default:
		if !g.cookieAuth(w, r, &info) {
			return
		}
	}
	g.API.ServeHTTP(w, r.WithContext(WithInfo(r.Context(), info)))
}

// cookieAuth проверяет сессию, Origin и CSRF; при отказе отвечает сам.
func (g *Gate) cookieAuth(w http.ResponseWriter, r *http.Request, info *RequestInfo) bool {
	token := CookieToken(r)
	sess, ok := g.Sessions.Validate(token)
	if !ok {
		rejectRPC(w, r, http.StatusUnauthorized, "unauthenticated", "требуется вход")
		return false
	}
	if CheckOrigin(r, true) != nil {
		rejectRPC(w, r, http.StatusForbidden, "permission_denied", "Origin не совпадает с Host")
		return false
	}
	if r.URL.Path != ProcWhoAmI && !CSRFEqual(r.Header.Get(CSRFHeader), sess.CSRF) {
		rejectRPC(w, r, http.StatusForbidden, "permission_denied", "неверный CSRF-токен")
		return false
	}
	info.Session, info.Token = &sess, token
	return true
}

// bearerOK сверяет Bearer-токен с токеном ядра за постоянное время. Схема
// «Bearer» обязательна: голый токен в заголовке не принимается.
func (g *Gate) bearerOK(r *http.Request) bool {
	scheme, rest, found := strings.Cut(r.Header.Get("Authorization"), " ")
	if !found || !strings.EqualFold(scheme, "Bearer") || g.Bearer == "" {
		return false
	}
	got := strings.TrimLeft(rest, " ")
	return got != "" && subtle.ConstantTimeCompare([]byte(got), []byte(g.Bearer)) == 1
}

// clientIP — адрес клиента без порта. Заголовкам X-Forwarded-For не верим:
// ядро слушает LAN напрямую, подделать их может любой клиент.
func clientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

// writeRPCError отвечает ошибкой в формате Connect (JSON с кодом).
func writeRPCError(w http.ResponseWriter, status int, code, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_, _ = w.Write([]byte(`{"code":"` + code + `","message":"` + msg + `"}`))
}
