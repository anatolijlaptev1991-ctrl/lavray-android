// SPDX-License-Identifier: GPL-3.0-only

package backup

import (
	"errors"

	"golang.org/x/text/unicode/norm"
)

// password.go — пароль копии и Unicode. Один и тот же текст записывается по-
// разному («é» одной кодовой точкой — NFC — либо буквой со знаком — NFD), а
// ключ выводится из байтов. Чтобы копия, снятая на одной клавиатуре, открывалась
// на другой, приложение работает с паролем через SealPassword/OpenPassword:
// писать всегда в NFC, читать сначала NFC, затем (для старых копий) сырой
// пароль как есть. Seal/Open — низкоуровневые, байты пароля берут без изменений.

// NormalizePassword приводит пароль к форме NFC.
func NormalizePassword(password string) string {
	return norm.NFC.String(password)
}

// SealPassword шифрует payload паролем, приведённым к NFC.
func SealPassword(password string, payload []byte, opts Options) ([]byte, error) {
	return Seal(NormalizePassword(password), payload, opts)
}

// OpenPassword открывает файл паролем в форме NFC. Только если копия не
// открылась из-за «неверный пароль» и сырой пароль отличается от NFC, один раз
// пробует сырой: так открываются старые копии, снятые через сырой API с паролем
// в NFD. Остальные ошибки (формат, размер, подлинность) возвращаются сразу.
func OpenPassword(password string, file []byte) ([]byte, error) {
	nfc := NormalizePassword(password)
	plain, err := Open(nfc, file)
	if err == nil || !errors.Is(err, ErrWrongPassword) || nfc == password {
		return plain, err
	}
	return Open(password, file)
}
