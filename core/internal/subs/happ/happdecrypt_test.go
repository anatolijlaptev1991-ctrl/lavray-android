// SPDX-License-Identifier: GPL-3.0-only

package happ

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"strconv"
	"strings"
	"testing"

	"golang.org/x/crypto/chacha20poly1305"
)

// Сборки тестовых ссылок повторяют референсные конструкции
// (happ-decrypt-universal lib.rs: make_rsa_link / make_crypt5_link).

func buildRsaLink(t *testing.T, prefix string, key *rsa.PrivateKey, msg string) string {
	t.Helper()
	encrypted, err := rsa.EncryptPKCS1v15(rand.Reader, &key.PublicKey, []byte(msg))
	if err != nil {
		t.Fatal(err)
	}
	return prefix + base64StdEncode(encrypted)
}

func buildCrypt5Link(t *testing.T, marker, encodedPrivateKey, msg string) string {
	t.Helper()
	key, err := loadPrivateKey(encodedPrivateKey)
	if err != nil {
		t.Fatal(err)
	}
	chachaKey := sha256Sum([]byte(marker))
	nonce := []byte(marker + "test")

	finalB64 := base64StdEncode([]byte(msg))
	chachaPlain := m4842j(finalB64)
	encrypted, err := chachaSeal(chachaKey, nonce, []byte(chachaPlain))
	if err != nil {
		t.Fatal(err)
	}
	encryptedSegment := base64StdEncode(encrypted)

	rsaPlain := m4842j(base64StdEncode(chachaKey))
	rsaCiphertext, err := rsa.EncryptPKCS1v15(rand.Reader, &key.PublicKey, []byte(rsaPlain))
	if err != nil {
		t.Fatal(err)
	}

	body := string(nonce) + itoa(len(encryptedSegment)) + "f" + encryptedSegment + base64StdEncode(rsaCiphertext)
	shuffled := marker[:4] + body + marker[4:]
	return "happ://crypt5/" + permute4(shuffled)
}

// buildSaltedCrypt5Link — новая salted-раскладка: 2 байта-заполнителя и
// 8-байтовый salt между nonce и длиной; salt ксорится в ключ ChaCha20.
func buildSaltedCrypt5Link(t *testing.T, marker, encodedPrivateKey, msg string) string {
	t.Helper()
	key, err := loadPrivateKey(encodedPrivateKey)
	if err != nil {
		t.Fatal(err)
	}
	chachaKey := sha256Sum([]byte("salted-" + marker))
	salt := []byte(marker[:4] + "salt") // 8 байт
	nonce := []byte(marker + "salt")    // 12 байт

	finalB64 := base64StdEncode([]byte(msg))
	chachaPlain := m4842j(finalB64)
	encrypted, err := chachaSeal(chachaKey, nonce, []byte(chachaPlain))
	if err != nil {
		t.Fatal(err)
	}
	encryptedSegment := base64StdEncode(encrypted)

	saltedKey := append([]byte{}, chachaKey...)
	for i := range saltedKey {
		saltedKey[i] ^= salt[i%len(salt)]
	}
	rsaPlain := m4842j(base64StdEncode(saltedKey))
	rsaCiphertext, err := rsa.EncryptPKCS1v15(rand.Reader, &key.PublicKey, []byte(rsaPlain))
	if err != nil {
		t.Fatal(err)
	}

	body := string(nonce) + "QZ" + string(salt) + itoa(len(encryptedSegment)) + "f" + encryptedSegment + base64StdEncode(rsaCiphertext)
	shuffled := marker[:4] + body + marker[4:]
	return "happ://crypt5/" + permute4(shuffled)
}

func TestVerifiedVectors(t *testing.T) {
	if len(verifiedVectors) == 0 {
		t.Fatal("no verified vectors embedded")
	}
	for i, v := range verifiedVectors {
		got, err := Decrypt(v.Input)
		if err != nil {
			t.Errorf("vector %d: unexpected error: %v", i, err)
			continue
		}
		if got != v.Expected {
			t.Errorf("vector %d: got %q, want %q", i, got, v.Expected)
		}
	}
}

func TestSyntheticAllKeys(t *testing.T) {
	if err := loadKeys(); err != nil {
		t.Fatal(err)
	}
	prefixes := []string{"happ://crypt/", "happ://crypt2/", "happ://crypt3/", "happ://crypt4/"}
	for mode, key := range nativeKeys {
		want := "https://sub.example.com/native-mode" + itoa(mode)
		link := buildRsaLink(t, prefixes[mode], key, want)
		if got, err := Decrypt(link); err != nil || got != want {
			t.Errorf("native mode %d: got %q, %v (want %q)", mode, got, err, want)
		}
		if !IsHappLink(link) {
			t.Errorf("IsHappLink(%s...) = false", link[:20])
		}
	}

	for marker, encodedKey := range crypt5Keys {
		want := "https://sub.example.com/crypt5-" + marker
		link := buildCrypt5Link(t, marker, encodedKey, want)
		if got, err := Decrypt(link); err != nil || got != want {
			t.Errorf("crypt5 marker %s: got %q, %v (want %q)", marker, got, err, want)
		}
	}
}

func TestSyntheticSaltedAllKeys(t *testing.T) {
	if err := loadKeys(); err != nil {
		t.Fatal(err)
	}
	for marker, encodedKey := range crypt5Keys {
		want := "https://sub.example.com/salted-" + marker
		link := buildSaltedCrypt5Link(t, marker, encodedKey, want)
		if got, err := Decrypt(link); err != nil || got != want {
			t.Errorf("salted crypt5 marker %s: got %q, %v (want %q)", marker, got, err, want)
		}
	}
}

func TestRoundTripShuffles(t *testing.T) {
	const value = "abcdefghijklmnopqrstuvwxyz0123456789"
	if inverseM4831f(m4831f(value)) != value {
		t.Error("m4831f inverse mismatch")
	}
	if m4842j(m4842j(value)) != value {
		t.Error("m4842j inverse mismatch")
	}
	if permute4(permute4(value)) != value {
		t.Error("permute4 inverse mismatch")
	}
}

func TestInvalidInputs(t *testing.T) {
	bad := []string{
		"",
		"   ",
		"happ://crypt/",
		"happ://crypt/!!!not-base64!!!",
		"happ://crypt/QUJD",              // корректный base64, но не RSA-шифртекст
		"happ://crypt5/short",            // слишком коротко
		"happ://crypt5/AAAAAAAAAAAAAAAA", // неизвестный маркер
		"happ://crypt9/QUJD",             // неизвестный префикс → путь crypt5 → ошибка
	}
	for _, s := range bad {
		got, err := Decrypt(s)
		if err == nil {
			t.Errorf("Decrypt(%q) = %q, want error", s, got)
		}
	}
	if IsHappLink("https://example.com/sub") {
		t.Error("IsHappLink should reject plain urls")
	}
	if !IsHappLink("  HAPP://CRYPT5/abc") {
		t.Error("IsHappLink should accept padded/uppercase happ links")
	}
}

// TestLNB13ShortBodyNoPanic — регрессия LNB-13 (docs/BUGS.md): в nekoray-форке
// тело crypt5 длины 13–21 байт с не-цифрой в body[12] паниковало на срезе
// body[14:22]. Здесь Decrypt обязан вернуть ошибку и не паниковать.
// Таблица длин payload 8..30 по регрессионному плану бага; паника валит тест
// (внешний recover), ошибка без паники — норма.
func TestLNB13ShortBodyNoPanic(t *testing.T) {
	if err := loadKeys(); err != nil {
		t.Fatal(err)
	}
	// Берём валидный вектор, добиваем/обрезаем payload до нужной длины
	// (маркер остаётся корректным, тело — мусор).
	valid := verifiedVectors[4].Input // crypt5
	payload := valid[len("happ://crypt5/"):]

	for n := 8; n <= 30; n++ {
		for _, prefix := range []string{"happ://crypt/", "happ://crypt2/", "happ://crypt3/", "happ://crypt4/", "happ://crypt5/", "happ://crypt6/"} {
			for _, body12 := range []byte{'0', '9', 'x', 0, 0xff} {
				p := payload
				if len(p) > n {
					p = p[:n]
				} else {
					p = p + strings.Repeat("A", n-len(p))
				}
				if len(p) > 12 {
					b := []byte(p)
					b[12] = body12
					p = string(b)
				}
				link := prefix + p
				func() {
					defer func() {
						if r := recover(); r != nil {
							t.Fatalf("LNB-13: Decrypt(%q len=%d body12=%q) паника: %v", prefix+"…", n, string(body12), r)
						}
					}()
					if _, err := Decrypt(link); err == nil {
						// Мусорный вход не может расшифроваться корректно; если
						// вдруг расшифровался — проверяем, что это не паника.
						// (Теоретически недостижимо для обрезанного payload.)
						_ = err
					}
				}()
			}
		}
	}
}

// TestLNB13ExactBoundary — точные граничные тела 13/21/22 байта salted-ветки.
func TestLNB13ExactBoundary(t *testing.T) {
	if err := loadKeys(); err != nil {
		t.Fatal(err)
	}
	marker := ""
	for m := range crypt5Keys {
		marker = m
		break
	}
	for _, n := range []int{13, 20, 21, 22, 23} {
		body := strings.Repeat("Q", n)
		if body[12] == 'Q' { // не цифра → salted-ветвь предпочитается
			shuffled := marker[:4] + body + marker[4:]
			link := "happ://crypt5/" + permute4(shuffled)
			func() {
				defer func() {
					if r := recover(); r != nil {
						t.Fatalf("LNB-13: len(body)=%d паника: %v", n, r)
					}
				}()
				_, err := Decrypt(link)
				if err == nil {
					t.Fatalf("len(body)=%d: ожидалась ошибка", n)
				}
			}()
		}
	}
}

func TestUppercasePrefix(t *testing.T) {
	v := verifiedVectors[0]
	upper := strings.ToUpper("happ://crypt") + strings.TrimPrefix(v.Input, "happ://crypt")
	got, err := Decrypt(upper)
	if err != nil {
		t.Fatalf("uppercase prefix: unexpected error: %v", err)
	}
	if got != v.Expected {
		t.Fatalf("uppercase prefix: got %q, want %q", got, v.Expected)
	}
}

// terse helpers

func base64StdEncode(b []byte) string { return base64.StdEncoding.EncodeToString(b) }

func sha256Sum(b []byte) []byte {
	sum := sha256.Sum256(b)
	return sum[:]
}

func itoa(n int) string { return strconv.Itoa(n) }

func chachaSeal(key, nonce, plaintext []byte) ([]byte, error) {
	aead, err := chacha20poly1305.New(key)
	if err != nil {
		return nil, err
	}
	return aead.Seal(nil, nonce, plaintext, nil), nil
}
