// SPDX-License-Identifier: GPL-3.0-only

package web

// password_legacy.go — чтение прежнего формата pbkdf2$<итерации>$<соль>$<хэш>
// (PBKDF2-HMAC-SHA256, 600 000 итераций в прежней версии). Новые хэши в этом
// формате не пишутся: вход по такому хэшу перехеширует пароль в Argon2id.

import (
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"strconv"
	"strings"

	"golang.org/x/crypto/pbkdf2"
)

// Границы итераций: слабее 100 000 не принимаем, чрезмерные — тоже (DoS).
const (
	pbkdf2MinIterations = 100000
	pbkdf2MaxIterations = 5000000
)

func verifyPBKDF2(password, stored string) bool {
	parts := strings.Split(stored, "$")
	if len(parts) != 4 || parts[0] != "pbkdf2" {
		return false
	}
	it, err := strconv.Atoi(parts[1])
	if err != nil || it < pbkdf2MinIterations || it > pbkdf2MaxIterations {
		return false
	}
	salt, err := base64.RawStdEncoding.DecodeString(parts[2])
	if err != nil || len(salt) == 0 {
		return false
	}
	want, err := base64.RawStdEncoding.DecodeString(parts[3])
	if err != nil || len(want) == 0 || len(want) > 64 {
		return false
	}
	got := pbkdf2.Key([]byte(password), salt, it, len(want), sha256.New)
	return subtle.ConstantTimeCompare(got, want) == 1
}
