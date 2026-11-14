// SPDX-License-Identifier: GPL-3.0-only

// update_reasons.go — раскладка ошибок подсистемы обновлений по Connect-кодам
// и причинам. Причина идёт в начале сообщения («<причина>: текст») и дублируется
// заголовком lavray-update-reason: UI показывает точное сообщение, а не
// «внутренняя ошибка». «Проверка не удалась» (UNAVAILABLE) ≠ «обновлений нет».
package api

import (
	"errors"
	"fmt"
	"strings"

	"connectrpc.com/connect"

	"github.com/anatolijlaptev1991-ctrl/lavray-android/core/internal/updates"
)

// Причины FAILED_PRECONDITION у CheckUpdates (контракт с UI). Причина, которой
// нет в ui/src/lib/updates/errors.ts (сейчас metadata_invalid), показывается
// общим текстом «Обновление не прошло проверку» вместе с деталью из ядра.
const (
	ReasonNotConfigured       = "not_configured"
	ReasonBadSignature        = "bad_signature"
	ReasonExpired             = "expired"
	ReasonSerialRollback      = "serial_rollback"
	ReasonWrongProduct        = "wrong_product"
	ReasonWrongChannel        = "wrong_channel"
	ReasonRootRotation        = "root_rotation"
	ReasonArtifactRoleMissing = "artifact_role_missing"
	ReasonMirrorUnreachable   = "mirror_unreachable" // зеркало метаданных молчит (проверка не удалась, а не «нет обновлений»)
	ReasonManifestMismatch    = "manifest_mismatch"  // манифест подменён: не тот документ, что пиннит timestamp
	ReasonClockSkew           = "clock_skew"         // timestamp «из будущего»: часы компьютера отстают
	ReasonMetadataInvalid     = "metadata_invalid"   // документ метаданных подписан верно, но устроен неверно (нет поля, лишнее поле, недопустимое значение)
	ReasonActiveSession       = "active_session"
	ReasonNotStaged           = "not_staged"
	ReasonNoUpdate            = "no_update"

	// UpdateReasonHeader — заголовок ответа с причиной.
	UpdateReasonHeader = "Lavray-Update-Reason"
)

// updateReason — причина для ошибки обновлений ("" — не из списка).
func updateReason(err error) string {
	switch {
	case errors.Is(err, updates.ErrNotConfigured):
		return ReasonNotConfigured
	case errors.Is(err, updates.ErrManifestMismatch):
		return ReasonManifestMismatch
	case errors.Is(err, updates.ErrArtifactRoleMissing):
		return ReasonArtifactRoleMissing
	case errors.Is(err, updates.ErrRootRotation), errors.Is(err, updates.ErrRootNotAdvanced):
		return ReasonRootRotation
	case errors.Is(err, updates.ErrSerialRollback):
		return ReasonSerialRollback
	case errors.Is(err, updates.ErrWrongProduct):
		return ReasonWrongProduct
	case errors.Is(err, updates.ErrWrongChannel):
		return ReasonWrongChannel
	case errors.Is(err, updates.ErrTimestampHorizon):
		return ReasonClockSkew
	case errors.Is(err, updates.ErrExpired):
		return ReasonExpired
	case errors.Is(err, updates.ErrBadSignature), errors.Is(err, updates.ErrInsufficientSignatures),
		errors.Is(err, updates.ErrBadRole), errors.Is(err, updates.ErrMinisigFormat),
		errors.Is(err, updates.ErrMinisigKeyID), errors.Is(err, updates.ErrMinisigTrustedComment):
		return ReasonBadSignature
	case errors.Is(err, updates.ErrMissingField), errors.Is(err, updates.ErrBadJSON),
		errors.Is(err, updates.ErrBadValue), errors.Is(err, updates.ErrWrongType):
		// Последними: более конкретные причины выше (подпись, срок, serial) важнее.
		return ReasonMetadataInvalid
	}
	return ""
}

// reasonError — FAILED_PRECONDITION с причиной в начале сообщения и заголовке.
func reasonError(reason, format string, args ...any) error {
	e := connect.NewError(connect.CodeFailedPrecondition, fmt.Errorf(reason+": "+format, args...))
	e.Meta().Set(UpdateReasonHeader, reason)
	return e
}

// updateErr превращает ошибку проверки/загрузки в Connect-ошибку: «все
// источники недоступны» — UNAVAILABLE, остальное из списка причин —
// FAILED_PRECONDITION с причиной, прочее — как у mapErr.
func updateErr(err error) error {
	if err == nil {
		return nil
	}
	var ce *connect.Error
	if errors.As(err, &ce) {
		return ce
	}
	if reason := updateReason(err); reason != "" {
		return reasonError(reason, "%s", describeUpdateErr(reason, err))
	}
	var mirror *updates.MirrorError
	if errors.As(err, &mirror) {
		return unavailableWithReason(ReasonMirrorUnreachable, strings.Join(mirror.Hosts, ", "))
	}
	if errors.Is(err, updates.ErrNetwork) || errors.Is(err, updates.ErrTooLarge) {
		return connect.NewError(connect.CodeUnavailable, err)
	}
	return mapErr(err)
}

// unavailableWithReason — UNAVAILABLE (проверка не удалась) с причиной в начале
// сообщения и в заголовке: интерфейс показывает осмысленный текст.
func unavailableWithReason(reason, detail string) error {
	e := connect.NewError(connect.CodeUnavailable, fmt.Errorf("%s: %s", reason, detail))
	e.Meta().Set(UpdateReasonHeader, reason)
	return e
}

func describeUpdateErr(reason string, err error) string {
	if reason == ReasonNotConfigured {
		return "updates not configured (no trust root in this build)"
	}
	return err.Error()
}

// verifyFailReason — причина события verify_failed для ошибки загрузки/проверки
// ("" — это не нарушение целостности, а сбой загрузки).
func verifyFailReason(err error) string {
	switch {
	case errors.Is(err, updates.ErrHashMismatch):
		return "sha256_mismatch"
	case errors.Is(err, updates.ErrLengthMismatch):
		return "truncated"
	}
	return updateReason(err)
}
