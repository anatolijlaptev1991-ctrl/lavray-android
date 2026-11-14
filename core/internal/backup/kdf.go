// SPDX-License-Identifier: GPL-3.0-only

package backup

import (
	"crypto/hmac"
	"crypto/sha256"

	"golang.org/x/crypto/argon2"
)

// keySize — длина ключа XChaCha20-Poly1305.
const keySize = 32

// keyCheckLabel — метка, которой HMAC подтверждает «ключ верный».
const keyCheckLabel = "LAVBK1 key check"

// DeriveKey выводит 32-байтовый ключ из пароля Argon2id. Параметры уже
// проверены (Params.Validate): иначе вызывающий рискует памятью и временем.
func DeriveKey(password string, salt [SaltSize]byte, p Params) []byte {
	return argon2.IDKey([]byte(password), salt[:], p.T, p.MKiB, p.P, keySize)
}

// keyCheck = HMAC-SHA256(key, "LAVBK1 key check"): по нему «неверный пароль»
// отличается от «файл изменён» до расшифрования. Раскрытие значения ключ не
// выдаёт, а перебор пароля всё равно упирается в стоимость Argon2id.
func keyCheck(key []byte) [KeyCheckSize]byte {
	mac := hmac.New(sha256.New, key)
	mac.Write([]byte(keyCheckLabel))
	var out [KeyCheckSize]byte
	copy(out[:], mac.Sum(nil))
	return out
}

// wipe затирает ключ после использования (в пределах возможного в Go).
func wipe(b []byte) {
	for i := range b {
		b[i] = 0
	}
}
