// SPDX-License-Identifier: GPL-3.0-only

package web

// auth.go — сценарии входа: связывает пользователей, сессии и лимитер. Слой
// api вызывает эти методы из RPC WebSessionService и сам переводит ошибки в
// коды Connect.

import (
	"errors"
	"sync"
	"unicode/utf8"
)

// ClientInfo — кто входит: адрес для лимитера и метка клиента для списка сессий.
type ClientInfo struct {
	IP    string
	Label string
}

// LoginResult — открытая сессия и токен для cookie.
type LoginResult struct {
	Token   string
	Session Session
}

// Auth — вход, первичная настройка и смена пароля.
type Auth struct {
	Users    *Users
	Sessions *Sessions
	Limiter  *Limiter
	// SetupCode — код первичной настройки; nil — код не требуется (панель на
	// loopback). Потребляется после создания администратора.
	SetupCode *SetupCode

	// mu: проверка пароля идёт по одной. Иначе пачка параллельных запросов
	// проходит Limiter.Allow до первой записанной неудачи и обходит блокировку, а
	// Argon2 (19 МиБ на расчёт) одновременно считался бы десятки раз.
	mu sync.Mutex
}

// NewAuth собирает сервис из частей.
func NewAuth(users *Users, sessions *Sessions, limiter *Limiter) *Auth {
	return &Auth{Users: users, Sessions: sessions, Limiter: limiter}
}

// Login проверяет пароль под защитой лимитера и открывает сессию. Ошибки:
// *RateLimitError (пауза), ErrNoAdmin (нужна настройка), ErrBadCredentials.
func (a *Auth) Login(username, password string, c ClientInfo) (LoginResult, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if wait := a.Limiter.Allow(username, c.IP); wait > 0 {
		return LoginResult{}, &RateLimitError{After: wait}
	}
	if overlong(username, password) { // до расчёта хэша: гигантский ввод — неудача
		a.Limiter.Failure(username, c.IP)
		return LoginResult{}, ErrBadCredentials
	}
	rec, err := a.Users.Authenticate(username, password)
	if errors.Is(err, ErrBadCredentials) {
		a.Limiter.Failure(username, c.IP)
	}
	if err != nil {
		return LoginResult{}, err
	}
	a.Limiter.Success(username, c.IP)
	return a.open(rec, c)
}

// SetupFirstAdmin создаёт первого администратора и сразу открывает сессию.
// Работает один раз: когда администратор уже есть, отказ мгновенный — без
// лимитера и без расчёта хэша (иначе один клиент выедал бы общий лимит входов).
// При включённом коде настройки (SetupCode) неверный код — ErrBadSetupCode и
// неудача для лимитера.
func (a *Auth) SetupFirstAdmin(username, password, code string, c ClientInfo) (LoginResult, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if need, err := a.Users.NeedsSetup(); err != nil {
		return LoginResult{}, err
	} else if !need {
		return LoginResult{}, ErrAdminExists
	}
	const account = "\x00setup"
	if wait := a.Limiter.Allow(account, c.IP); wait > 0 {
		return LoginResult{}, &RateLimitError{After: wait}
	}
	if a.SetupCode != nil && !a.SetupCode.Check(code) {
		a.Limiter.Failure(account, c.IP)
		return LoginResult{}, ErrBadSetupCode
	}
	rec, err := a.Users.CreateFirstAdmin(username, password)
	if err != nil {
		return LoginResult{}, err
	}
	if a.SetupCode != nil {
		a.SetupCode.Consume()
	}
	return a.open(rec, c)
}

// overlong — имя или пароль длиннее допустимого политикой: такой вход заведомо
// неверен, а считать для него хэш незачем.
func overlong(username, password string) bool {
	return len(username) > 4*maxUsernameRunes || len(password) > 4*maxPasswordRunes ||
		utf8.RuneCountInString(username) > maxUsernameRunes || utf8.RuneCountInString(password) > maxPasswordRunes
}

func (a *Auth) open(rec UserRecord, c ClientInfo) (LoginResult, error) {
	token, sess, err := a.Sessions.Create(rec, c.Label, c.IP)
	if err != nil {
		return LoginResult{}, err
	}
	return LoginResult{Token: token, Session: sess}, nil
}

// ChangePassword меняет пароль пользователя сессии sess и отзывает все его
// остальные сессии; возвращает их число. Неверный прежний пароль считается
// неудачей входа: украденной сессией пароль не подобрать.
func (a *Auth) ChangePassword(sess Session, oldPassword, newPassword, ip string) (int, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if overlong("", oldPassword) || overlong("", newPassword) {
		return 0, ErrWeakPassword
	}
	if wait := a.Limiter.Allow(sess.Username, ip); wait > 0 {
		return 0, &RateLimitError{After: wait}
	}
	err := a.Users.ChangePassword(sess.Username, oldPassword, newPassword)
	if errors.Is(err, ErrBadCredentials) {
		a.Limiter.Failure(sess.Username, ip)
	}
	if err != nil {
		return 0, err
	}
	a.Limiter.Success(sess.Username, ip)
	return a.Sessions.RevokeOthers(sess.UserID, sess.ID), nil
}
