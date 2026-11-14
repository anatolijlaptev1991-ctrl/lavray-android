// SPDX-License-Identifier: GPL-3.0-only

package api

import (
	"errors"
	"strconv"

	"connectrpc.com/connect"

	"github.com/anatolijlaptev1991-ctrl/lavray-android/core/internal/web"
)

// webErr переводит ошибки входа в коды Connect:
//
//	ErrBadCredentials     → UNAUTHENTICATED (общий текст, без подсказок)
//	ErrBadSetupCode       → PERMISSION_DENIED
//	ErrNoAdmin            → FAILED_PRECONDITION (нужна первичная настройка)
//	ErrAdminExists        → FAILED_PRECONDITION (настройка уже выполнена)
//	ErrWeakPassword/Name  → INVALID_ARGUMENT
//	ErrSessionNotFound    → NOT_FOUND
//	*web.RateLimitError   → RESOURCE_EXHAUSTED + заголовок Retry-After
func webErr(err error) error {
	var rl *web.RateLimitError
	switch {
	case err == nil:
		return nil
	case errors.As(err, &rl):
		ce := connect.NewError(connect.CodeResourceExhausted, rl)
		ce.Meta().Set("Retry-After", strconv.Itoa(rl.RetrySeconds()))
		return ce
	case errors.Is(err, web.ErrBadSetupCode):
		return connect.NewError(connect.CodePermissionDenied, err)
	case errors.Is(err, web.ErrBadCredentials):
		return connect.NewError(connect.CodeUnauthenticated, web.ErrBadCredentials)
	case errors.Is(err, web.ErrNoAdmin), errors.Is(err, web.ErrAdminExists):
		return connect.NewError(connect.CodeFailedPrecondition, err)
	case errors.Is(err, web.ErrWeakPassword), errors.Is(err, web.ErrBadUsername):
		return connect.NewError(connect.CodeInvalidArgument, err)
	case errors.Is(err, web.ErrSessionNotFound), errors.Is(err, web.ErrUserNotFound):
		return connect.NewError(connect.CodeNotFound, err)
	}
	return err // прочее уйдёт как INTERNAL через mapErr интерсептора
}
