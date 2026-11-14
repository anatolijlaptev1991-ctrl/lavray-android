// SPDX-License-Identifier: GPL-3.0-only

package updates

import "errors"

// dirBusyError — каталог нельзя заменить. Error() общий и не несёт путей
// (ошибка уходит по RPC); подробности системной ошибки доступны через Detail
// и пишутся в журнал ядра.
type dirBusyError struct{ cause error }

// NewDirBusyError оборачивает системную ошибку замены каталога.
func NewDirBusyError(cause error) error { return &dirBusyError{cause: cause} }

func (e *dirBusyError) Error() string        { return ErrDirBusy.Error() }
func (e *dirBusyError) Is(target error) bool { return target == ErrDirBusy }

// Detail — текст системной ошибки (с путями) для журнала.
func (e *dirBusyError) Detail() string {
	if e.cause == nil {
		return ""
	}
	return e.cause.Error()
}

// ErrorDetail — подробности ошибки для журнала (с путями); "" — подробностей нет.
func ErrorDetail(err error) string {
	var d interface{ Detail() string }
	if errors.As(err, &d) {
		return d.Detail()
	}
	return ""
}
