// SPDX-License-Identifier: GPL-3.0-only

// geo_pack.go — сборка гео-архива для публикации (updsign geo-pack, T-75/7.5).
// Архив детерминирован: записи отсортированы, метод Store (уже сжатые .srs не
// сжимаются, а вывод не зависит от версии Go), время записей фиксировано
// (1980-01-01 00:00:00), дополнительных полей, комментариев и атрибутов нет.
// Принимается только то, что примет клиент (geo_zip.go): geoip/*.srs и
// geosite/*.srs, корректные .srs; ссылки, каталоги, прочие файлы — отказ.
package updates

import (
	"archive/zip"
	"bytes"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/anatolijlaptev1991-ctrl/lavray-android/core/internal/presets"
)

// dosEpochDate — 1980-01-01 в формате даты MS-DOS (ГГГГГГГММММДДДДД: год−1980, месяц, день).
const dosEpochDate = 1<<5 | 1

var packDirs = map[string]bool{"geoip": true, "geosite": true}

// PackGeoZip собирает детерминированный zip из каталога dir с подкаталогами
// geoip/ и geosite/. Ошибки: ErrArchiveEntry — чужое содержимое или ссылка,
// ErrGeoInvalid — файл не .srs, ErrArchiveLimit — лимиты клиента, ErrGeoIncomplete — пусто.
func PackGeoZip(dir string) ([]byte, error) {
	if fi, err := os.Lstat(dir); err != nil {
		return nil, err
	} else if !fi.IsDir() {
		return nil, fmt.Errorf("%w: %q — не каталог (или ссылка на каталог)", ErrArchiveEntry, dir)
	}
	names, err := collectGeoFiles(dir)
	if err != nil {
		return nil, err
	}
	if len(names) == 0 {
		return nil, fmt.Errorf("%w: в %q нет файлов geoip/*.srs и geosite/*.srs", ErrGeoIncomplete, dir)
	}
	if len(names) > DefaultGeoLimits.MaxEntries {
		return nil, fmt.Errorf("%w: %d файлов, предел %d", ErrArchiveLimit, len(names), DefaultGeoLimits.MaxEntries)
	}
	raw, err := writeGeoZip(dir, names)
	if err != nil {
		return nil, err
	}
	if _, err := openGeoZip(raw, DefaultGeoLimits); err != nil { // то, что примет клиент
		return nil, err
	}
	return raw, nil
}

// checkGeoEntry проверяет запись обхода (rel не ".") по её типу, не следуя ссылкам.
func checkGeoEntry(rel string, d fs.DirEntry) error {
	switch {
	case d.Type()&fs.ModeSymlink != 0:
		return fmt.Errorf("%w: %q — символическая ссылка", ErrArchiveEntry, rel)
	case d.IsDir():
		if !packDirs[rel] {
			return fmt.Errorf("%w: каталог %q недопустим (нужны только geoip/ и geosite/)", ErrArchiveEntry, rel)
		}
		return nil
	case !d.Type().IsRegular():
		return fmt.Errorf("%w: %q — не обычный файл", ErrArchiveEntry, rel)
	}
	_, err := geoEntryName(rel)
	return err
}

// collectGeoFiles обходит каталог без перехода по ссылкам и проверяет каждую запись.
func collectGeoFiles(root string) ([]string, error) {
	var names []string
	seen := map[string]bool{}
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(root, p)
		rel = filepath.ToSlash(rel)
		if rel == "." {
			return nil
		}
		if err := checkGeoEntry(rel, d); err != nil || d.IsDir() {
			return err
		}
		key := strings.ToLower(rel)
		if seen[key] {
			return fmt.Errorf("%w: %q повторяется (имена без учёта регистра)", ErrArchiveEntry, rel)
		}
		seen[key] = true
		names = append(names, rel)
		return nil
	})
	sort.Strings(names)
	return names, err
}

// writeGeoZip пишет архив: фиксированные заголовки, записи по порядку names.
func writeGeoZip(root string, names []string) ([]byte, error) {
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	var total int64
	for _, name := range names {
		data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(name)))
		if err != nil {
			return nil, err
		}
		if int64(len(data)) > DefaultGeoLimits.MaxFileBytes {
			return nil, fmt.Errorf("%w: %q больше %d байт", ErrArchiveLimit, name, DefaultGeoLimits.MaxFileBytes)
		}
		if total += int64(len(data)); total > DefaultGeoLimits.MaxTotalBytes {
			return nil, fmt.Errorf("%w: суммарно больше %d байт", ErrArchiveLimit, DefaultGeoLimits.MaxTotalBytes)
		}
		if !presets.ValidSRS(data) {
			return nil, fmt.Errorf("%w: %q", ErrGeoInvalid, name)
		}
		// Modified остаётся нулевым: иначе писатель добавит поле «extended timestamp».
		w, err := zw.CreateHeader(&zip.FileHeader{Name: name, Method: zip.Store, ModifiedDate: dosEpochDate})
		if err != nil {
			return nil, err
		}
		if _, err := w.Write(data); err != nil {
			return nil, err
		}
	}
	if err := zw.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}
