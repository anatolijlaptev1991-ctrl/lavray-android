// SPDX-License-Identifier: GPL-3.0-only

package web

// password.go — хранение паролей панели: Argon2id (LNB-04). Формат строки:
//
//	argon2id$v=19$m=<КиБ>,t=<проходы>,p=<потоки>$<соль-b64>$<хэш-b64>
//
// (base64 без дополнения). Прежний формат pbkdf2$… читается в password_legacy.go;
// успешная проверка такого хэша сообщает «нужно перехешировать», и вызывающий
// записывает новый Argon2id-хэш.

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"fmt"
	"strconv"
	"strings"

	"golang.org/x/crypto/argon2"
)

// argonParams — стоимость Argon2id. Значения по умолчанию — вариант OWASP
// (19 МиБ, 2 прохода, 1 поток): на роутере с 256–512 МиБ памяти вход остаётся
// быстрым, а перебор на GPU дорог.
type argonParams struct {
	time    uint32
	memKiB  uint32
	threads uint8
}

var currentArgon = argonParams{time: 2, memKiB: 19 * 1024, threads: 1}

// Границы для чтения чужих хэшей: испорченная запись не должна заставить ядро
// выделить гигабайты.
const (
	argonMaxMemKiB  = 256 * 1024
	argonMaxTime    = 10
	argonMaxThreads = 8
	argonKeyLen     = 32
	argonSaltLen    = 16
)

// HashPassword возвращает Argon2id-хэш пароля со случайной солью.
func HashPassword(password string) (string, error) {
	return hashWith(password, currentArgon)
}

func hashWith(password string, p argonParams) (string, error) {
	salt := make([]byte, argonSaltLen)
	if _, err := rand.Read(salt); err != nil {
		return "", fmt.Errorf("web: источник случайности недоступен: %w", err)
	}
	key := argon2.IDKey([]byte(password), salt, p.time, p.memKiB, p.threads, argonKeyLen)
	return fmt.Sprintf("argon2id$v=%d$m=%d,t=%d,p=%d$%s$%s", argon2.Version, p.memKiB, p.time, p.threads,
		base64.RawStdEncoding.EncodeToString(salt), base64.RawStdEncoding.EncodeToString(key)), nil
}

// VerifyPassword проверяет пароль по сохранённому хэшу любого поддерживаемого
// формата. needsRehash=true при верном пароле означает: хэш устаревшего формата
// или со слабыми параметрами — запишите свежий.
func VerifyPassword(password, stored string) (ok, needsRehash bool) {
	switch {
	case strings.HasPrefix(stored, "argon2id$"):
		return verifyArgon(password, stored)
	case strings.HasPrefix(stored, "pbkdf2$"):
		return verifyPBKDF2(password, stored), true
	}
	return false, false
}

func verifyArgon(password, stored string) (ok, needsRehash bool) {
	parts := strings.Split(stored, "$")
	if len(parts) != 5 || parts[1] != "v="+strconv.Itoa(argon2.Version) {
		return false, false
	}
	p, valid := parseArgonParams(parts[2])
	if !valid {
		return false, false
	}
	salt, err1 := base64.RawStdEncoding.DecodeString(parts[3])
	want, err2 := base64.RawStdEncoding.DecodeString(parts[4])
	if err1 != nil || err2 != nil || len(salt) < 8 || len(want) < 16 || len(want) > 64 {
		return false, false
	}
	got := argon2.IDKey([]byte(password), salt, p.time, p.memKiB, p.threads, uint32(len(want)))
	if subtle.ConstantTimeCompare(got, want) != 1 {
		return false, false
	}
	return true, p != currentArgon || len(want) != argonKeyLen
}

// parseArgonParams разбирает «m=…,t=…,p=…» и проверяет границы.
func parseArgonParams(s string) (argonParams, bool) {
	fields := strings.Split(s, ",")
	if len(fields) != 3 {
		return argonParams{}, false
	}
	var vals [3]uint64
	for i, prefix := range []string{"m=", "t=", "p="} {
		if !strings.HasPrefix(fields[i], prefix) {
			return argonParams{}, false
		}
		v, err := strconv.ParseUint(fields[i][2:], 10, 32)
		if err != nil {
			return argonParams{}, false
		}
		vals[i] = v
	}
	m, t, th := vals[0], vals[1], vals[2]
	if m < 1024 || m > argonMaxMemKiB || t < 1 || t > argonMaxTime || th < 1 || th > argonMaxThreads {
		return argonParams{}, false
	}
	return argonParams{time: uint32(t), memKiB: uint32(m), threads: uint8(th)}, true
}
