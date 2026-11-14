// SPDX-License-Identifier: GPL-3.0-only

package backup

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"testing"

	"golang.org/x/crypto/argon2"
	"golang.org/x/crypto/chacha20poly1305"
)

// fastOpts — минимально допустимые параметры Argon2id (8 МиБ, t=1, p=1):
// тесты не ждут по 0,3 с на каждый вывод ключа.
func fastOpts() Options { return Options{Params: Params{T: 1, MKiB: 8 * 1024, P: 1}} }

// countRand — детерминированный «генератор»: байты 0,1,2,… по кругу.
type countRand struct{ n byte }

func (r *countRand) Read(p []byte) (int, error) {
	for i := range p {
		p[i] = r.n
		r.n++
	}
	return len(p), nil
}

func mustSeal(t *testing.T, pw string, payload []byte) []byte {
	t.Helper()
	out, err := Seal(pw, payload, fastOpts())
	if err != nil {
		t.Fatalf("Seal: %v", err)
	}
	return out
}

func TestSealOpenRoundTrip(t *testing.T) {
	for _, payload := range [][]byte{{}, []byte("x"), bytes.Repeat([]byte("секрет "), 5000)} {
		file := mustSeal(t, "correct horse", payload)
		got, err := Open("correct horse", file)
		if err != nil {
			t.Fatalf("Open(%d байт): %v", len(payload), err)
		}
		if !bytes.Equal(got, payload) {
			t.Fatalf("данные не совпали (%d байт)", len(payload))
		}
	}
}

func TestBackupWrongPasswordFails(t *testing.T) {
	file := mustSeal(t, "правильный пароль", []byte("данные"))
	for _, pw := range []string{"неправильный", "правильный парол", "правильный пароль ", "ПРАВИЛЬНЫЙ ПАРОЛЬ"} {
		if _, err := Open(pw, file); !errors.Is(err, ErrWrongPassword) {
			t.Errorf("пароль %q: err = %v, want ErrWrongPassword", pw, err)
		}
	}
}

// wantTamperClass — какой ошибкой должно закончиться изменение байта i.
// Заголовок разбирается строго (ErrCorrupt), соль и keycheck меняют ключ или
// его проверку (ErrWrongPassword), nonce, reserved-зона AAD и тело ломают
// подлинность при верном пароле (ErrTampered). Параметры t/m/p — либо выход за
// границы (ErrCorrupt), либо другой ключ (ErrWrongPassword).
func wantTamperClass(i int) []error {
	switch {
	case i < offT || i >= offReserved && i < HeaderSize:
		return []error{ErrCorrupt}
	case i < offSalt:
		return []error{ErrCorrupt, ErrWrongPassword}
	case i < offNonce || i >= offKeyCheck && i < offReserved:
		return []error{ErrWrongPassword}
	}
	return []error{ErrTampered} // nonce и тело
}

// TestBackupTamperDetected — S: изменение ЛЮБОГО байта заголовка или тела
// никогда не даёт успешного расшифрования.
func TestBackupTamperDetected(t *testing.T) {
	const pw = "tamper-test-pw"
	file := mustSeal(t, pw, []byte("полезная нагрузка резервной копии"))
	for i := range file {
		mod := bytes.Clone(file)
		mod[i] ^= 0xFF
		got, err := Open(pw, mod)
		if err == nil {
			t.Fatalf("байт %d изменён, а Open успешен: %q", i, got)
		}
		ok := false
		for _, want := range wantTamperClass(i) {
			ok = ok || errors.Is(err, want)
		}
		if !ok {
			t.Errorf("байт %d: err = %v, ждали %v", i, err, wantTamperClass(i))
		}
	}
}

// TestTruncatedCorrupt — B: обрезанный файл на любой длине даёт ошибку.
func TestTruncatedCorrupt(t *testing.T) {
	file := mustSeal(t, "trunc-pw", []byte("данные"))
	for n := 0; n < len(file); n++ {
		_, err := Open("trunc-pw", file[:n])
		if err == nil {
			t.Fatalf("длина %d из %d: Open успешен", n, len(file))
		}
		if n < HeaderSize+TagSize && !errors.Is(err, ErrCorrupt) {
			t.Errorf("длина %d: err = %v, want ErrCorrupt", n, err)
		}
		if n >= HeaderSize+TagSize && !errors.Is(err, ErrTampered) {
			t.Errorf("длина %d: err = %v, want ErrTampered", n, err)
		}
	}
}

func TestEmptyPasswordRejected(t *testing.T) {
	if _, err := Seal("", []byte("x"), fastOpts()); !errors.Is(err, ErrEmptyPassword) {
		t.Errorf("Seal: err = %v, want ErrEmptyPassword", err)
	}
	file := mustSeal(t, "not-empty-pw", []byte("x"))
	if _, err := Open("", file); !errors.Is(err, ErrEmptyPassword) {
		t.Errorf("Open: err = %v, want ErrEmptyPassword", err)
	}
}

// TestNonceUnique — S: две копии одних и тех же данных с одним паролем не
// делят ни соль, ни nonce.
func TestNonceUnique(t *testing.T) {
	seen := map[string]bool{}
	for i := 0; i < 50; i++ {
		file := mustSeal(t, "same-pw-same-data", []byte("same data"))
		h, err := ParseHeader(file)
		if err != nil {
			t.Fatal(err)
		}
		for _, v := range [][]byte{h.Nonce[:], h.Salt[:]} {
			if seen[string(v)] {
				t.Fatalf("повтор соли или nonce на итерации %d", i)
			}
			seen[string(v)] = true
		}
	}
}

func TestSealRandFailure(t *testing.T) {
	opts := fastOpts()
	opts.Rand = io.LimitReader(&countRand{}, 10) // соль 16 байт не набирается
	if _, err := Seal("rand-fail-pw", []byte("x"), opts); err == nil {
		t.Fatal("ждём ошибку источника случайных чисел")
	}
}

func TestSealRejectsBadParams(t *testing.T) {
	for _, p := range []Params{{0, 8 * 1024, 1}, {1, 1024, 1}, {1, 8 * 1024, 0}, {11, 8 * 1024, 1}} {
		if _, err := Seal("bad-params-pw", nil, Options{Params: p}); err == nil {
			t.Errorf("params %+v приняты", p)
		}
	}
}

func TestOpenTooLarge(t *testing.T) {
	big := make([]byte, MaxFileBytes+1)
	if _, err := Open("any-password", big); !errors.Is(err, ErrTooLarge) {
		t.Fatalf("err = %v, want ErrTooLarge", err)
	}
}

// TestGoldenVectorFixedRand — формат не меняется незаметно. Эталон строится
// независимо: ключ и шифр собираются здесь теми же примитивами x/crypto, а не
// кодом пакета, и сверяются с закреплённым дайджестом файла.
func TestGoldenVectorFixedRand(t *testing.T) {
	const pw = "golden-password"
	payload := []byte("LAVBK1 golden payload")
	opts := Options{Params: Params{T: 1, MKiB: 8 * 1024, P: 1}, Rand: &countRand{}}
	file, err := Seal(pw, payload, opts)
	if err != nil {
		t.Fatal(err)
	}

	// независимая сборка: соль = байты 0..15, nonce = байты 16..39
	salt := make([]byte, 16)
	nonce := make([]byte, 24)
	for i := range salt {
		salt[i] = byte(i)
	}
	for i := range nonce {
		nonce[i] = byte(16 + i)
	}
	key := argon2.IDKey([]byte(pw), salt, 1, 8*1024, 1, 32)
	mac := hmac.New(sha256.New, key)
	mac.Write([]byte("LAVBK1 key check"))
	header := make([]byte, HeaderSize)
	copy(header, "LAVBK1")
	header[6], header[7] = 1, 1    // ver, kdf
	header[8] = 1                  // t = 1 (u32 LE)
	header[12], header[13] = 0, 32 // m = 8192 KiB (u32 LE)
	header[16] = 1                 // p
	copy(header[17:], salt)
	copy(header[33:], nonce)
	copy(header[57:], mac.Sum(nil))
	aead, err := chacha20poly1305.NewX(key)
	if err != nil {
		t.Fatal(err)
	}
	want := append(header, aead.Seal(nil, nonce, payload, header)...)
	if !bytes.Equal(file, want) {
		t.Fatalf("файл не совпал с независимой сборкой:\n got %x\nwant %x", file, want)
	}

	sum := sha256.Sum256(file)
	const golden = "7b99e89305cbc3f7e087ee86f01376965ffff82f7b7bfba40e4bef8b06030596"
	if got := hex.EncodeToString(sum[:]); got != golden {
		t.Fatalf("sha256(файл) = %s, ждём %s", got, golden)
	}
	if got, err := Open(pw, file); err != nil || !bytes.Equal(got, payload) {
		t.Fatalf("Open золотого файла: %q, %v", got, err)
	}
}

// FuzzOpenNeverPanicsNeverSucceeds — Open не паникует и не «расшифровывает»
// произвольные байты чужим паролем.
func FuzzOpenNeverPanicsNeverSucceeds(f *testing.F) {
	valid, err := Seal("some-other-pw", []byte("data"), fastOpts())
	if err != nil {
		f.Fatal(err)
	}
	f.Add(valid)
	f.Add(valid[:HeaderSize])
	f.Add([]byte{})
	f.Add([]byte("LAVBK1"))
	f.Fuzz(func(t *testing.T, data []byte) {
		// дорогие параметры KDF фаззер не проверяет: время и память
		if h, err := ParseHeader(data); err == nil && (h.Params.MKiB > 16*1024 || h.Params.T > 3 || h.Params.P > 4) {
			return
		}
		if _, err := Open("fuzz-password", data); err == nil {
			t.Fatalf("Open успешен на случайных данных (%d байт)", len(data))
		}
	})
}

// FuzzOpenMutatedValidFile — S: файл, полученный правкой настоящей копии, и
// открытие ВЕРНЫМ паролем — это путь AEAD и keycheck. Успехом может закончиться
// только открытие неизменённого файла.
func FuzzOpenMutatedValidFile(f *testing.F) {
	const pw = "fuzz-correct-pw"
	payload := []byte("полезная нагрузка резервной копии")
	// соль и nonce фиксированы: рабочий процесс фаззера строит тот же valid,
	// что и координатор, и сид «исходный файл» действительно равен valid
	opts := fastOpts()
	opts.Rand = &countRand{}
	valid, err := Seal(pw, payload, opts)
	if err != nil {
		f.Fatal(err)
	}
	f.Add(valid)
	for _, i := range []int{0, offVersion, offT, offM, offP, offSalt, offNonce, offKeyCheck, offReserved, HeaderSize, len(valid) - 1} {
		mod := bytes.Clone(valid)
		mod[i] ^= 0x01
		f.Add(mod)
	}
	f.Add(valid[:HeaderSize+TagSize])
	f.Add(valid[:len(valid)-1])
	f.Add(append(bytes.Clone(valid), 0))
	f.Fuzz(func(t *testing.T, data []byte) {
		// дорогие параметры KDF фаззер не проверяет: время и память
		if h, err := ParseHeader(data); err == nil && (h.Params.MKiB > 16*1024 || h.Params.T > 3 || h.Params.P > 4) {
			return
		}
		got, err := Open(pw, data)
		if bytes.Equal(data, valid) {
			if err != nil || !bytes.Equal(got, payload) {
				t.Fatalf("исходный файл не открылся: %v", err)
			}
			return
		}
		if err == nil {
			t.Fatalf("изменённый файл (%d байт) открылся: %q", len(data), got)
		}
	})
}
