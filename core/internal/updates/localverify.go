// SPDX-License-Identifier: GPL-3.0-only

// localverify.go — проверка установщика на диске по его подписи minisign и корню
// доверия: офлайн-путь «Установить из файла» (api.VerifyLocalUpdate) и подкоманда
// ядра verify-artifact, которой оболочка проверяет файл перед повышением прав.
package updates

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
)

// MaxSignatureBytes — предел размера файла .minisig (подпись — сотни байт).
const MaxSignatureBytes = 4096

// LocalPolicy — чего требуем от подписанных заявлений кроме самой подписи.
type LocalPolicy struct {
	Product    string // пусто — не сверять
	Platform   string // ключ актива платформы ("windows-x64")
	AppVersion string // установленная версия
	// AllowOlder: версия может быть не новее установленной (откат подписанным
	// прошлым установщиком). Остальные проверки те же.
	AllowOlder bool
}

// LocalResult — подтверждённые сведения об установщике.
type LocalResult struct {
	Claims ArtifactClaims
	Sha256 string
	Size   int64
}

// HashInstaller считает sha256 (hex), BLAKE2b-512 и размер файла за один проход.
func HashInstaller(path string) (string, [DigestSize]byte, int64, error) {
	var d [DigestSize]byte
	f, err := os.Open(path)
	if err != nil {
		return "", d, 0, err
	}
	defer f.Close()
	sum, b2 := sha256.New(), NewBlake2b()
	n, err := io.Copy(io.MultiWriter(sum, b2), io.LimitReader(f, MaxArtifactBytes+1))
	if err != nil {
		return "", d, 0, err
	}
	if n > MaxArtifactBytes {
		return "", d, 0, ErrTooLarge
	}
	copy(d[:], b2.Sum(nil))
	return hex.EncodeToString(sum.Sum(nil)), d, n, nil
}

// ReadSignatureFile читает .minisig, не больше MaxSignatureBytes.
func ReadSignatureFile(path string) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	raw, err := io.ReadAll(io.LimitReader(f, MaxSignatureBytes+1))
	if err != nil {
		return nil, err
	}
	if len(raw) > MaxSignatureBytes {
		return nil, fmt.Errorf("%w: файл подписи больше %d байт", ErrMinisigFormat, MaxSignatureBytes)
	}
	return raw, nil
}

// VerifyInstallerSignature: подпись по ключам роли artifact корня trust, заявления
// trusted comment не противоречат политике.
func VerifyInstallerSignature(trust *Trust, sig []byte, digest [DigestSize]byte, pol LocalPolicy) (ArtifactClaims, error) {
	parsed, err := ParseMinisig(sig)
	if err != nil {
		return ArtifactClaims{}, err
	}
	claims, err := ParseClaims(parsed.TrustedComment)
	if err != nil {
		return claims, err
	}
	if err := trust.Root().VerifyArtifactSignature(parsed, digest, claims); err != nil {
		return claims, err
	}
	return claims, pol.check(claims)
}

func (p LocalPolicy) check(c ArtifactClaims) error {
	switch {
	case p.Product != "" && c.Product != p.Product:
		return fmt.Errorf("%w: файл для продукта %q", ErrWrongProduct, c.Product)
	case c.Platform != p.Platform:
		return fmt.Errorf("файл для платформы %q, а здесь %q", c.Platform, p.Platform)
	case !p.AllowOlder && CompareVersions(c.Version, p.AppVersion) <= 0:
		return fmt.Errorf("версия файла %s не новее установленной %s", c.Version, p.AppVersion)
	}
	return nil
}

// VerifyInstallerFile проверяет файл целиком: хэш, подпись из sigPath и политику.
func VerifyInstallerFile(trust *Trust, installer, sigPath string, pol LocalPolicy) (LocalResult, error) {
	sha, digest, size, err := HashInstaller(installer)
	if err != nil {
		return LocalResult{}, fmt.Errorf("установщик: %w", err)
	}
	sig, err := ReadSignatureFile(sigPath)
	if err != nil {
		return LocalResult{}, fmt.Errorf("подпись: %w", err)
	}
	claims, err := VerifyInstallerSignature(trust, sig, digest, pol)
	if err != nil {
		return LocalResult{}, err
	}
	return LocalResult{Claims: claims, Sha256: sha, Size: size}, nil
}
