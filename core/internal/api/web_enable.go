// SPDX-License-Identifier: GPL-3.0-only

package api

// web_enable.go — включение веб-режима: хранилище пользователей поверх store
// и сборка WebSessionService. Вызывается обвязкой процесса при --web-bind.

import (
	"errors"

	"github.com/anatolijlaptev1991-ctrl/lavray-android/core/internal/logs"
	"github.com/anatolijlaptev1991-ctrl/lavray-android/core/internal/store"
	"github.com/anatolijlaptev1991-ctrl/lavray-android/core/internal/web"
)

// WebOptions — сведения о веб-слушателе для GetWebModeStatus.
type WebOptions struct {
	BindAddress    string // фактический адрес слушателя
	TLSEnabled     bool
	TLSFingerprint string // sha256 сертификата; пусто без TLS
	// SetupCode — код первичной настройки (панель не на loopback); nil — не нужен.
	SetupCode *web.SetupCode
	// Limiter, Sessions — для тестов (часы); nil — по умолчанию.
	Limiter  *web.Limiter
	Sessions *web.Sessions
}

// EnableWeb включает WebSessionService и возвращает Auth: обвязка берёт из него
// хранилище сессий для шлюза web.Gate. Вызывать до Register.
func (s *Server) EnableWeb(opts WebOptions) *web.Auth {
	users := web.NewUsers(storeUsers{st: s.st})
	users.Warn = func(msg string) { s.logs.Logf(logs.LevelWarn, logs.SourceAPI, "web: %s", msg) }
	sessions, limiter := opts.Sessions, opts.Limiter
	if sessions == nil {
		sessions = web.NewSessions(nil)
	}
	if limiter == nil {
		limiter = web.NewLimiter(nil, web.LimiterConfig{})
	}
	auth := web.NewAuth(users, sessions, limiter)
	auth.SetupCode = opts.SetupCode
	s.ext.web = &webSessionService{auth: auth, opts: opts, logs: s.logs}
	return auth
}

// storeUsers — web.UserRepo поверх таблицы web_users.
type storeUsers struct{ st *store.Store }

func (u storeUsers) CreateFirst(username, hash string) (web.UserRecord, error) {
	rec, err := u.st.CreateFirstWebUser(username, hash)
	if errors.Is(err, store.ErrWebAdminExists) {
		return web.UserRecord{}, web.ErrAdminExists
	}
	if err != nil {
		return web.UserRecord{}, err
	}
	return toUserRecord(rec), nil
}

func (u storeUsers) Get(username string) (web.UserRecord, error) {
	rec, err := u.st.GetWebUser(username)
	if errors.Is(err, store.ErrNotFound) {
		return web.UserRecord{}, web.ErrUserNotFound
	}
	if err != nil {
		return web.UserRecord{}, err
	}
	return toUserRecord(rec), nil
}

func (u storeUsers) Count() (int, error) { return u.st.CountWebUsers() }

func (u storeUsers) SetPassword(id int64, hash string) error {
	err := u.st.SetWebUserPassword(id, hash)
	if errors.Is(err, store.ErrNotFound) {
		return web.ErrUserNotFound
	}
	return err
}

func toUserRecord(u *store.WebUser) web.UserRecord {
	return web.UserRecord{ID: u.ID, Username: u.Username, PasswordHash: u.PasswordHash}
}
