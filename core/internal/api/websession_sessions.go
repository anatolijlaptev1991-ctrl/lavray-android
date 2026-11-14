// SPDX-License-Identifier: GPL-3.0-only

package api

// websession_sessions.go — список и отзыв сессий (WebSessionService). Пользователь
// видит и отзывает только свои сессии; Bearer-токен ядра — все.

import (
	"context"

	"connectrpc.com/connect"

	lavrayv1 "github.com/anatolijlaptev1991-ctrl/lavray-android/core/internal/gen/lavrayv1"
	"github.com/anatolijlaptev1991-ctrl/lavray-android/core/internal/logs"
	"github.com/anatolijlaptev1991-ctrl/lavray-android/core/internal/web"
)

// scopeUserID — чьи сессии доступны: пользователя сессии либо все (0) у Bearer.
func scopeUserID(info web.RequestInfo) int64 {
	if info.Session != nil {
		return info.Session.UserID
	}
	return 0
}

func (w *webSessionService) ListSessions(ctx context.Context, _ *connect.Request[lavrayv1.Empty]) (*connect.Response[lavrayv1.ListSessionsResponse], error) {
	info, _ := web.InfoFrom(ctx)
	list := w.auth.Sessions.List(scopeUserID(info))
	out := &lavrayv1.ListSessionsResponse{Sessions: make([]*lavrayv1.SessionInfo, 0, len(list))}
	for _, s := range list {
		si := &lavrayv1.SessionInfo{
			SessionId:   s.ID,
			Username:    s.Username,
			CreatedAtMs: s.CreatedAt.UnixMilli(),
			ExpiresAtMs: w.auth.Sessions.ExpiresAt(s).UnixMilli(),
			LastSeenMs:  s.LastSeen.UnixMilli(),
			Current:     info.Session != nil && info.Session.ID == s.ID,
		}
		si.ClientLabel = strpIf(s.Label)
		si.RemoteAddr = strpIf(s.RemoteAddr)
		out.Sessions = append(out.Sessions, si)
	}
	return connect.NewResponse(out), nil
}

func (w *webSessionService) RevokeSession(ctx context.Context, req *connect.Request[lavrayv1.RevokeSessionRequest]) (*connect.Response[lavrayv1.Empty], error) {
	info, _ := web.InfoFrom(ctx)
	id := req.Msg.SessionId
	if !w.owns(info, id) || !w.auth.Sessions.Revoke(id) {
		return nil, webErr(web.ErrSessionNotFound)
	}
	w.logf(logs.LevelInfo, "сессия %s отозвана с %s", id, info.IP)
	resp := connect.NewResponse(&lavrayv1.Empty{})
	if info.Session != nil && info.Session.ID == id {
		resp.Header().Add("Set-Cookie", web.ClearCookie(info.Secure).String())
	}
	return resp, nil
}

// owns — сессия id входит в область видимости вызывающего.
func (w *webSessionService) owns(info web.RequestInfo, id string) bool {
	for _, s := range w.auth.Sessions.List(scopeUserID(info)) {
		if s.ID == id {
			return true
		}
	}
	return false
}
