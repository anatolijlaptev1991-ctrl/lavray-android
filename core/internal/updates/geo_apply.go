// SPDX-License-Identifier: GPL-3.0-only

// geo_apply.go — применение гео-набора из обновления (UPDATE-SYSTEM §8, T-75/7.5).
// Архив сверяется с подписанным манифестом (длина, sha256), распаковывается в
// временный каталог рядом с <данные>/geo (geo_zip.go), дополняется маркером
// ревизии и подменяет каталог атомарно (dirswap.go): прежний набор остаётся в
// geo.prev для отката. GeoDir конфигурации указывает на <данные>/geo и всегда
// видит действующую ревизию. Набор обязан содержать все файлы встроенного:
// иначе пресет, ссылающийся на отсутствующий .srs, не запустился бы.
package updates

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/anatolijlaptev1991-ctrl/lavray-android/core/internal/presets"
)

// geoSourceFile — какой архив лёг в каталог (для «уже установлено» и отчёта об откате).
const geoSourceFile = "source.json"

// GeoInstalled — сведения о наборе, лежащем в каталоге (пустые — поставка из бинарника).
type GeoInstalled struct {
	Sha256      string `json:"sha256"`
	GeneratedAt string `json:"generated_at,omitempty"`
	Rev         int64  `json:"rev,omitempty"` // ревизия манифеста; 0 — поставка из бинарника
}

// GeoApplied — итог ApplyGeo.
type GeoApplied struct {
	Changed bool // false — этот архив уже установлен
	Sha256  string
	Files   int
}

// InstalledGeo читает сведения о наборе в dir; нет маркера — нулевое значение.
// Маркер ревизии не выше встроенной (GeoRevision) значит, что набор переписан
// EnsureGeo нового бинарника: source.json устарел и набор снова предлагается.
func InstalledGeo(dir string) GeoInstalled {
	if presets.GeoRevisionOf(dir) <= presets.GeoRevision {
		return GeoInstalled{}
	}
	p := filepath.Join(dir, geoSourceFile)
	if fi, err := os.Stat(p); err != nil || fi.Size() > 4096 {
		return GeoInstalled{}
	}
	raw, err := os.ReadFile(p)
	var out GeoInstalled
	if err != nil || json.Unmarshal(raw, &out) != nil {
		return GeoInstalled{}
	}
	return out
}

// ApplyGeo проверяет архив по sec (длина и sha256 подписанного манифеста),
// распаковывает и подменяет каталог dir. required — файлы, которые набор обязан
// содержать. Любая ошибка оставляет dir и dir.prev как были.
func ApplyGeo(dir string, archive []byte, sec GeoSection, lim GeoLimits, required []string) (*GeoApplied, error) {
	if int64(len(archive)) != sec.Length {
		return nil, fmt.Errorf("%w: архив %d байт, в манифесте %d", ErrLengthMismatch, len(archive), sec.Length)
	}
	if err := CheckSha256(archive, sec.Sha256); err != nil {
		return nil, err
	}
	have := InstalledGeo(dir)
	if have.Sha256 == sec.Sha256 {
		return &GeoApplied{Sha256: sec.Sha256}, nil
	}
	if sec.Rev < 1 || sec.Rev <= have.Rev {
		return nil, fmt.Errorf("%w: rev %d при установленной %d", ErrGeoNotNewer, sec.Rev, have.Rev)
	}
	CleanSwapLeftovers(dir)
	next, err := NewSiblingDir(dir)
	if err != nil {
		return nil, err
	}
	files, err := buildGeoDir(dir, next, archive, sec, lim, required)
	if err == nil {
		err = SwapDir(dir, next)
	}
	if err != nil {
		_ = os.RemoveAll(next)
		return nil, err
	}
	return &GeoApplied{Changed: true, Sha256: sec.Sha256, Files: len(files)}, nil
}

// buildGeoDir заполняет next: файлы архива и маркеры ревизии и источника.
func buildGeoDir(cur, next string, archive []byte, sec GeoSection, lim GeoLimits, required []string) ([]string, error) {
	files, err := extractGeoZip(archive, next, lim)
	if err != nil {
		return nil, err
	}
	if err := requireGeoFiles(files, required); err != nil {
		return nil, err
	}
	rev := max(presets.GeoRevision, presets.GeoRevisionOf(cur)) + 1
	marker := fmt.Sprintf("%d %s\n", rev, time.Now().UTC().Format(time.RFC3339))
	if err := writeFileSynced(filepath.Join(next, presets.GeoMarkerFile), []byte(marker)); err != nil {
		return nil, err
	}
	src, err := json.Marshal(GeoInstalled{Sha256: sec.Sha256, GeneratedAt: sec.GeneratedAt, Rev: sec.Rev})
	if err != nil {
		return nil, err
	}
	if err := writeFileSynced(filepath.Join(next, geoSourceFile), src); err != nil {
		return nil, err
	}
	for _, d := range []string{next, filepath.Join(next, "geoip"), filepath.Join(next, "geosite")} {
		syncDir(d)
	}
	return files, nil
}

// requireGeoFiles: среди распакованных файлов есть все обязательные.
func requireGeoFiles(files, required []string) error {
	have := make(map[string]bool, len(files))
	for _, f := range files {
		have[f] = true
	}
	for _, r := range required {
		if !have[r] {
			return fmt.Errorf("%w: нет %q", ErrGeoIncomplete, r)
		}
	}
	if len(files) == 0 {
		return fmt.Errorf("%w: архив пуст", ErrGeoIncomplete)
	}
	return nil
}

// RollbackGeo возвращает прошлую ревизию набора; ErrNoPrevious — её нет. Возвращённый
// набор проверяется как при запуске (presets.ExtractGeo): недостающие и повреждённые
// файлы, ставшие встроенными позже прошлой ревизии, чинятся из поставки, целые
// файлы прошлой ревизии остаются.
func RollbackGeo(dir string) error {
	if err := RollbackDir(dir); err != nil {
		return err
	}
	if _, err := presets.ExtractGeo(dir); err != nil {
		return fmt.Errorf("набор возвращён, но не восстановлен до полного: %w", err)
	}
	return nil
}
