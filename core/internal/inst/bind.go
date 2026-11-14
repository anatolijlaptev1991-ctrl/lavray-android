// SPDX-License-Identifier: GPL-3.0-only

package inst

import (
	"errors"
	"strings"
	"syscall"
)

// IsAddrInUse — старт упал потому, что порт уже занят (T-55, B1-36): EADDRINUSE
// на Unix, WSAEADDRINUSE (10048) на Windows. Ищет код ошибки по всей цепочке
// обёрток (sing-box → inst → api); запасной путь — текст системного сообщения.
func IsAddrInUse(err error) bool {
	if err == nil {
		return false
	}
	var errno syscall.Errno
	if errors.As(err, &errno) && (errno == syscall.EADDRINUSE || errno == 10048) {
		return true
	}
	m := strings.ToLower(err.Error())
	return strings.Contains(m, "address already in use") ||
		strings.Contains(m, "only one usage of each socket address")
}
