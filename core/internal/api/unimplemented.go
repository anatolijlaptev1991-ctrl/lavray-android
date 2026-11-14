// SPDX-License-Identifier: GPL-3.0-only

// unimplemented.go — общая ошибка «RPC запланирован, но ещё не реализован».
// Заглушек методов здесь больше нет: пресеты (T-80) и хвосты A реализованы;
// ошибкой пока пользуется только UpdateService (ветки, которые доделывает T-75).
package api

import (
	"errors"

	"connectrpc.com/connect"
)

var errNotYetW6 = connect.NewError(connect.CodeUnimplemented, errors.New("RPC запланирован, но ещё не реализован в W6"))
