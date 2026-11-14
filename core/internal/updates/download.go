// SPDX-License-Identifier: GPL-3.0-only

// download.go — загрузка установщика в staging: файл «.part», докачка через
// Range, точная длина из манифеста, потоковые sha256 и BLAKE2b-512, подпись
// minisign, fsync и rename. Обрыв по сети оставляет .part для докачки (с этого
// же или следующего источника); любое нарушение целостности (длина больше
// заявленной, sha256, подпись) удаляет файл — битые байты не оседают на диске.
package updates

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
)

// MaxArtifactBytes — жёсткий предел размера установщика (проверка до загрузки).
const MaxArtifactBytes = 512 << 20

// ArtifactVerifier проверяет подпись установщика по BLAKE2b-512 его содержимого.
type ArtifactVerifier func(digest [DigestSize]byte) error

// Downloader скачивает один актив в каталог staging.
type Downloader struct {
	Sources []ArtifactSource
	Dir     string // <данные>/updates/staging (уже существует)
	// Verify обязателен: установщик без подписи minisign не принимается (fail-closed).
	Verify   ArtifactVerifier
	Progress func(done, total int64) // монотонно возрастает; nil — без прогресса
	// Signature — текст minisig актива. Если задан, рядом с установщиком кладётся
	// «<файл>.minisig»: оболочка перед повышением прав перепроверит файл по нему
	// тем же кодом, что и ядро (подкоманда verify-artifact).
	Signature string
}

// run — состояние одной загрузки (Downloader можно переиспользовать).
type run struct {
	d        *Downloader
	asset    Asset
	reported int64
}

// Fetch возвращает путь проверенного установщика «<sha256>-<имя>». Уже
// скачанный и проверенный файл переиспользуется (идемпотентно).
func (d *Downloader) Fetch(ctx context.Context, asset Asset) (string, error) {
	if d.Verify == nil {
		return "", fmt.Errorf("%w: проверка подписи не задана", ErrArtifactRoleMissing)
	}
	if asset.Length < 1 || asset.Length > MaxArtifactBytes {
		return "", fmt.Errorf("%w: длина артефакта %d вне (0, %d]", ErrTooLarge, asset.Length, MaxArtifactBytes)
	}
	final, err := StagedPath(d.Dir, asset.Sha256, path.Base(asset.Path))
	if err != nil {
		return "", err
	}
	r := &run{d: d, asset: asset}
	if r.reuse(final) {
		return final, d.putSignature(final)
	}
	var last error
	for _, src := range d.Sources {
		err := r.fetchFrom(ctx, src, final)
		if err == nil {
			return final, nil
		}
		if ctx.Err() != nil {
			return "", ctx.Err() // отмена: .part сохраняется для докачки
		}
		last = preferIntegrity(last, err)
	}
	if last == nil {
		last = fmt.Errorf("%w: нет источников артефактов", ErrNetwork)
	}
	return "", last
}

// reuse: файл уже в staging и проходит ту же проверку целиком. Файл не
// прошёл проверку — удаляется (после закрытия: на Windows открытый файл не удалить).
func (r *run) reuse(final string) bool {
	h := newDigests()
	if err := hashFile(final, h); err != nil || h.written != r.asset.Length {
		return false
	}
	if h.check(r.asset.Sha256, r.d.Verify) != nil {
		_ = os.Remove(final)
		return false
	}
	r.report(r.asset.Length)
	return true
}

func (r *run) fetchFrom(ctx context.Context, src ArtifactSource, final string) error {
	asset, part := r.asset, final+PartSuffix
	h, f, body, offset, err := r.openPart(ctx, src, part)
	if err != nil {
		return err
	}
	closed := false
	defer func() {
		if !closed {
			_ = f.Close()
		}
	}()
	if err := r.stream(ctx, h, f, body, offset); err != nil {
		if errors.Is(err, ErrLengthMismatch) && h.written > asset.Length {
			_ = f.Close()
			closed = true
			_ = os.Remove(part)
		}
		return err
	}
	if err := h.check(asset.Sha256, r.d.Verify); err != nil {
		_ = f.Close()
		closed = true
		_ = os.Remove(part)
		return err
	}
	if err := f.Sync(); err != nil {
		return err
	}
	closed = true
	if err := f.Close(); err != nil {
		return err
	}
	if err := r.d.putSignature(final); err != nil { // подпись раньше установщика: файл без неё не появится
		return err
	}
	return os.Rename(part, final)
}

// openPart открывает .part для дозаписи и возвращает хэшеры, уже «прожёвавшие»
// имеющиеся байты. Сервер проигнорировал Range — начинаем заново.
func (r *run) openPart(ctx context.Context, src ArtifactSource, part string) (*digests, *os.File, io.ReadCloser, int64, error) {
	have := fileSize(part)
	if have > r.asset.Length {
		_ = os.Remove(part)
		have = 0
	}
	st, err := src.Open(ctx, r.asset.Path, have)
	if err != nil {
		return nil, nil, nil, 0, fmt.Errorf("%w: %v", ErrNetwork, err)
	}
	h := newDigests()
	flags := os.O_WRONLY | os.O_CREATE
	switch {
	case have > 0 && st.Start == have:
		if err := hashFile(part, h); err != nil {
			st.Body.Close()
			return nil, nil, nil, 0, err
		}
		flags |= os.O_APPEND
	case st.Start == 0:
		have, flags = 0, flags|os.O_TRUNC
	default:
		st.Body.Close()
		return nil, nil, nil, 0, fmt.Errorf("%w: сервер отдал смещение %d вместо запрошенного", ErrLengthMismatch, st.Start)
	}
	f, err := os.OpenFile(part, flags, 0o600)
	if err != nil {
		st.Body.Close()
		return nil, nil, nil, 0, err
	}
	return h, f, st.Body, have, nil
}

func (r *run) report(done int64) {
	if r.d.Progress == nil || done <= r.reported {
		return
	}
	r.reported = done
	r.d.Progress(done, r.asset.Length)
}
