// SPDX-License-Identifier: GPL-3.0-only

// minisig_verify.go — проверка подписи minisign: подпись файла (Ed — по
// содержимому, ED — по BLAKE2b-512) и глобальная подпись trusted comment
// (ed25519 над «подпись файла || trusted comment»), плюс сверка keyid и
// ожидаемых заявлений «product;platform;channel;version;serial».
package updates

import (
	"crypto/ed25519"
	"crypto/subtle"
	"fmt"
	"strconv"
	"strings"

	"golang.org/x/crypto/blake2b"
)

// DigestSize — размер BLAKE2b-512, которым подписывается prehashed-артефакт.
const DigestSize = blake2b.Size

// ArtifactClaims — что подписано trusted comment: именно эта строка связывает
// файл с продуктом, платформой, каналом, версией и serial манифеста.
type ArtifactClaims struct {
	Product, Platform, Channel, Version string
	Serial                              int64
}

// String — «product;platform;channel;version;serial».
func (c ArtifactClaims) String() string {
	return c.Product + ";" + c.Platform + ";" + c.Channel + ";" + c.Version + ";" + strconv.FormatInt(c.Serial, 10)
}

// ParseClaims разбирает trusted comment «product;platform;channel;version;serial».
func ParseClaims(trusted string) (ArtifactClaims, error) {
	p := strings.Split(trusted, ";")
	serial, err := strconv.ParseInt(p[len(p)-1], 10, 64)
	if len(p) != 5 || err != nil || serial < 1 {
		return ArtifactClaims{}, fmt.Errorf("%w: trusted comment не вида product;platform;channel;version;serial", ErrMinisigTrustedComment)
	}
	return ArtifactClaims{Product: p[0], Platform: p[1], Channel: p[2], Version: p[3], Serial: serial}, nil
}

// Verify проверяет подпись над сообщением целиком (оба алгоритма).
func (k *MinisignKey) Verify(sig *Minisig, msg []byte) error {
	if err := k.checkKeyID(sig); err != nil {
		return err
	}
	signed := msg
	if sig.Alg == MinisigAlgPrehashed {
		d := blake2b.Sum512(msg)
		signed = d[:]
	}
	return k.verifySigned(sig, signed)
}

// VerifyDigest проверяет prehashed-подпись по готовому BLAKE2b-512 (потоковая
// загрузка не держит файл в памяти). Legacy «Ed» без файла проверить нельзя.
func (k *MinisignKey) VerifyDigest(sig *Minisig, digest [DigestSize]byte) error {
	if sig.Alg != MinisigAlgPrehashed {
		return fmt.Errorf("%w: для артефакта нужна prehashed-подпись %q", ErrMinisigFormat, MinisigAlgPrehashed)
	}
	if err := k.checkKeyID(sig); err != nil {
		return err
	}
	return k.verifySigned(sig, digest[:])
}

func (k *MinisignKey) checkKeyID(sig *Minisig) error {
	if subtle.ConstantTimeCompare(k.KeyID[:], sig.KeyID[:]) != 1 {
		return ErrMinisigKeyID
	}
	return nil
}

func (k *MinisignKey) verifySigned(sig *Minisig, signed []byte) error {
	if !ed25519.Verify(k.Pub, signed, sig.Sig[:]) {
		return fmt.Errorf("%w: подпись файла minisign", ErrBadSignature)
	}
	global := append(append([]byte(nil), sig.Sig[:]...), sig.TrustedComment...)
	if !ed25519.Verify(k.Pub, global, sig.GlobalSig[:]) {
		return fmt.Errorf("%w: %w", ErrBadSignature, ErrMinisigTrustedComment)
	}
	return nil
}

// CheckClaims сверяет trusted comment с ожидаемыми заявлениями (после того
// как глобальная подпись подтвердила, что comment не подделан).
func (m *Minisig) CheckClaims(want ArtifactClaims) error {
	if m.TrustedComment != want.String() {
		return fmt.Errorf("%w: подписано %q, ожидалось %q", ErrMinisigTrustedComment, m.TrustedComment, want.String())
	}
	return nil
}
