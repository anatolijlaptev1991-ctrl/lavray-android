// SPDX-License-Identifier: GPL-3.0-only

// update_comp_errors.go — раскладка ошибок доставки пресетов и гео по Connect-
// кодам и причинам verify_failed (T-75/7.5). Нарушение целостности или
// безопасности артефакта — событие verify_failed с причиной и очисткой staging
// (как у приложения); состояние (нет прошлой ревизии, каталог занят) — ошибка
// вызова FAILED_PRECONDITION с причиной в начале сообщения.
package api

import (
	"errors"

	"connectrpc.com/connect"

	lavrayv1 "github.com/anatolijlaptev1991-ctrl/lavray-android/core/internal/gen/lavrayv1"
	"github.com/anatolijlaptev1991-ctrl/lavray-android/core/internal/logs"
	"github.com/anatolijlaptev1991-ctrl/lavray-android/core/internal/updates"
)

// Причины FAILED_PRECONDITION у Rollback/ApplyUpdates для данных.
const (
	ReasonNoPrevious = "no_previous"
	ReasonDirBusy    = "dir_busy"
)

// componentFailReason — причина verify_failed для ошибки загрузки/применения
// артефакта данных ("" — не нарушение целостности, а сбой окружения).
func componentFailReason(err error) string {
	switch {
	case errors.Is(err, updates.ErrHashMismatch):
		return "sha256_mismatch"
	case errors.Is(err, updates.ErrLengthMismatch):
		return "truncated"
	case errors.Is(err, updates.ErrTooLarge):
		return "too_large"
	case errors.Is(err, updates.ErrPresetNotNewer), errors.Is(err, updates.ErrGeoNotNewer):
		return "not_newer"
	case errors.Is(err, updates.ErrExpired):
		return "expired"
	case errors.Is(err, updates.ErrSerialRollback):
		return "serial_rollback"
	case errors.Is(err, updates.ErrNotStaged):
		return "not_staged"
	case errors.Is(err, updates.ErrPresetRejected):
		return "preset_rejected"
	case errors.Is(err, updates.ErrArchiveEntry):
		return "archive_unsafe"
	case errors.Is(err, updates.ErrArchiveLimit):
		return "archive_limits"
	case errors.Is(err, updates.ErrGeoIncomplete):
		return "geo_incomplete"
	case errors.Is(err, updates.ErrGeoInvalid):
		return "geo_invalid"
	}
	return ""
}

// logDetail пишет подробности сбоя (с путями) в журнал ядра: по RPC они не уходят.
func (s *Server) logDetail(what string, err error) {
	detail := updates.ErrorDetail(err)
	if detail == "" {
		detail = err.Error()
	}
	s.logs.Logf(logs.LevelWarn, "update", "%s: %s", what, detail)
}

// dataFailure — ошибка вызова для сбоя применения или отката данных: состояние
// окружения (нет прошлой ревизии, каталог занят) называется причиной, прочее —
// общий INTERNAL. Тексты ошибок ФС с абсолютными путями наружу не отдаются.
func (s *Server) dataFailure(c lavrayv1.UpdateComponent, err error) error {
	switch {
	case errors.Is(err, updates.ErrNoPrevious):
		return reasonError(ReasonNoPrevious, "прошлой ревизии нет: откатываться не к чему")
	case errors.Is(err, updates.ErrDirBusy):
		s.logDetail(compName(c), err)
		return reasonError(ReasonDirBusy, "каталог данных занят работающим соединением: отключитесь и повторите")
	}
	s.logDetail(compName(c), err)
	return connect.NewError(connect.CodeInternal, errors.New("не удалось выполнить действие с данными; подробности в журнале ядра"))
}

// dataApplyFailed разбирает причину неудачного применения. Нарушение целостности,
// безопасности или свежести — событие verify_failed и очистка staging; занятый
// каталог — ошибка вызова (артефакт цел, повторить можно после отключения).
func (s *Server) dataApplyFailed(c lavrayv1.UpdateComponent, staged *stagedComp, err error, send eventSend) error {
	if errors.Is(err, updates.ErrDirBusy) {
		s.upd.setCompPhase(c, lavrayv1.ComponentState_PHASE_DOWNLOADED, ReasonDirBusy)
		return s.dataFailure(c, err)
	}
	s.upd.setCompStaged(c, nil)
	s.cleanComponentStaging(staged)
	reason := componentFailReason(err)
	state := reason
	if reason == "" {
		state = "internal" // в состоянии компонента — код, а не текст ошибки с путями
	}
	s.upd.setCompPhase(c, lavrayv1.ComponentState_PHASE_FAILED, state)
	if reason == "" {
		return s.dataFailure(c, err)
	}
	send(&lavrayv1.UpdateEvent{Kind: &lavrayv1.UpdateEvent_VerifyFailed{VerifyFailed: &lavrayv1.VerificationFailed{Component: c, Reason: reason}}})
	return nil
}
