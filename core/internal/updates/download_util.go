// SPDX-License-Identifier: GPL-3.0-only

// download_util.go — вспомогательные части загрузки: двойной хэш (sha256 для
// манифеста, BLAKE2b-512 для подписи minisign), чтение с отменой, файлы .part.
package updates

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"fmt"
	"hash"
	"io"
	"os"
	"path/filepath"

	"golang.org/x/crypto/blake2b"
)

func fileSize(p string) int64 {
	if fi, err := os.Stat(p); err == nil && fi.Mode().IsRegular() {
		return fi.Size()
	}
	return 0
}

func hashFile(p string, w io.Writer) error {
	f, err := os.Open(filepath.Clean(p))
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = io.Copy(w, f)
	return err
}

// preferIntegrity: из нескольких ошибок источников показываем более важную —
// нарушение целостности/подписи, а не сетевую.
func preferIntegrity(prev, next error) error {
	if prev == nil || errors.Is(prev, ErrNetwork) {
		return next
	}
	return prev
}

// digests считает sha256 и BLAKE2b-512 за один проход.
type digests struct {
	sha     hash.Hash
	b2      hash.Hash
	written int64
}

func newDigests() *digests {
	return &digests{sha: sha256.New(), b2: NewBlake2b()}
}

// NewBlake2b — хэшер BLAKE2b-512 для prehashed-подписи minisign.
func NewBlake2b() hash.Hash {
	h, _ := blake2b.New512(nil) // без ключа ошибки не бывает
	return h
}

func (d *digests) Write(p []byte) (int, error) {
	_, _ = d.sha.Write(p)
	_, _ = d.b2.Write(p)
	d.written += int64(len(p))
	return len(p), nil
}

// check: sha256 из манифеста (за постоянное время), затем подпись minisign.
func (d *digests) check(wantSha string, verify ArtifactVerifier) error {
	got := hex.EncodeToString(d.sha.Sum(nil))
	if subtle.ConstantTimeCompare([]byte(got), []byte(wantSha)) != 1 {
		return fmt.Errorf("%w: sha256 артефакта не совпал с манифестом", ErrHashMismatch)
	}
	var digest [DigestSize]byte
	copy(digest[:], d.b2.Sum(nil))
	return verify(digest)
}

// ctxReader прерывает чтение при отмене контекста (тело ответа без таймаута).
type ctxReader struct {
	ctx context.Context
	r   io.Reader
}

func (c *ctxReader) Read(p []byte) (int, error) {
	if err := c.ctx.Err(); err != nil {
		return 0, err
	}
	return c.r.Read(p)
}

// putSignature кладёт «<установщик>.minisig» атомарно (временный файл и
// переименование). Signature пусто — ничего не делает.
func (d *Downloader) putSignature(final string) error {
	if d.Signature == "" {
		return nil
	}
	tmp := final + MinisigSuffix + ".tmp"
	if err := os.WriteFile(tmp, []byte(d.Signature), 0o600); err != nil {
		return err
	}
	if err := os.Rename(tmp, final+MinisigSuffix); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return nil
}
