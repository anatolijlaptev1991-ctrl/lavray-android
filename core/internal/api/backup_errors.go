// SPDX-License-Identifier: GPL-3.0-only

package api

// backup_errors.go — ошибки BackupService: код ConnectRPC плюс причина в
// деталях ErrorInfo { domain: "lavray.core", reason } (контракт proto). UI
// выбирает текст по причине, а не по тексту сообщения.

import (
	"context"
	"errors"
	"fmt"
	"unicode/utf8"

	"connectrpc.com/connect"
	"google.golang.org/genproto/googleapis/rpc/errdetails"

	"github.com/anatolijlaptev1991-ctrl/lavray-android/core/internal/backup"
	"github.com/anatolijlaptev1991-ctrl/lavray-android/core/internal/logs"
	"github.com/anatolijlaptev1991-ctrl/lavray-android/core/internal/store"
)

// Причины (reason) ошибок BackupService.
const (
	reasonPasswordShort    = "password_too_short"
	reasonPasswordLong     = "password_too_long"
	reasonNotRecognized    = "backup_not_recognized"
	reasonWrongPassword    = "backup_wrong_password"
	reasonTampered         = "backup_tampered"
	reasonNewerVersion     = "backup_from_newer_version"
	reasonConfirmRequired  = "confirm_replace_required"
	reasonConnectionActive = "connection_active"
	reasonTooLarge         = "backup_too_large"
	reasonBusy             = "backup_busy"
	reasonFailed           = "backup_failed"

	// errorDomain — домен ErrorInfo во всех ошибках ядра (proto/lavray.proto).
	errorDomain = "lavray.core"

	// Политика пароля копии: символов (рун) не меньше, байт не больше.
	minPasswordRunes = 8
	maxPasswordBytes = 1024
)

// backupError — ошибка Connect с деталью ErrorInfo.
func backupError(code connect.Code, reason, format string, args ...any) error {
	ce := connect.NewError(code, fmt.Errorf(format, args...))
	if d, err := connect.NewErrorDetail(&errdetails.ErrorInfo{Reason: reason, Domain: errorDomain}); err == nil {
		ce.AddDetail(d)
	}
	return ce
}

// checkBackupPassword — длина пароля: от 8 символов до 1024 байт, считая по
// нормализованной (NFC) форме — той, из которой выводится ключ и которую
// проверяет интерфейс. Пароль нигде не повторяется в тексте ошибки.
func checkBackupPassword(password string) error {
	password = backup.NormalizePassword(password)
	if len(password) > maxPasswordBytes {
		return backupError(connect.CodeInvalidArgument, reasonPasswordLong,
			"пароль длиннее %d байт", maxPasswordBytes)
	}
	if utf8.RuneCountInString(password) < minPasswordRunes {
		return backupError(connect.CodeInvalidArgument, reasonPasswordShort,
			"пароль короче %d символов", minPasswordRunes)
	}
	return nil
}

// checkBackupSize — файл копии не больше backup.MaxFileBytes.
func checkBackupSize(data []byte) error {
	if len(data) > backup.MaxFileBytes {
		return backupError(connect.CodeResourceExhausted, reasonTooLarge,
			"файл копии больше %d МиБ", backup.MaxFileBytes>>20)
	}
	return nil
}

// openBackupError — ошибка backup.Open: пароль, подлинность, формат.
func openBackupError(err error) error {
	switch {
	case errors.Is(err, backup.ErrWrongPassword):
		return backupError(connect.CodePermissionDenied, reasonWrongPassword, "неверный пароль копии")
	case errors.Is(err, backup.ErrTampered):
		return backupError(connect.CodeDataLoss, reasonTampered, "файл копии изменён или повреждён")
	case errors.Is(err, backup.ErrTooLarge):
		return backupError(connect.CodeResourceExhausted, reasonTooLarge,
			"файл копии больше %d МиБ", backup.MaxFileBytes>>20)
	case errors.Is(err, backup.ErrEmptyPassword):
		return backupError(connect.CodeInvalidArgument, reasonPasswordShort, "пароль пуст")
	case errors.Is(err, backup.ErrCorrupt):
		return backupError(connect.CodeInvalidArgument, reasonNotRecognized, "это не файл резервной копии LAVRay")
	}
	return err
}

// contentBackupError — ошибка проверки содержимого, расшифрованного верным
// паролем (контейнер, БД внутри, версия схемы).
func contentBackupError(err error) error {
	var tooNew *store.SchemaTooNewError
	switch {
	case errors.As(err, &tooNew):
		return backupError(connect.CodeFailedPrecondition, reasonNewerVersion,
			"копия сделана более новой версией LAVRay (схема v%d, эта сборка понимает до v%d)", tooNew.Found, tooNew.Supported)
	case errors.Is(err, backup.ErrCorrupt), errors.Is(err, store.ErrCorrupt), errors.Is(err, store.ErrConstraint):
		return backupError(connect.CodeDataLoss, reasonTampered, "содержимое копии повреждено")
	}
	return err
}

// failed превращает необработанную ошибку операции в общий ответ INTERNAL без
// текста ошибки: в нём бывают локальные пути (временный каталог, файл базы).
// Подробности пишутся только в журнал ядра. Ошибки Connect (уже с причиной) и
// отмена контекста проходят как есть.
func (b *backupService) failed(op string, err error) error {
	var ce *connect.Error
	switch {
	case err == nil, errors.As(err, &ce):
		return err
	case errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded):
		return mapErr(err)
	}
	b.srv.logs.Logf(logs.LevelError, logs.SourceAPI, "резервная копия (%s): %v", op, err)
	return backupError(connect.CodeInternal, reasonFailed, "операция с резервной копией не удалась; подробности в журнале ядра")
}
