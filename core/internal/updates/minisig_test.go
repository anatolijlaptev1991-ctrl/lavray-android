// SPDX-License-Identifier: GPL-3.0-only

package updates

import (
	"bytes"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"strings"
	"testing"
)

// Вектор официальной утилиты minisign (тесты rust-minisign-verify, ключ и
// подписи выпущены самим minisign; сообщение — "test").
const (
	refPubKey    = "RWQf6LRCGA9i53mlYecO4IzT51TGPpvWucNSCh1CBM0QTaLn73Y7GFO3"
	refLegacySig = "untrusted comment: signature from minisign secret key\n" +
		"RWQf6LRCGA9i59SLOFxz6NxvASXDJeRtuZykwQepbDEGt87ig1BNpWaVWuNrm73YiIiJbq71Wi+dP9eKL8OC351vwIasSSbXxwA=\n" +
		"trusted comment: timestamp:1555779966\tfile:test\n" +
		"QtKMXWyYcwdpZAlPF7tE2ENJkRd1ujvKjlj1m9RtHTBnZPa5WKU5uWRs5GoP5M/VqE81QFuMKI5k/SfNQUaOAA==\n"
	refPrehashedSig = "untrusted comment: signature from minisign secret key\n" +
		"RUQf6LRCGA9i559r3g7V1qNyJDApGip8MfqcadIgT9CuhV3EMhHoN1mGTkUidF/z7SrlQgXdy8ofjb7bNJJylDOocrCo8KLzZwo=\n" +
		"trusted comment: timestamp:1556193335\tfile:test\n" +
		"y/rUw2y8/hOUYjZU71eHp/Wo1KZ40fGy2VJEDl34XMJM+TX48Ss/17u3IvIfbVR1FkZZSNCisQbuQY+bHwhEBg==\n"
)

type testMinisignKey struct {
	priv ed25519.PrivateKey
	key  *MinisignKey
}

func newMinisignKey(t *testing.T) testMinisignKey {
	t.Helper()
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	k := &MinisignKey{Pub: pub}
	if _, err := rand.Read(k.KeyID[:]); err != nil {
		t.Fatal(err)
	}
	return testMinisignKey{priv: priv, key: k}
}

func (m testMinisignKey) sign(data []byte, trusted string) []byte {
	return SignMinisigPrehashed(m.priv, m.key.KeyID, DigestOf(data), trusted)
}

func TestMinisigVerifiedAndTamperRejected(t *testing.T) {
	k := newMinisignKey(t)
	data := []byte("installer-bytes")
	sig, err := ParseMinisig(k.sign(data, "p;w;stable;1.0.0;3"))
	if err != nil {
		t.Fatal(err)
	}
	if err := k.key.Verify(sig, data); err != nil {
		t.Fatalf("валидная подпись отвергнута: %v", err)
	}
	if err := k.key.Verify(sig, append([]byte("x"), data...)); !errors.Is(err, ErrBadSignature) {
		t.Fatalf("изменённый файл принят: %v", err)
	}
}

func TestMinisigReferenceVector(t *testing.T) {
	key, err := ParseMinisignPublicKey(refPubKey)
	if err != nil {
		t.Fatal(err)
	}
	for name, text := range map[string]string{"legacy": refLegacySig, "prehashed": refPrehashedSig} {
		sig, err := ParseMinisig([]byte(text))
		if err != nil {
			t.Fatalf("%s: разбор: %v", name, err)
		}
		if err := key.Verify(sig, []byte("test")); err != nil {
			t.Errorf("%s: эталонная подпись minisign не прошла: %v", name, err)
		}
		if err := key.Verify(sig, []byte("Test")); err == nil {
			t.Errorf("%s: чужое сообщение принято", name)
		}
	}
}

func TestMinisigPrehashedED(t *testing.T) {
	key, _ := ParseMinisignPublicKey(refPubKey)
	sig, err := ParseMinisig([]byte(refPrehashedSig))
	if err != nil || sig.Alg != MinisigAlgPrehashed {
		t.Fatalf("ожидался ED, получено %v (%v)", sig, err)
	}
	if err := key.VerifyDigest(sig, DigestOf([]byte("test"))); err != nil {
		t.Fatalf("проверка по готовому дайджесту: %v", err)
	}
}

func TestMinisigLegacyEd(t *testing.T) {
	key, _ := ParseMinisignPublicKey(refPubKey)
	sig, err := ParseMinisig([]byte(refLegacySig))
	if err != nil || sig.Alg != MinisigAlgLegacy {
		t.Fatalf("ожидался Ed, получено %v (%v)", sig, err)
	}
	if err := key.VerifyDigest(sig, DigestOf([]byte("test"))); !errors.Is(err, ErrMinisigFormat) {
		t.Fatalf("legacy не годится для потоковой проверки артефакта: %v", err)
	}
}

func TestMinisigWrongKeyID(t *testing.T) {
	k, other := newMinisignKey(t), newMinisignKey(t)
	data := []byte("x")
	sig, _ := ParseMinisig(k.sign(data, "c"))
	if err := other.key.Verify(sig, data); !errors.Is(err, ErrMinisigKeyID) {
		t.Fatalf("чужой keyid: %v", err)
	}
	// тот же keyid, но другой ключ — подпись не сходится
	forged := &MinisignKey{KeyID: k.key.KeyID, Pub: other.key.Pub}
	if err := forged.Verify(sig, data); !errors.Is(err, ErrBadSignature) {
		t.Fatalf("чужой ключ с тем же keyid: %v", err)
	}
}

func TestMinisigTrustedCommentMismatch(t *testing.T) {
	k := newMinisignKey(t)
	data := []byte("x")
	text := string(k.sign(data, "lavray-windows;windows-x64;stable;1.0.0;3"))
	forged := strings.Replace(text, "stable;1.0.0", "beta;9.9.9", 1)
	sig, err := ParseMinisig([]byte(forged))
	if err != nil {
		t.Fatal(err)
	}
	if err := k.key.Verify(sig, data); !errors.Is(err, ErrMinisigTrustedComment) {
		t.Fatalf("подменённый trusted comment принят: %v", err)
	}
	good, _ := ParseMinisig([]byte(text))
	want := ArtifactClaims{"lavray-windows", "windows-x64", "stable", "1.0.0", 3}
	if err := good.CheckClaims(want); err != nil {
		t.Fatalf("совпадающие заявления: %v", err)
	}
	want.Version = "1.0.1"
	if err := good.CheckClaims(want); !errors.Is(err, ErrMinisigTrustedComment) {
		t.Fatalf("чужая версия в заявлениях: %v", err)
	}
}

func TestMinisigGlobalSigTampered(t *testing.T) {
	k := newMinisignKey(t)
	data := []byte("x")
	sig, _ := ParseMinisig(k.sign(data, "c"))
	sig.GlobalSig[0] ^= 0xff
	if err := k.key.Verify(sig, data); !errors.Is(err, ErrMinisigTrustedComment) {
		t.Fatalf("испорченная глобальная подпись: %v", err)
	}
}

func TestMinisigCRLFAndTruncated(t *testing.T) {
	crlf := strings.ReplaceAll(refPrehashedSig, "\n", "\r\n")
	if _, err := ParseMinisig([]byte(crlf)); err != nil {
		t.Fatalf("CRLF должен приниматься: %v", err)
	}
	lines := strings.Split(strings.TrimSpace(refPrehashedSig), "\n")
	bad := map[string]string{
		"пусто":           "",
		"3 строки":        strings.Join(lines[:3], "\n"),
		"5 строк":         refPrehashedSig + "extra\n",
		"не base64":       lines[0] + "\n!!!\n" + lines[2] + "\n" + lines[3],
		"короткая":        lines[0] + "\n" + base64.StdEncoding.EncodeToString([]byte("ED12345678")) + "\n" + lines[2] + "\n" + lines[3],
		"нет untrusted":   "foo\n" + strings.Join(lines[1:], "\n"),
		"нет trusted":     lines[0] + "\n" + lines[1] + "\nfoo\n" + lines[3],
		"неизвестный alg": lines[0] + "\n" + base64.StdEncoding.EncodeToString(append([]byte("XX"), make([]byte, 72)...)) + "\n" + lines[2] + "\n" + lines[3],
		"пустая внутри":   lines[0] + "\n\n" + lines[2] + "\n" + lines[3],
		"огромный":        strings.Repeat("a", minisigMaxBytes+1),
	}
	for name, text := range bad {
		if _, err := ParseMinisig([]byte(text)); !errors.Is(err, ErrMinisigFormat) {
			t.Errorf("%s: ожидалась ErrMinisigFormat, получено %v", name, err)
		}
	}
}

func TestMinisignKeyBlobRoundTrip(t *testing.T) {
	k := newMinisignKey(t).key
	back, err := ParseMinisignPublicKey("untrusted comment: minisign public key\n" + k.Base64() + "\n")
	if err != nil || !bytes.Equal(back.Pub, k.Pub) || back.KeyID != k.KeyID {
		t.Fatalf("round-trip ключа: %v", err)
	}
	if _, err := ParseMinisignPublicKey("a\nb\nc"); !errors.Is(err, ErrMinisigFormat) {
		t.Fatalf("мусор вместо ключа: %v", err)
	}
	if _, err := MinisignKeyFromBlob(make([]byte, 42)); !errors.Is(err, ErrMinisigFormat) {
		t.Fatalf("блоб без префикса Ed: %v", err)
	}
}

func FuzzParseMinisig(f *testing.F) {
	f.Add([]byte(refLegacySig))
	f.Add([]byte(refPrehashedSig))
	f.Add([]byte(strings.ReplaceAll(refPrehashedSig, "\n", "\r\n")))
	f.Add([]byte(""))
	f.Fuzz(func(t *testing.T, raw []byte) {
		sig, err := ParseMinisig(raw)
		if err == nil && (sig == nil || (sig.Alg != MinisigAlgLegacy && sig.Alg != MinisigAlgPrehashed)) {
			t.Fatalf("успех с недопустимым алгоритмом: %+v", sig)
		}
	})
}
