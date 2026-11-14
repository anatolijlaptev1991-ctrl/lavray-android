// SPDX-License-Identifier: GPL-3.0-only

// geoassets.go — встроенные geo-базы (.srs) для встроенных пресетов.
//
// Зачем embed: без этих файлов ЛЮБОЙ запуск с пресетом падал на старте
// sing-box («open .../geosite/openai.srs: The system cannot find the path»),
// то есть приложение не подключалось вообще. Теперь базы едут внутри
// бинарника и раскладываются в каталог данных при запуске.
//
// Обновление: релизная подсистема (UPDATE-SYSTEM §8, канал GEO) кладёт
// новые .srs в тот же каталог и пишет в revision.txt более высокую ревизию —
// набор с ревизией выше встроенной приоритетнее встроенного, поэтому
// гео-обновления не требуют пересборки.
//
// T-15 (A1-07, B2-24): раньше существующий непустой файл считался исправным
// и не проверялся, а revision.txt только писался. Усечённый сбоем .srs
// ломал каждый Connect с пресетом («invalid sing-box rule-set file») до
// ручного удаления каталога geo, а новая GeoRevision ничего не обновляла.
package presets

import (
	"crypto/sha256"
	"embed"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

//go:embed geo
var geoFS embed.FS

// GeoRevision — ревизия встроенного набора (растёт при обновлении баз).
const GeoRevision = 1

// geoMarker — файл-маркер в каталоге geo: «<ревизия> <время RFC3339>».
const geoMarker = "revision.txt"

// geoEntry — один встроенный файл набора: размер и sha256 считаются из самого
// embed, поэтому манифест не может разойтись с содержимым.
type geoEntry struct {
	rel  string // путь внутри geo через "/", например "geosite/openai.srs"
	size int
	sum  [sha256.Size]byte
}

// geoManifest — список встроенных файлов с размером и sha256 (считается один раз).
var geoManifest = sync.OnceValues(func() ([]geoEntry, error) {
	var entries []geoEntry
	err := fs.WalkDir(geoFS, "geo", func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		raw, err := geoFS.ReadFile(p)
		if err != nil {
			return err
		}
		entries = append(entries, geoEntry{
			rel:  strings.TrimPrefix(p, "geo/"),
			size: len(raw),
			sum:  sha256.Sum256(raw),
		})
		return nil
	})
	return entries, err
})

// ExtractGeo сверяет файлы набора в dir со встроенными и записывает то, что
// отсутствует, пусто, повреждено или устарело. Возвращает число записанных
// файлов. Запись атомарна (временный файл + rename): сбой посреди записи не
// оставляет усечённый .srs.
//
// Правило по ревизии набора, записанной в revision.txt (0 — маркера нет):
//   - ревизия ≤ GeoRevision: каждый файл обязан совпасть со встроенным по
//     размеру и sha256, иначе переписывается (та же ревизия — то же
//     содержимое; ниже — набор устарел);
//   - ревизия > GeoRevision: набор положил канал GEO, он новее встроенного —
//     цельные файлы не трогаем, чиним только отсутствующие и повреждённые.
//
// Маркер пишется последним и не понижает ревизию.
func ExtractGeo(dir string) (int, error) {
	if dir == "" {
		return 0, fmt.Errorf("geo: пустой каталог")
	}
	entries, err := geoManifest()
	if err != nil {
		return 0, fmt.Errorf("geo: встроенный набор: %w", err)
	}
	saved := readGeoRevision(dir)
	written := 0
	for _, e := range entries {
		dst := filepath.Join(dir, filepath.FromSlash(e.rel))
		if geoFileOK(dst, e, saved) {
			continue
		}
		raw, err := geoFS.ReadFile(path.Join("geo", e.rel))
		if err != nil {
			return written, fmt.Errorf("geo: распаковка %s: %w", e.rel, err)
		}
		if err := writeFileAtomic(dst, raw, 0o600); err != nil {
			return written, fmt.Errorf("geo: распаковка %s: %w", e.rel, err)
		}
		written++
	}
	// Маркер ревизии — для диагностики/обновлений; ревизию канала GEO не
	// понижаем, а неизменный маркер не переписываем (лишняя запись на диск при
	// каждом запуске ни к чему).
	if rev := max(GeoRevision, saved); rev != saved {
		marker := fmt.Sprintf("%d %s\n", rev, time.Now().UTC().Format(time.RFC3339))
		if err := writeFileAtomic(filepath.Join(dir, geoMarker), []byte(marker), 0o600); err != nil {
			return written, fmt.Errorf("geo: маркер ревизии: %w", err)
		}
	}
	return written, nil
}

// EnsureGeo проверяет и при необходимости восстанавливает geo-набор в
// <dataDir>/geo. Вызывается при каждом запуске ядра: проверка — чтение
// ~20 файлов общим объёмом ~0,4 МиБ.
func EnsureGeo(dataDir string) error {
	_, err := ExtractGeo(filepath.Join(dataDir, "geo"))
	return err
}

// geoFileOK — файл dst не нуждается в записи.
func geoFileOK(dst string, e geoEntry, savedRevision int) bool {
	raw, err := os.ReadFile(dst)
	if err != nil || len(raw) == 0 {
		return false // нет файла (или не читается) / пуст
	}
	if len(raw) == e.size && sha256.Sum256(raw) == e.sum {
		return true // совпал со встроенным
	}
	// Отличается от встроенного. Оставить можно только файл набора канала
	// GEO (ревизия выше встроенной) и только цельный.
	return savedRevision > GeoRevision && validSRS(raw)
}

// readGeoRevision — ревизия из revision.txt; 0, если маркера нет или он
// нечитаем (набор считается устаревшим и перепроверяется целиком).
func readGeoRevision(dir string) int {
	raw, err := os.ReadFile(filepath.Join(dir, geoMarker))
	if err != nil {
		return 0
	}
	n, err := strconv.Atoi(strings.TrimSpace(strings.SplitN(string(raw), " ", 2)[0]))
	if err != nil || n < 0 {
		return 0
	}
	return n
}

// validSRS — файл похож на целый .srs sing-box: магия "SRS", байт версии и
// zlib-поток, читаемый до конца с проверкой контрольной суммы (усечённый файл
// даёт unexpected EOF). Формат — sing-box common/srs.Read.
func validSRS(raw []byte) bool {
	_, err := InflatedSRSSize(raw, MaxSRSInflated) // разжатие ограничено: проверка идёт при каждом запуске
	return err == nil
}

// writeFileAtomic пишет data во временный файл рядом с dst, сбрасывает его на
// диск и переименовывает поверх dst: читатель видит либо старое, либо новое
// содержимое целиком.
func writeFileAtomic(dst string, data []byte, perm os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(dst), 0o700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(dst), filepath.Base(dst)+".*.tmp")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	cleanup := func() { _ = os.Remove(tmpName) }
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		cleanup()
		return err
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		cleanup()
		return err
	}
	if err := tmp.Close(); err != nil {
		cleanup()
		return err
	}
	if err := os.Chmod(tmpName, perm); err != nil {
		cleanup()
		return err
	}
	if err := os.Rename(tmpName, dst); err != nil {
		cleanup()
		return err
	}
	return nil
}
