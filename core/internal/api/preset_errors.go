// SPDX-License-Identifier: GPL-3.0-only

package api

// preset_errors.go — ошибки редактора наборов (T-80): коды Connect по таблице
// RPC плана. NOT_FOUND и FAILED_PRECONDITION — общие notFound/failedPrecondition.

import (
	"fmt"

	"connectrpc.com/connect"
)

// presetInvalidError — набор не прошёл проверку (INVALID_ARGUMENT); текст
// ошибки — путь поля и причина, как у presets.Validate.
type presetInvalidError struct{ err error }

func (e *presetInvalidError) Error() string { return e.err.Error() }
func (e *presetInvalidError) Unwrap() error { return e.err }

// abortedRevision — устаревшая ревизия набора (ABORTED): клиент перечитывает.
func abortedRevision(id string, have, want int) error {
	return connect.NewError(connect.CodeAborted,
		fmt.Errorf("набор %q изменён: ревизия %d, ожидалась %d — перечитайте набор", id, have, want))
}

// presetsDataLoss — повреждена запись пользовательских наборов (DATA_LOSS).
// Запись не перезаписывается: пользователь сможет её восстановить.
func presetsDataLoss(format string, args ...any) error {
	return connect.NewError(connect.CodeDataLoss, fmt.Errorf(format, args...))
}
