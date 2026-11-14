// SPDX-License-Identifier: GPL-3.0-only

// geo_zip.go — безопасная распаковка гео-архива (T-75/7.5, UPDATE-SYSTEM §10:
// zip-slip, симлинки, zip-бомбы). Формат — zip из стандартной библиотеки.
// Допустимы только записи «geoip/<имя>.srs» и «geosite/<имя>.srs» (раскладка
// каталога поставки <GeoDir>/<kind>/<name>.srs, cfgbuild/route.go): ни каталогов,
// ни «..», ни абсолютных путей, ни обратных слэшей, ни дисков, ни ссылок. Размеры
// проверяются по заголовкам ДО чтения и по факту при чтении.
package updates

import (
	"archive/zip"
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/anatolijlaptev1991-ctrl/lavray-android/core/internal/presets"
)

// GeoLimits — пределы распаковки гео-архива.
type GeoLimits struct {
	MaxEntries    int   // записей в архиве
	MaxFileBytes  int64 // один файл после распаковки
	MaxTotalBytes int64 // все файлы после распаковки
	MaxRatio      int64 // во сколько раз файл крупнее своей сжатой части
}

// DefaultGeoLimits — пределы для настоящих наборов (около 20 файлов по сотням КиБ).
var DefaultGeoLimits = GeoLimits{MaxEntries: 512, MaxFileBytes: 8 << 20, MaxTotalBytes: 32 << 20, MaxRatio: 100}

// MaxGeoArchiveBytes — предел размера самого архива (проверяется до загрузки).
// Настоящие наборы занимают меньше мегабайта; предел подходит и для OpenWrt.
const MaxGeoArchiveBytes = 16 << 20

// ratioFloor — файлы меньше этого размера проверке сжатия не подлежат.
const ratioFloor = 64 << 10

var (
	geoEntryRe = regexp.MustCompile(`^(geoip|geosite)/([A-Za-z0-9@!._-]+)\.srs$`)
	winDevices = map[string]bool{"con": true, "prn": true, "aux": true, "nul": true,
		"com1": true, "com2": true, "com3": true, "com4": true, "com5": true, "com6": true, "com7": true, "com8": true, "com9": true,
		"lpt1": true, "lpt2": true, "lpt3": true, "lpt4": true, "lpt5": true, "lpt6": true, "lpt7": true, "lpt8": true, "lpt9": true}
)

// geoEntryName проверяет имя записи; возвращает путь внутри каталога набора.
func geoEntryName(name string) (string, error) {
	m := geoEntryRe.FindStringSubmatch(name)
	if m == nil || strings.Contains(name, "..") {
		return "", fmt.Errorf("%w: имя %q", ErrArchiveEntry, name)
	}
	stem := strings.ToLower(strings.SplitN(m[2], ".", 2)[0])
	if winDevices[stem] {
		return "", fmt.Errorf("%w: %q — зарезервированное имя Windows", ErrArchiveEntry, name)
	}
	return name, nil
}

// vetGeoEntry проверяет запись по заголовку: имя, тип, шифрование, размеры.
func vetGeoEntry(f *zip.File, lim GeoLimits) (string, error) {
	rel, err := geoEntryName(f.Name)
	if err != nil {
		return "", err
	}
	if !f.Mode().IsRegular() {
		return "", fmt.Errorf("%w: %q — не обычный файл (ссылка, устройство, каталог)", ErrArchiveEntry, f.Name)
	}
	if f.Flags&0x1 != 0 || (f.Method != zip.Store && f.Method != zip.Deflate) {
		return "", fmt.Errorf("%w: %q зашифрована или сжата неподдерживаемым методом", ErrArchiveEntry, f.Name)
	}
	if f.UncompressedSize64 > uint64(lim.MaxFileBytes) {
		return "", fmt.Errorf("%w: %q разжимается в %d байт, предел %d", ErrArchiveLimit, f.Name, f.UncompressedSize64, lim.MaxFileBytes)
	}
	if f.UncompressedSize64 > ratioFloor && f.UncompressedSize64 > uint64(lim.MaxRatio)*max(f.CompressedSize64, 1) {
		return "", fmt.Errorf("%w: %q сжат в %d раз и более", ErrArchiveLimit, f.Name, lim.MaxRatio)
	}
	return rel, nil
}

// openGeoZip открывает архив и проверяет все записи до чтения содержимого.
func openGeoZip(archive []byte, lim GeoLimits) (map[string]*zip.File, error) {
	zr, err := zip.NewReader(bytes.NewReader(archive), int64(len(archive)))
	if err != nil {
		return nil, fmt.Errorf("%w: архив не читается: %v", ErrArchiveEntry, err)
	}
	if len(zr.File) > lim.MaxEntries {
		return nil, fmt.Errorf("%w: %d записей, предел %d", ErrArchiveLimit, len(zr.File), lim.MaxEntries)
	}
	var total uint64
	byRel := make(map[string]*zip.File, len(zr.File))
	seen := make(map[string]bool, len(zr.File))
	for _, f := range zr.File {
		rel, err := vetGeoEntry(f, lim)
		if err != nil {
			return nil, err
		}
		key := strings.ToLower(rel)
		if seen[key] {
			return nil, fmt.Errorf("%w: %q повторяется (имена без учёта регистра)", ErrArchiveEntry, f.Name)
		}
		seen[key] = true
		if total += f.UncompressedSize64; total > uint64(lim.MaxTotalBytes) {
			return nil, fmt.Errorf("%w: суммарно больше %d байт", ErrArchiveLimit, lim.MaxTotalBytes)
		}
		byRel[rel] = f
	}
	return byRel, nil
}

// readGeoEntry читает запись целиком; фактический размер обязан совпасть с заголовком.
func readGeoEntry(f *zip.File) ([]byte, error) {
	rc, err := f.Open()
	if err != nil {
		return nil, fmt.Errorf("%w: %q: %v", ErrArchiveEntry, f.Name, err)
	}
	defer rc.Close()
	want := int64(f.UncompressedSize64)
	raw, err := io.ReadAll(io.LimitReader(rc, want+1))
	if err != nil {
		return nil, fmt.Errorf("%w: %q: %v", ErrArchiveEntry, f.Name, err)
	}
	if int64(len(raw)) != want {
		return nil, fmt.Errorf("%w: %q: размер в заголовке %d, фактический другой", ErrArchiveLimit, f.Name, want)
	}
	return raw, nil
}

// extractGeoZip раскладывает проверенный архив в dst (новый пустой каталог) и
// возвращает пути записанных файлов. Каждый файл обязан быть корректным .srs.
func extractGeoZip(archive []byte, dst string, lim GeoLimits) ([]string, error) {
	byRel, err := openGeoZip(archive, lim)
	if err != nil {
		return nil, err
	}
	rels := make([]string, 0, len(byRel))
	for rel := range byRel {
		rels = append(rels, rel)
	}
	sort.Strings(rels)
	var total int64
	for _, rel := range rels {
		raw, err := readGeoEntry(byRel[rel])
		if err != nil {
			return nil, err
		}
		// Лимит суммарного объёма действует и на размер после распаковки .srs
		// (вложенный zlib-поток): бомба в Store-записи не разворачивается.
		n, err := presets.InflatedSRSSize(raw, min(lim.MaxTotalBytes-total, presets.MaxSRSInflated))
		if errors.Is(err, presets.ErrSRSTooLarge) {
			return nil, fmt.Errorf("%w: %q разжимается больше доступных %d байт", ErrArchiveLimit, rel, lim.MaxTotalBytes-total)
		}
		if err != nil {
			return nil, fmt.Errorf("%w: %q", ErrGeoInvalid, rel)
		}
		total += n
		if err := putGeoFile(dst, rel, raw); err != nil {
			return nil, err
		}
	}
	return rels, nil
}

func putGeoFile(dst, rel string, raw []byte) error {
	if !filepath.IsLocal(filepath.FromSlash(rel)) {
		return fmt.Errorf("%w: %q", ErrArchiveEntry, rel)
	}
	path := filepath.Join(dst, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	return writeFileSynced(path, raw)
}
