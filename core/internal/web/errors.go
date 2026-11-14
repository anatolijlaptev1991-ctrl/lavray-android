// SPDX-License-Identifier: GPL-3.0-only

package web

import (
	"errors"
	"fmt"
	"time"
)

// Доменные ошибки входа и пользователей. Слой api переводит их в коды Connect
// (UNAUTHENTICATED, FAILED_PRECONDITION, INVALID_ARGUMENT, RESOURCE_EXHAUSTED).
var (
	// ErrBadCredentials — неверные имя или пароль. Текст общий: по нему нельзя
	// понять, существует ли учётная запись.
	ErrBadCredentials = errors.New("неверное имя или пароль")
	// ErrNoAdmin — ни одного администратора ещё нет (нужна первичная настройка).
	ErrNoAdmin = errors.New("администратор не создан")
	// ErrAdminExists — первичная настройка уже выполнена.
	ErrAdminExists = errors.New("администратор уже создан")
	// ErrUserNotFound — пользователя нет в хранилище.
	ErrUserNotFound = errors.New("пользователь не найден")
	// ErrWeakPassword — пароль не проходит политику.
	ErrWeakPassword = errors.New("пароль не подходит")
	// ErrBadUsername — имя не проходит проверку.
	ErrBadUsername = errors.New("недопустимое имя пользователя")
	// ErrBadSetupCode — неверный или отсутствующий код первичной настройки.
	ErrBadSetupCode = errors.New("неверный код первичной настройки")
	// ErrSessionNotFound — сессии с таким id нет.
	ErrSessionNotFound = errors.New("сессия не найдена")
)

// RateLimitError — вход временно закрыт лимитером; After — через сколько
// повторить (уходит клиенту заголовком Retry-After).
type RateLimitError struct{ After time.Duration }

func (e *RateLimitError) Error() string {
	return fmt.Sprintf("слишком много попыток входа, повторите через %d с", e.RetrySeconds())
}

// RetrySeconds — задержка в целых секундах, округлённая вверх, не меньше 1.
func (e *RateLimitError) RetrySeconds() int {
	s := int((e.After + time.Second - 1) / time.Second)
	if s < 1 {
		return 1
	}
	return s
}
