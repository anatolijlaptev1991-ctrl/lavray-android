// SPDX-License-Identifier: GPL-3.0-only

package web

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"strconv"
	"strings"
	"testing"

	"golang.org/x/crypto/pbkdf2"
)

// legacyHash — хэш прежнего формата pbkdf2$<it>$<salt>$<hash> (то, что писала
// версия до Argon2id).
func legacyHash(password string, iterations int) string {
	salt := make([]byte, 16)
	_, _ = rand.Read(salt)
	key := pbkdf2.Key([]byte(password), salt, iterations, 32, sha256.New)
	return "pbkdf2$" + strconv.Itoa(iterations) + "$" +
		base64.RawStdEncoding.EncodeToString(salt) + "$" + base64.RawStdEncoding.EncodeToString(key)
}

func TestArgon2idRoundTrip(t *testing.T) {
	h, err := HashPassword("correct horse battery")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(h, "argon2id$v=19$m=") {
		t.Fatalf("формат хэша: %q", h)
	}
	ok, rehash := VerifyPassword("correct horse battery", h)
	if !ok || rehash {
		t.Fatalf("верный пароль: ok=%v rehash=%v", ok, rehash)
	}
	if ok, _ := VerifyPassword("wrong password!", h); ok {
		t.Fatal("неверный пароль принят")
	}
	h2, _ := HashPassword("correct horse battery")
	if h == h2 {
		t.Fatal("соль не случайна: одинаковые хэши")
	}
}

func TestArgon2idVerifiesLegacyPBKDF2(t *testing.T) {
	h := legacyHash("старый-пароль-12345", 100000)
	ok, rehash := VerifyPassword("старый-пароль-12345", h)
	if !ok || !rehash {
		t.Fatalf("старый хэш: ok=%v rehash=%v, ждали ok и перехеширование", ok, rehash)
	}
	if ok, _ := VerifyPassword("другой-пароль-12345", h); ok {
		t.Fatal("неверный пароль принят старым форматом")
	}
}

func TestVerifyRejectsMalformed(t *testing.T) {
	const salt, key = "AAAAAAAAAAAAAAAAAAAAAA", "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"
	bad := []string{
		"", "plain", "pbkdf2$", "pbkdf2$10$AAAA$BBBB", // мало итераций
		"pbkdf2$100000$!!!$AAAA", "pbkdf2$abc$AAAA$BBBB",
		"argon2id$v=19$m=19456,t=2,p=1$AAAA",                    // нет хэша
		"argon2id$v=19$m=999999999,t=2,p=1$" + salt + "$" + key, // память вне границ
		"argon2id$v=19$m=19456,t=0,p=1$" + salt + "$" + key,
		"argon2id$v=18$m=19456,t=2,p=1$" + salt + "$" + key,
		"md5$abc",
	}
	for _, h := range bad {
		if ok, _ := VerifyPassword("any-password-1", h); ok {
			t.Errorf("испорченный хэш %q принят", h)
		}
	}
}

func TestVerifyOldArgonParamsAskRehash(t *testing.T) {
	// Параметры слабее текущих: пароль верен, но хэш надо обновить.
	h, err := hashWith("пароль-для-проверки", argonParams{time: 1, memKiB: 8 * 1024, threads: 1})
	if err != nil {
		t.Fatal(err)
	}
	ok, rehash := VerifyPassword("пароль-для-проверки", h)
	if !ok || !rehash {
		t.Fatalf("ok=%v rehash=%v", ok, rehash)
	}
}
