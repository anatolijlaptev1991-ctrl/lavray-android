// SPDX-License-Identifier: GPL-3.0-only

// errors.go — ошибки конфигбилдера. Коды соответствуют ConnectRPC/контракту
// lavray.v1 (запрещённый ключ custom_config → INVALID_ARGUMENT, §3.1.1).
package cfgbuild

import (
	"errors"
	"fmt"

	"connectrpc.com/connect"
	"google.golang.org/genproto/googleapis/rpc/errdetails"
)

// errorDomain — домен ErrorInfo во всех ошибках ядра (proto/lavray.proto).
const errorDomain = "lavray.core"

// Error — типизированная ошибка сборки конфига: Connect-код + путь к ключу,
// из-за которого ошибка (для INVALID_ARGUMENT allowlist-мержа).
type Error struct {
	Code connect.Code
	Key  string // ключ/секция конфига, напр. "inbounds" или "experimental.clash_api"
	// Reason — стабильный код причины для ErrorInfo.reason ("" — без детали);
	// UI выбирает по нему текст, а не по сообщению.
	Reason string
	msg    string
}

func (e *Error) Error() string {
	if e.Key != "" {
		return fmt.Sprintf("cfgbuild: %s: %s", e.Key, e.msg)
	}
	return "cfgbuild: " + e.msg
}

// ConnectError оборачивает ошибку в connect.Error для API-слоя (W6).
func (e *Error) ConnectError() *connect.Error {
	ce := connect.NewError(e.Code, e)
	if e.Reason != "" {
		if d, err := connect.NewErrorDetail(&errdetails.ErrorInfo{Reason: e.Reason, Domain: errorDomain}); err == nil {
			ce.AddDetail(d)
		}
	}
	return ce
}

// invalidArgument — INVALID_ARGUMENT с именем ключа (формат msg повторно
// подставляет key, если есть %s-плейсхолдер).
func invalidArgument(key, format string, args ...any) *Error {
	msg := fmt.Sprintf(format, args...)
	return &Error{Code: connect.CodeInvalidArgument, Key: key, msg: msg}
}

func failedPrecondition(key, format string, args ...any) *Error {
	return &Error{Code: connect.CodeFailedPrecondition, Key: key, msg: fmt.Sprintf(format, args...)}
}

// AsError возвращает *Error, если err им является.
func AsError(err error) (*Error, bool) {
	var e *Error
	if errors.As(err, &e) {
		return e, true
	}
	return nil, false
}
