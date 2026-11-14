// SPDX-License-Identifier: GPL-3.0-only

// manifest_fetch.go — загрузка и проверка manifest-<channel>.json, который
// timestamp пиннит по sha256 и длине: подмена тела (валидная подпись на другом
// документе) невозможна. Serial ниже сохранённого — откат метаданных; на
// решение «есть ли обновление» serial не влияет (его принимает availability).
package updates

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"time"
)

// ManifestQuery — что ожидается от манифеста.
type ManifestQuery struct {
	Channel     string
	Product     string // пусто → продукт не сверяется (тесты)
	SavedSerial int64
	Now         time.Time
}

// FetchManifest берёт манифест канала с источника победившего timestamp,
// затем с остальных (пиннинг делает любой источник безопасным). Нет записи
// канала в timestamp — (nil, nil): в этом канале ничего не опубликовано.
func FetchManifest(ctx context.Context, trust *Trust, srcs []Source, ts *Timestamp, q ManifestQuery) (*Manifest, Source, error) {
	name := ManifestName(q.Channel)
	entry, ok := ts.Meta[name]
	if !ok {
		return nil, nil, nil
	}
	var valErr, netErr error
	for _, s := range srcs {
		raw, err := s.Fetch(ctx, name)
		if err != nil {
			netErr = firstErr(netErr, err)
			continue
		}
		mf, err := checkManifest(trust, raw, entry, q)
		if err == nil {
			return mf, s, nil
		}
		valErr = firstErr(valErr, err)
	}
	if valErr != nil {
		return nil, nil, valErr
	}
	return nil, nil, fmt.Errorf("%w: manifest: %v", ErrNetwork, netErr)
}

func checkManifest(trust *Trust, raw []byte, entry TimestampEntry, q ManifestQuery) (*Manifest, error) {
	env, err := trust.Verify(raw, RoleManifest)
	if err != nil {
		return nil, err
	}
	sum := sha256.Sum256(env.Signed)
	if hex.EncodeToString(sum[:]) != entry.Sha256 {
		return nil, fmt.Errorf("%w: %w: sha256 manifest не совпал с timestamp", ErrManifestMismatch, ErrHashMismatch)
	}
	if int64(len(env.Signed)) != entry.Length {
		return nil, fmt.Errorf("%w: %w: длина manifest не совпала с timestamp", ErrManifestMismatch, ErrLengthMismatch)
	}
	mf, err := ParseManifest(env.Signed)
	if err != nil {
		return nil, err
	}
	return mf, checkManifestClaims(mf, entry, q)
}

func checkManifestClaims(mf *Manifest, entry TimestampEntry, q ManifestQuery) error {
	switch {
	case mf.Channel != q.Channel:
		return fmt.Errorf("%w: %q вместо %q", ErrWrongChannel, mf.Channel, q.Channel)
	case q.Product != "" && mf.Product != q.Product:
		return fmt.Errorf("%w: %q вместо %q", ErrWrongProduct, mf.Product, q.Product)
	case mf.Serial != entry.Serial:
		return fmt.Errorf("%w: %w: serial манифеста %d, в timestamp %d", ErrManifestMismatch, ErrHashMismatch, mf.Serial, entry.Serial)
	case expires(mf.Expires, q.Now):
		return fmt.Errorf("%w: manifest истёк", ErrExpired)
	case mf.Serial < q.SavedSerial:
		return fmt.Errorf("%w: serial %d < сохранённого %d", ErrSerialRollback, mf.Serial, q.SavedSerial)
	}
	return nil
}
