// SPDX-License-Identifier: GPL-3.0-only

package api

// websession_service.go — WebSessionService (T-76): 9 RPC веб-панели поверх
// web.Auth. Сессию и адрес клиента даёт шлюз web.Gate через контекст
// (web.InfoFrom); cookie выставляется заголовком ответа. Включается только
// при --web-bind (server_ext.go), иначе сервис UNIMPLEMENTED.
//
// Коды ошибок: Login — UNAUTHENTICATED (общий текст), RESOURCE_EXHAUSTED с
// Retry-After, FAILED_PRECONDITION (нет администратора); SetupFirstAdmin —
// FAILED_PRECONDITION, если администратор уже есть; RevokeSession — NOT_FOUND.

import (
	"context"
	"errors"
	"time"

	"connectrpc.com/connect"

	lavrayv1 "github.com/anatolijlaptev1991-ctrl/lavray-android/core/internal/gen/lavrayv1"
	"github.com/anatolijlaptev1991-ctrl/lavray-android/core/internal/logs"
	"github.com/anatolijlaptev1991-ctrl/lavray-android/core/internal/web"
)

type webSessionService struct {
	auth *web.Auth
	opts WebOptions
	logs *logs.Mux
}

func (w *webSessionService) logf(level, format string, args ...any) {
	w.logs.Logf(level, logs.SourceAPI, "web: "+format, args...)
}

func (w *webSessionService) GetWebModeStatus(_ context.Context, _ *connect.Request[lavrayv1.GetWebModeStatusRequest]) (*connect.Response[lavrayv1.WebModeStatus], error) {
	need, err := w.auth.Users.NeedsSetup()
	if err != nil {
		return nil, err
	}
	st := &lavrayv1.WebModeStatus{
		WebModeEnabled:            true,
		FirstRunSetupRequired:     need,
		BindAddress:               w.opts.BindAddress,
		TlsEnabled:                w.opts.TLSEnabled,
		SessionTtlMinutes:         int32(web.IdleTTL / time.Minute),
		SessionAbsoluteTtlMinutes: int32(web.AbsoluteTTL / time.Minute),
		// ActiveSessions до входа не раскрывается: статус открыт любому клиенту LAN.
		SetupCodeRequired: need && w.opts.SetupCode != nil && w.opts.SetupCode.Code() != "",
	}
	if w.opts.TLSEnabled && w.opts.TLSFingerprint != "" {
		st.TlsFingerprintSha256 = strpIf(w.opts.TLSFingerprint)
	}
	return connect.NewResponse(st), nil
}

func (w *webSessionService) SetupFirstAdmin(ctx context.Context, req *connect.Request[lavrayv1.SetupFirstAdminRequest]) (*connect.Response[lavrayv1.WebSession], error) {
	info, _ := web.InfoFrom(ctx)
	res, err := w.auth.SetupFirstAdmin(req.Msg.Username, req.Msg.Password, req.Msg.GetSetupCode(), clientOf(info, req.Header().Get("User-Agent"), ""))
	if err != nil {
		if errors.Is(err, web.ErrBadSetupCode) {
			w.logf(logs.LevelWarn, "неверный код первичной настройки с %s", info.IP)
		}
		return nil, webErr(err)
	}
	w.logf(logs.LevelInfo, "создан первый администратор %q с %s", res.Session.Username, info.IP)
	return w.sessionResponse(res, info.Secure), nil
}

func (w *webSessionService) Login(ctx context.Context, req *connect.Request[lavrayv1.LoginRequest]) (*connect.Response[lavrayv1.WebSession], error) {
	info, _ := web.InfoFrom(ctx)
	client := clientOf(info, req.Header().Get("User-Agent"), req.Msg.GetClientLabel())
	res, err := w.auth.Login(req.Msg.Username, req.Msg.Password, client)
	if err != nil {
		if errors.Is(err, web.ErrBadCredentials) {
			w.logf(logs.LevelWarn, "неудачный вход %q с %s", clipLog(req.Msg.Username), info.IP)
		}
		return nil, webErr(err)
	}
	w.logf(logs.LevelInfo, "вход %q с %s", res.Session.Username, info.IP)
	return w.sessionResponse(res, info.Secure), nil
}

func (w *webSessionService) Logout(ctx context.Context, _ *connect.Request[lavrayv1.LogoutRequest]) (*connect.Response[lavrayv1.Empty], error) {
	info, _ := web.InfoFrom(ctx)
	if info.Token != "" {
		w.auth.Sessions.RevokeToken(info.Token)
	}
	resp := connect.NewResponse(&lavrayv1.Empty{})
	resp.Header().Add("Set-Cookie", web.ClearCookie(info.Secure).String())
	return resp, nil
}

func (w *webSessionService) RenewSession(ctx context.Context, _ *connect.Request[lavrayv1.RenewSessionRequest]) (*connect.Response[lavrayv1.WebSession], error) {
	info, _ := web.InfoFrom(ctx)
	if info.Session == nil {
		return nil, failedPrecondition("продлевать можно только сессию пользователя; Bearer-токен не истекает")
	}
	// Шлюз уже продлил простой при проверке cookie; обновляем срок cookie.
	resp := connect.NewResponse(w.sessionMessage(*info.Session, true))
	resp.Header().Add("Set-Cookie", web.NewCookie(info.Token, web.IdleTTL, info.Secure).String())
	return resp, nil
}

func (w *webSessionService) WhoAmI(ctx context.Context, _ *connect.Request[lavrayv1.WhoAmIRequest]) (*connect.Response[lavrayv1.WebSession], error) {
	info, _ := web.InfoFrom(ctx)
	if info.Session == nil {
		// Bearer-токен ядра: пользователя нет, права полные.
		return connect.NewResponse(&lavrayv1.WebSession{Username: "token", Permissions: []string{"*"}}), nil
	}
	return connect.NewResponse(w.sessionMessage(*info.Session, true)), nil
}

func (w *webSessionService) ChangePassword(ctx context.Context, req *connect.Request[lavrayv1.ChangePasswordRequest]) (*connect.Response[lavrayv1.ChangePasswordResponse], error) {
	info, _ := web.InfoFrom(ctx)
	if info.Session == nil {
		return nil, failedPrecondition("пароль меняется из сессии пользователя, а не по Bearer-токену")
	}
	n, err := w.auth.ChangePassword(*info.Session, req.Msg.OldPassword, req.Msg.NewPassword, info.IP)
	if errors.Is(err, web.ErrBadCredentials) {
		w.logf(logs.LevelWarn, "неверный прежний пароль при смене, пользователь %q, адрес %s", info.Session.Username, info.IP)
		return nil, invalidArg("неверный текущий пароль")
	}
	if err != nil {
		return nil, webErr(err)
	}
	w.logf(logs.LevelInfo, "пароль пользователя %q изменён, отозвано сессий: %d", info.Session.Username, n)
	return connect.NewResponse(&lavrayv1.ChangePasswordResponse{RevokedOtherSessions: int32(n)}), nil
}

// sessionResponse — ответ Login/SetupFirstAdmin с cookie сессии.
func (w *webSessionService) sessionResponse(res web.LoginResult, secure bool) *connect.Response[lavrayv1.WebSession] {
	resp := connect.NewResponse(w.sessionMessage(res.Session, true))
	resp.Header().Add("Set-Cookie", web.NewCookie(res.Token, web.IdleTTL, secure).String())
	return resp
}

func (w *webSessionService) sessionMessage(s web.Session, withCSRF bool) *lavrayv1.WebSession {
	m := &lavrayv1.WebSession{
		ExpiresAtMs: w.auth.Sessions.ExpiresAt(s).UnixMilli(),
		Username:    s.Username,
		Permissions: []string{"*"},
	}
	if withCSRF {
		m.CsrfToken = strp(s.CSRF)
	}
	return m
}

// clipLog укорачивает чужой ввод перед записью в журнал: имя из запроса может
// быть огромным, а журнал прогоняет каждую строку через маскирование секретов.
func clipLog(s string) string {
	if r := []rune(s); len(r) > 64 {
		return string(r[:64]) + "…"
	}
	return s
}

// clientOf — адрес и метка клиента для лимитера и списка сессий. Метка:
// client_label из запроса, иначе User-Agent; не длиннее 80 символов.
func clientOf(info web.RequestInfo, userAgent, label string) web.ClientInfo {
	if label == "" {
		label = userAgent
	}
	if r := []rune(label); len(r) > 80 {
		label = string(r[:80])
	}
	return web.ClientInfo{IP: info.IP, Label: label}
}
