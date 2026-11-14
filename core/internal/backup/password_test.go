// SPDX-License-Identifier: GPL-3.0-only

package backup

import (
	"bytes"
	"errors"
	"testing"

	"golang.org/x/text/unicode/norm"
)

// pwNFC и pwNFD — один и тот же пароль в двух формах Unicode: «é» и «й»
// одной кодовой точкой (NFC) либо буквой со следующим за ней знаком (NFD).
const (
	pwNFC = "caf\u00e9-\u0439\u043e\u0433\u0443\u0440\u0442"
	pwNFD = "cafe\u0301-\u0438\u0306\u043e\u0433\u0443\u0440\u0442"
)

func TestPasswordFixturesDiffer(t *testing.T) {
	if pwNFC == pwNFD {
		t.Fatal("формы NFC и NFD в тесте совпали")
	}
	if norm.NFC.String(pwNFD) != pwNFC || norm.NFD.String(pwNFC) != pwNFD {
		t.Fatal("пара пароля не соответствует NFC/NFD")
	}
}

func TestNormalizePassword(t *testing.T) {
	if got := NormalizePassword(pwNFD); got != pwNFC {
		t.Errorf("NormalizePassword(NFD) = %+q, want %+q", got, pwNFC)
	}
	if got := NormalizePassword(pwNFC); got != pwNFC {
		t.Errorf("NormalizePassword(NFC) изменил пароль: %+q", got)
	}
	if got := NormalizePassword("ascii-пароль"); got != "ascii-пароль" {
		t.Errorf("NormalizePassword изменил уже нормальную строку: %q", got)
	}
}

// TestSealPasswordNFCOpensWithNFD — копия, запечатанная паролем в одной форме, открывается тем же
// паролем в другой форме.
func TestSealPasswordNFCOpensWithNFD(t *testing.T) {
	payload := []byte("данные копии")
	for _, tc := range []struct{ name, seal, open string }{
		{"NFC → NFD", pwNFC, pwNFD},
		{"NFD → NFC", pwNFD, pwNFC},
		{"NFC → NFC", pwNFC, pwNFC},
		{"NFD → NFD", pwNFD, pwNFD},
	} {
		file, err := SealPassword(tc.seal, payload, fastOpts())
		if err != nil {
			t.Fatalf("%s: SealPassword: %v", tc.name, err)
		}
		got, err := OpenPassword(tc.open, file)
		if err != nil || !bytes.Equal(got, payload) {
			t.Errorf("%s: OpenPassword = %q, %v", tc.name, got, err)
		}
	}
}

// TestSealPasswordAlwaysWritesNFC — ключ выводится из NFC-формы независимо от
// того, в какой форме пришёл пароль.
func TestSealPasswordAlwaysWritesNFC(t *testing.T) {
	file, err := SealPassword(pwNFD, []byte("x"), fastOpts())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Open(pwNFC, file); err != nil {
		t.Errorf("первичный Open(NFC): %v", err)
	}
	if _, err := Open(pwNFD, file); !errors.Is(err, ErrWrongPassword) {
		t.Errorf("первичный Open(NFD) = %v, want ErrWrongPassword (файл должен быть в NFC)", err)
	}
}

// TestOpenPasswordLegacyRawNFD — копия, запечатанная «сырыми» байтами NFD
// (старые копии через сырой API), по-прежнему открывается паролем в NFD.
func TestOpenPasswordLegacyRawNFD(t *testing.T) {
	payload := []byte("старая копия")
	file := mustSeal(t, pwNFD, payload) // Seal без нормализации
	got, err := OpenPassword(pwNFD, file)
	if err != nil || !bytes.Equal(got, payload) {
		t.Fatalf("OpenPassword(NFD) старой копии: %q, %v", got, err)
	}
	// NFC-форма даёт другой ключ: старая копия ей не открывается.
	if _, err := OpenPassword(pwNFC, file); !errors.Is(err, ErrWrongPassword) {
		t.Errorf("OpenPassword(NFC) старой NFD-копии = %v, want ErrWrongPassword", err)
	}
}

func TestOpenPasswordWrongPassword(t *testing.T) {
	file, err := SealPassword(pwNFC, []byte("x"), fastOpts())
	if err != nil {
		t.Fatal(err)
	}
	legacy := mustSeal(t, pwNFD, []byte("x"))
	for _, pw := range []string{pwNFC + "!", pwNFD + "!", "совсем другой пароль", "ascii-wrong"} {
		for name, f := range map[string][]byte{"NFC-копия": file, "NFD-копия": legacy} {
			if _, err := OpenPassword(pw, f); !errors.Is(err, ErrWrongPassword) {
				t.Errorf("%s, пароль %+q: err = %v, want ErrWrongPassword", name, pw, err)
			}
		}
	}
}

// TestOpenPasswordPropagatesOtherErrors — запасной вариант с сырым паролем
// пробуется только после «неверный пароль»; другие ошибки возвращаются как есть.
func TestOpenPasswordPropagatesOtherErrors(t *testing.T) {
	file, err := SealPassword(pwNFC, []byte("тело"), fastOpts())
	if err != nil {
		t.Fatal(err)
	}
	file[len(file)-1] ^= 0xFF
	if _, err := OpenPassword(pwNFD, file); !errors.Is(err, ErrTampered) {
		t.Errorf("повреждённое тело: err = %v, want ErrTampered", err)
	}
	if _, err := OpenPassword(pwNFD, []byte("не копия")); !errors.Is(err, ErrCorrupt) {
		t.Errorf("не копия: err = %v, want ErrCorrupt", err)
	}
	if _, err := OpenPassword("", file); !errors.Is(err, ErrEmptyPassword) {
		t.Errorf("пустой пароль: err = %v, want ErrEmptyPassword", err)
	}
	if _, err := SealPassword("", nil, fastOpts()); !errors.Is(err, ErrEmptyPassword) {
		t.Errorf("SealPassword(пустой) = %v, want ErrEmptyPassword", err)
	}
}

// TestPasswordASCIIUnchanged — для ASCII и уже нормальных паролей поведение
// прежнее: SealPassword даёт те же байты, что Seal, и Open их открывает.
func TestPasswordASCIIUnchanged(t *testing.T) {
	const pw = "golden-password"
	payload := []byte("LAVBK1 golden payload")
	mk := func() Options {
		o := fastOpts()
		o.Rand = &countRand{}
		return o
	}
	a, err := Seal(pw, payload, mk())
	if err != nil {
		t.Fatal(err)
	}
	b, err := SealPassword(pw, payload, mk())
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(a, b) {
		t.Fatal("SealPassword изменил файл для ASCII-пароля")
	}
	if got, err := OpenPassword(pw, a); err != nil || !bytes.Equal(got, payload) {
		t.Fatalf("OpenPassword(ASCII): %q, %v", got, err)
	}
}
