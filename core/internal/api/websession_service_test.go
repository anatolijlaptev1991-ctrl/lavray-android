// SPDX-License-Identifier: GPL-3.0-only

package api

// websession_service_test.go — WebSessionService целиком через шлюз web.Gate:
// вход, cookie, CSRF, Origin, блокировка, сессии, смена пароля (GE).

import (
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/anatolijlaptev1991-ctrl/lavray-android/core/internal/gen/lavrayv1connect"
	"github.com/anatolijlaptev1991-ctrl/lavray-android/core/internal/web"
)

// TestWebSessionServiceFlow — полный путь: статус → настройка → вход → сессии →
// смена пароля → выход.
func TestWebSessionServiceFlow(t *testing.T) {
	f := newWebFixture(t, false)
	b := f.newBrowser()

	st := b.call("WebSessionService/GetWebModeStatus", `{}`)
	if st.Code != "" || st.Body["webModeEnabled"] != true || st.Body["firstRunSetupRequired"] != true ||
		st.str("bindAddress") != "127.0.0.1:0" || st.Body["sessionTtlMinutes"] != float64(720) ||
		st.Body["sessionAbsoluteTtlMinutes"] != float64(10080) {
		t.Fatalf("статус до настройки: %+v", st)
	}
	if r := b.login("admin", goodWebPass); r.Code != "failed_precondition" {
		t.Fatalf("вход до настройки: %+v", r)
	}
	setup := b.setup("admin", goodWebPass)
	if setup.Code != "" || setup.str("username") != "admin" || b.csrf == "" || setup.str("token") != "" {
		t.Fatalf("настройка: %+v", setup)
	}
	if st := b.call("WebSessionService/GetWebModeStatus", `{}`); st.Body["firstRunSetupRequired"] == true || st.Body["activeSessions"] != nil {
		t.Fatalf("статус после настройки (число сессий до входа не раскрывается): %+v", st.Body)
	}

	// Перезагрузка страницы: CSRF утерян, WhoAmI возвращает его по cookie.
	b.csrf = ""
	who := b.call("WebSessionService/WhoAmI", `{}`)
	if who.Code != "" || who.str("username") != "admin" || who.str("csrfToken") == "" {
		t.Fatalf("WhoAmI: %+v", who)
	}
	b.csrf = who.str("csrfToken")

	other := f.newBrowser()
	if r := other.login("Admin", goodWebPass); r.Code != "" { // имя без учёта регистра
		t.Fatalf("второй вход: %+v", r)
	}
	list := b.call("WebSessionService/ListSessions", `{}`)
	sessions, _ := list.Body["sessions"].([]any)
	if len(sessions) != 2 {
		t.Fatalf("ListSessions: %+v", list.Body)
	}
	var otherID string
	for _, raw := range sessions {
		s := raw.(map[string]any)
		if s["current"] != true {
			otherID, _ = s["sessionId"].(string)
		}
		if s["sessionId"] == "" {
			t.Fatalf("в списке нет id: %+v", s)
		}
	}
	if r := b.call("WebSessionService/RevokeSession", `{"sessionId":"`+otherID+`"}`); r.Code != "" {
		t.Fatalf("RevokeSession: %+v", r)
	}
	if r := b.call("WebSessionService/RevokeSession", `{"sessionId":"`+otherID+`"}`); r.Code != "not_found" {
		t.Fatalf("повторный RevokeSession: %+v", r)
	}
	if r := other.call("WebSessionService/WhoAmI", `{}`); r.Status != http.StatusUnauthorized {
		t.Fatalf("отозванная сессия действует: %+v", r)
	}

	if r := b.call("WebSessionService/RenewSession", `{}`); r.Code != "" || !strings.Contains(strings.Join(r.Header.Values("Set-Cookie"), ";"), "lavray_session=") {
		t.Fatalf("RenewSession: %+v", r)
	}
	if r := b.call("WebSessionService/Logout", `{}`); r.Code != "" {
		t.Fatalf("Logout: %+v", r)
	}
	if r := b.call("WebSessionService/WhoAmI", `{}`); r.Status != http.StatusUnauthorized {
		t.Fatalf("после выхода сессия действует: %+v", r)
	}
}

// TestChangePasswordRevokesOthersE2E — смена пароля гасит остальные сессии,
// старый пароль перестаёт работать; ошибки возвращают понятные коды.
func TestChangePasswordRevokesOthersE2E(t *testing.T) {
	f := newWebFixture(t, false)
	b := f.newBrowser()
	b.setup("admin", goodWebPass)
	o1, o2 := f.newBrowser(), f.newBrowser()
	o1.login("admin", goodWebPass)
	o2.login("admin", goodWebPass)

	if r := b.call("WebSessionService/ChangePassword", `{"oldPassword":"wrong-password-1","newPassword":"brand-new-pass-1"}`); r.Code != "invalid_argument" {
		t.Fatalf("неверный прежний пароль: %+v", r)
	}
	if r := b.call("WebSessionService/ChangePassword", `{"oldPassword":"`+goodWebPass+`","newPassword":"short"}`); r.Code != "invalid_argument" {
		t.Fatalf("слабый новый пароль: %+v", r)
	}
	r := b.call("WebSessionService/ChangePassword", `{"oldPassword":"`+goodWebPass+`","newPassword":"brand-new-pass-1"}`)
	if r.Code != "" || r.Body["revokedOtherSessions"] != float64(2) {
		t.Fatalf("ChangePassword: %+v", r)
	}
	if r := b.call("WebSessionService/WhoAmI", `{}`); r.Code != "" {
		t.Fatalf("текущая сессия отозвана: %+v", r)
	}
	for _, o := range []*browser{o1, o2} {
		if r := o.call("WebSessionService/WhoAmI", `{}`); r.Status != http.StatusUnauthorized {
			t.Fatalf("чужая сессия жива: %+v", r)
		}
	}
	if r := f.newBrowser().login("admin", goodWebPass); r.Code != "unauthenticated" {
		t.Fatalf("старый пароль: %+v", r)
	}
	if r := f.newBrowser().login("admin", "brand-new-pass-1"); r.Code != "" {
		t.Fatalf("новый пароль: %+v", r)
	}
}

// TestFirstAdminOnlyOnceE2E — настройка срабатывает один раз, даже параллельно (D).
func TestFirstAdminOnlyOnceE2E(t *testing.T) {
	f := newWebFixture(t, false)
	var wg sync.WaitGroup
	var mu sync.Mutex
	ok, failed := 0, 0
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			r := f.newBrowser().setup("admin", goodWebPass)
			mu.Lock()
			defer mu.Unlock()
			switch r.Code {
			case "":
				ok++
			case "failed_precondition":
				failed++
			}
		}()
	}
	wg.Wait()
	if ok != 1 || failed != 4 {
		t.Fatalf("настройка: успех=%d отказ=%d, ждали 1 и 4", ok, failed)
	}
	if r := f.newBrowser().setup("second", goodWebPass); r.Code != "failed_precondition" {
		t.Fatalf("повторная настройка: %+v", r)
	}
}

// TestLoginLockoutE2E — 5 неудач закрывают вход: RESOURCE_EXHAUSTED + Retry-After,
// верный пароль в паузу тоже отклоняется; после паузы вход открыт (C).
func TestLoginLockoutE2E(t *testing.T) {
	f := newWebFixture(t, false)
	f.newBrowser().setup("admin", goodWebPass)
	b := f.newBrowser()
	for i := 0; i < 5; i++ {
		if r := b.login("admin", "wrong-password-1"); r.Code != "unauthenticated" {
			t.Fatalf("неудача %d: %+v", i+1, r)
		}
	}
	r := b.login("admin", goodWebPass)
	if r.Code != "resource_exhausted" || r.Status != http.StatusTooManyRequests || r.Header.Get("Retry-After") != "1" {
		t.Fatalf("после 5 неудач: status=%d code=%q retry=%q", r.Status, r.Code, r.Header.Get("Retry-After"))
	}
	f.clk.Advance(2 * time.Second)
	if r := b.login("admin", goodWebPass); r.Code != "" {
		t.Fatalf("после паузы: %+v", r)
	}
}

// TestLoginErrorsDoNotRevealAccounts — неверный пароль и несуществующее имя
// дают одинаковый ответ (S).
func TestLoginErrorsDoNotRevealAccounts(t *testing.T) {
	f := newWebFixture(t, false)
	f.newBrowser().setup("admin", goodWebPass)
	a := f.newBrowser().login("admin", "wrong-password-1")
	g := f.newBrowser().login("ghost", "wrong-password-1")
	if a.Code != g.Code || a.str("message") != g.str("message") || a.Status != g.Status {
		t.Fatalf("ответы различаются: %+v / %+v", a, g)
	}
}

// TestCSRFAndOriginE2E — изменяющие RPC с cookie требуют CSRF-токен и свой Origin (S).
func TestCSRFAndOriginE2E(t *testing.T) {
	f := newWebFixture(t, false)
	b := f.newBrowser()
	b.setup("admin", goodWebPass)
	body := `{"oldPassword":"x","newPassword":"y"}`
	good := b.csrf

	b.csrf = ""
	if r := b.call("WebSessionService/ChangePassword", body); r.Status != http.StatusForbidden {
		t.Fatalf("без CSRF: %d", r.Status)
	}
	b.csrf = good
	evil := func(r *http.Request) { r.Header.Set("Origin", "https://evil.example") }
	if r := b.call("WebSessionService/ChangePassword", body, evil); r.Status != http.StatusForbidden {
		t.Fatalf("чужой Origin: %d", r.Status)
	}
	noOrigin := func(r *http.Request) { r.Header.Del("Origin") }
	if r := b.call("WebSessionService/Logout", `{}`, noOrigin); r.Status != http.StatusForbidden {
		t.Fatalf("без Origin: %d", r.Status)
	}
	if r := b.call("WebSessionService/WhoAmI", `{}`); r.Code != "" {
		t.Fatalf("сессия должна пережить отклонённые запросы: %+v", r)
	}
}

// TestBearerSeesAllSessions — headless-клиент с токеном ядра видит все сессии и
// может отозвать любую; WhoAmI сообщает «token» (bearer).
func TestBearerSeesAllSessions(t *testing.T) {
	f := newWebFixture(t, false)
	b := f.newBrowser()
	b.setup("admin", goodWebPass)
	headless := f.newBrowser()
	bearer := func(r *http.Request) { r.Header.Set("Authorization", "Bearer "+testCoreToken) }
	list := headless.raw(procPrefix+"WebSessionService/ListSessions", `{}`, false, bearer)
	sessions, _ := list.Body["sessions"].([]any)
	if list.Code != "" || len(sessions) != 1 {
		t.Fatalf("ListSessions по Bearer: %+v", list)
	}
	id := sessions[0].(map[string]any)["sessionId"].(string)
	if r := headless.raw(procPrefix+"WebSessionService/RevokeSession", `{"sessionId":"`+id+`"}`, false, bearer); r.Code != "" {
		t.Fatalf("RevokeSession по Bearer: %+v", r)
	}
	who := headless.raw(procPrefix+"WebSessionService/WhoAmI", `{}`, false, bearer)
	if who.Code != "" || who.str("username") != "token" {
		t.Fatalf("WhoAmI по Bearer: %+v", who)
	}
	if r := headless.raw(procPrefix+"WebSessionService/ChangePassword", `{}`, false, bearer); r.Code != "failed_precondition" {
		t.Fatalf("ChangePassword по Bearer: %+v", r)
	}
}

// TestLoginSetsHardenedCookie — cookie сессии: HttpOnly, SameSite=Strict, Path=/;
// по TLS ещё и Secure; токена в теле ответа нет (S).
func TestLoginSetsHardenedCookie(t *testing.T) {
	for _, tlsMode := range []bool{false, true} {
		f := newWebFixture(t, tlsMode)
		b := f.newBrowser()
		res := b.setup("admin", goodWebPass)
		var cookie *http.Cookie
		for _, c := range (&http.Response{Header: res.Header}).Cookies() {
			if c.Name == web.CookieName {
				cookie = c
			}
		}
		if cookie == nil || cookie.Value == "" {
			t.Fatalf("tls=%v: cookie сессии не выдана: %v", tlsMode, res.Header)
		}
		if !cookie.HttpOnly || cookie.SameSite != http.SameSiteStrictMode || cookie.Path != "/" || cookie.MaxAge != 43200 {
			t.Errorf("tls=%v: cookie не укреплена: %+v", tlsMode, cookie)
		}
		if cookie.Secure != tlsMode {
			t.Errorf("tls=%v: Secure=%v", tlsMode, cookie.Secure)
		}
		if strings.Contains(string(mustJSON(res.Body)), cookie.Value) {
			t.Errorf("tls=%v: токен сессии попал в тело ответа", tlsMode)
		}
		out := b.login("admin", goodWebPass)
		if out.Header.Get("Set-Cookie") == "" {
			t.Errorf("tls=%v: Login не выдал cookie", tlsMode)
		}
	}
}

// TestWebDisabledByDefault — без --web-bind все 9 методов UNIMPLEMENTED (E).
func TestWebDisabledByDefault(t *testing.T) {
	base := newOfflineRPCServer(t)
	for _, m := range []string{"GetWebModeStatus", "SetupFirstAdmin", "Login", "Logout", "RenewSession", "WhoAmI", "ChangePassword", "ListSessions", "RevokeSession"} {
		out, err := callRaw(base, "/lavray.v1.WebSessionService/"+m, false)
		if err != nil || out.code != "unimplemented" {
			t.Errorf("%s: code=%q err=%v, ждали unimplemented", m, out.code, err)
		}
	}
}

// TestWebProcedureConstantsMatchProto — пути шлюза совпадают со сгенерированными.
func TestWebProcedureConstantsMatchProto(t *testing.T) {
	pairs := map[string]string{
		web.ProcGetWebModeStatus: lavrayv1connect.WebSessionServiceGetWebModeStatusProcedure,
		web.ProcSetupFirstAdmin:  lavrayv1connect.WebSessionServiceSetupFirstAdminProcedure,
		web.ProcLogin:            lavrayv1connect.WebSessionServiceLoginProcedure,
		web.ProcWhoAmI:           lavrayv1connect.WebSessionServiceWhoAmIProcedure,
	}
	for got, want := range pairs {
		if got != want {
			t.Errorf("константа шлюза %q != %q", got, want)
		}
	}
}

// TestSetupCodeProtectsFirstAdmin — панель не на loopback: первый администратор
// создаётся только с кодом из файла, чужой клиент LAN не успевает занять панель (S).
func TestSetupCodeProtectsFirstAdmin(t *testing.T) {
	code, err := web.NewSetupCode(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	f := newWebFixtureCode(t, false, code)
	b := f.newBrowser()
	if st := b.call("WebSessionService/GetWebModeStatus", `{}`); st.Body["setupCodeRequired"] != true || st.Body["firstRunSetupRequired"] != true {
		t.Fatalf("статус до настройки: %+v", st.Body)
	}
	for _, bad := range []string{"", "WRONGCODE1"} {
		if r := b.setupWithCode("attacker", goodWebPass, bad); r.Code != "permission_denied" {
			t.Fatalf("код %q: %+v", bad, r)
		}
	}
	if st := b.call("WebSessionService/GetWebModeStatus", `{}`); st.Body["firstRunSetupRequired"] != true {
		t.Fatal("администратор создан без верного кода")
	}
	if r := b.setupWithCode("admin", goodWebPass, code.Code()); r.Code != "" {
		t.Fatalf("верный код: %+v", r)
	}
	if st := b.call("WebSessionService/GetWebModeStatus", `{}`); st.Body["setupCodeRequired"] == true {
		t.Fatalf("после настройки код всё ещё требуется: %+v", st.Body)
	}
	if code.Code() != "" {
		t.Fatal("код не потреблён")
	}
}
