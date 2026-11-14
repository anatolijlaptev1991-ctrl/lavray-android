// SPDX-License-Identifier: GPL-3.0-only

// Package api — ConnectRPC-реализации контракта lavray.v1 (W6,
// core-port-plan §9 W6): Core/Profile(+Group)/Subscription/Routing/Dns/
// Settings — рабочие; Log/Stats/Connection/Diagnostics/Gateway/Update/
// WebSession — заглушки unimplemented (W7/W8), сервер с ними поднимается.
//
// recover-интерсептор (interceptors.go) гарантирует: паника хендлера —
// internal error, процесс ядра жив (THREAT-MODEL «паника ядра», LNB-13/32).
// Маппинг ошибок store/cfgbuild/subs → Connect-коды — errors.go.
package api

import (
	"context"
	"errors"
	"fmt"

	"connectrpc.com/connect"

	"github.com/anatolijlaptev1991-ctrl/lavray-android/core/internal/cfgbuild"
	"github.com/anatolijlaptev1991-ctrl/lavray-android/core/internal/inst"
	"github.com/anatolijlaptev1991-ctrl/lavray-android/core/internal/presets"
	"github.com/anatolijlaptev1991-ctrl/lavray-android/core/internal/store"
	"github.com/anatolijlaptev1991-ctrl/lavray-android/core/internal/subs"
	"github.com/anatolijlaptev1991-ctrl/lavray-android/core/internal/subs/parser"
	"github.com/anatolijlaptev1991-ctrl/lavray-android/core/internal/updates"
)

// mapErr — единый маппинг ошибок домена → Connect-коды. Хендлеры возвращают
// «сырые» ошибки; интерсептор прогоняет их через mapErr.
//
//	store.ErrNotFound        → NOT_FOUND
//	store.ErrLastGroup       → FAILED_PRECONDITION (FR-UI-107)
//	store.ErrConstraint      → INVALID_ARGUMENT (несуществующая группа и пр.)
//	cfgbuild.Error           → свой код (INVALID_ARGUMENT/FAILED_PRECONDITION
//	                           из allowlist/валидации, §3.1.1)
//	subs.ConfirmRequiredError→ FAILED_PRECONDITION (не в стриме — страховка)
//	inst.ErrAlreadyRunning   → FAILED_PRECONDITION
//	inst.ErrNotRunning       → FAILED_PRECONDITION
//	presets.ErrLimit/ErrTooLarge → RESOURCE_EXHAUSTED (T-80)
//	presets.ErrFormat, presetInvalidError → INVALID_ARGUMENT (T-80)
//	context.DeadlineExceeded → DEADLINE_EXCEEDED
//	прочее                   → INTERNAL
func mapErr(err error) error {
	if err == nil {
		return nil
	}
	if ce := new(connect.Error); errors.As(err, &ce) {
		return ce
	}
	var cfgErr *cfgbuild.Error
	if errors.As(err, &cfgErr) {
		return cfgErr.ConnectError()
	}
	switch {
	case errors.Is(err, store.ErrNotFound):
		return connect.NewError(connect.CodeNotFound, err)
	case errors.Is(err, store.ErrLastGroup):
		return connect.NewError(connect.CodeFailedPrecondition, err)
	case errors.Is(err, store.ErrConstraint):
		return connect.NewError(connect.CodeInvalidArgument, err)
	case errors.Is(err, inst.ErrAlreadyRunning):
		return connect.NewError(connect.CodeFailedPrecondition, err)
	case errors.Is(err, inst.ErrNotRunning):
		return connect.NewError(connect.CodeFailedPrecondition, err)
	case errors.Is(err, presets.ErrLimit), errors.Is(err, presets.ErrTooLarge):
		return connect.NewError(connect.CodeResourceExhausted, err)
	case errors.Is(err, presets.ErrFormat):
		return connect.NewError(connect.CodeInvalidArgument, err)
	}
	if pie := new(presetInvalidError); errors.As(err, &pie) {
		return connect.NewError(connect.CodeInvalidArgument, err)
	}
	var confirm *subs.ConfirmRequiredError
	if errors.As(err, &confirm) {
		return connect.NewError(connect.CodeFailedPrecondition, err)
	}
	// A13 (аудит round 1): ошибки парсера подписок и подсистемы обновлений
	// уходили как INTERNAL с сырым текстом — UI показывал «internal error»
	// вместо причины. Разложены по смыслу:
	var pe *parser.Error
	if errors.As(err, &pe) {
		code := connect.CodeInvalidArgument
		if pe.Msg == parser.ErrTooLargeMsg {
			code = connect.CodeResourceExhausted
		}
		return connect.NewError(code, err)
	}
	if isUpdateClientError(err) {
		return connect.NewError(connect.CodeFailedPrecondition, err)
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return connect.NewError(connect.CodeDeadlineExceeded, err)
	}
	if errors.Is(err, context.Canceled) {
		return connect.NewError(connect.CodeCanceled, err)
	}
	return connect.NewError(connect.CodeInternal, err)
}

// invalidArg — INVALID_ARGUMENT с человекочитаемым сообщением.
func invalidArg(format string, args ...any) error {
	return connect.NewError(connect.CodeInvalidArgument, fmt.Errorf(format, args...))
}

// failedPrecondition — FAILED_PRECONDITION.
func failedPrecondition(format string, args ...any) error {
	return connect.NewError(connect.CodeFailedPrecondition, fmt.Errorf(format, args...))
}

// isUpdateClientError — ошибки подсистемы обновлений (проверка/скачивание/
// верификация): это не «внутренняя поломка», а состояние, которое клиент
// должен показать пользователем (подпись не сошлась, serial откатился и т.п.).
func isUpdateClientError(err error) bool {
	return errors.Is(err, updates.ErrBadSignature) ||
		errors.Is(err, updates.ErrInsufficientSignatures) ||
		errors.Is(err, updates.ErrBadRole) ||
		errors.Is(err, updates.ErrExpired) ||
		errors.Is(err, updates.ErrTimestampHorizon) ||
		errors.Is(err, updates.ErrRootNotAdvanced) ||
		errors.Is(err, updates.ErrWrongProduct) ||
		errors.Is(err, updates.ErrWrongChannel) ||
		errors.Is(err, updates.ErrSerialRollback) ||
		errors.Is(err, updates.ErrHashMismatch) ||
		errors.Is(err, updates.ErrLengthMismatch) ||
		errors.Is(err, updates.ErrUnsafePath) ||
		errors.Is(err, updates.ErrBadURL) ||
		errors.Is(err, updates.ErrNotConfigured) ||
		errors.Is(err, updates.ErrNoCheck) ||
		errors.Is(err, updates.ErrNotStaged) ||
		errors.Is(err, updates.ErrActiveSession) ||
		errors.Is(err, updates.ErrAppTooOld) ||
		errors.Is(err, updates.ErrNetwork) ||
		errors.Is(err, updates.ErrBadJSON) ||
		errors.Is(err, updates.ErrMissingField) ||
		errors.Is(err, updates.ErrBadValue) ||
		errors.Is(err, updates.ErrWrongType)
}

// notFound — NOT_FOUND.
func notFound(format string, args ...any) error {
	return connect.NewError(connect.CodeNotFound, fmt.Errorf(format, args...))
}
