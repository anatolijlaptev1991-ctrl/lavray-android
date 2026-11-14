// SPDX-License-Identifier: GPL-3.0-only

package logs

// IsSecretKey — секретно ли значение ключа с таким именем (тот же список
// основ, что у Redact для «ключ=значение»). Для структурного маскирования
// вне журнала: diff конфига в PreviewCustomFields (T-08B).
func IsSecretKey(name string) bool { return isSecretKey(name) }
