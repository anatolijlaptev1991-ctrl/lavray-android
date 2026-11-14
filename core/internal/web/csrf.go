// SPDX-License-Identifier: GPL-3.0-only

package web

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"fmt"
)

// CSRFHeader — заголовок с CSRF-токеном сессии в каждом изменяющем запросе
// браузера. HTTP-заголовки регистронезависимы; в коде и в proto — X-Lavray-CSRF.
const CSRFHeader = "X-Lavray-CSRF"

// newToken возвращает 32 случайных байта в base64url. Сбой источника
// случайности — ошибка, а не нулевой токен: сессия при этом не создаётся
// (fail-closed).
func newToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("web: источник случайности недоступен: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

// CSRFEqual сравнивает присланный токен с ожидаемым за постоянное время.
// Пустой токен никогда не равен ничему.
func CSRFEqual(got, want string) bool {
	if got == "" || want == "" {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(got), []byte(want)) == 1
}
