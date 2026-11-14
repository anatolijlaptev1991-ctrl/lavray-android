// SPDX-License-Identifier: GPL-3.0-only

package presets

import (
	"bytes"
	"compress/zlib"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// T-15 (A1-07, B2-24): geo-набор проверяется по размеру и sha256 при каждом
// запуске, повреждённые и устаревшие файлы переписываются атомарно, набор
// более высокой ревизии (канал GEO) не откатывается. Тесты используют только
// публичный интерфейс (EnsureGeo, ExtractGeo, GeoRevision) и embed geoFS.

type embeddedFile struct {
	rel string // путь внутри geo через "/"
	raw []byte
}

func embeddedFiles(t *testing.T) []embeddedFile {
	t.Helper()
	var files []embeddedFile
	err := fs.WalkDir(geoFS, "geo", func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		raw, err := geoFS.ReadFile(p)
		files = append(files, embeddedFile{rel: strings.TrimPrefix(p, "geo/"), raw: raw})
		return err
	})
	if err != nil || len(files) < 15 {
		t.Fatalf("встроенный набор: %d файлов, err=%v", len(files), err)
	}
	return files
}

func (f embeddedFile) onDisk(dataDir string) string {
	return filepath.Join(dataDir, "geo", filepath.FromSlash(f.rel))
}

func mustEnsure(t *testing.T, dataDir string) {
	t.Helper()
	if err := EnsureGeo(dataDir); err != nil {
		t.Fatalf("EnsureGeo: %v", err)
	}
}

func markerRevision(t *testing.T, dataDir string) int {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(dataDir, "geo", "revision.txt"))
	if err != nil {
		t.Fatalf("revision.txt: %v", err)
	}
	n, err := strconv.Atoi(strings.Fields(string(raw))[0])
	if err != nil {
		t.Fatalf("revision.txt %q: %v", raw, err)
	}
	return n
}

func writeMarker(t *testing.T, dataDir string, revision int) {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(dataDir, "geo"), 0o700); err != nil {
		t.Fatal(err)
	}
	body := fmt.Sprintf("%d 2026-01-01T00:00:00Z\n", revision)
	if err := os.WriteFile(filepath.Join(dataDir, "geo", "revision.txt"), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}

// srsOK — независимая проверка «целый .srs»: магия SRS, версия, zlib-поток
// читается до конца (как sing-box common/srs.Read).
func srsOK(raw []byte) bool {
	if len(raw) < 5 || string(raw[:3]) != "SRS" {
		return false
	}
	zr, err := zlib.NewReader(bytes.NewReader(raw[4:]))
	if err != nil {
		return false
	}
	_, err = io.Copy(io.Discard, zr)
	return err == nil
}

func freshSRS() []byte {
	var b bytes.Buffer
	b.WriteString("SRS\x01")
	zw := zlib.NewWriter(&b)
	_, _ = zw.Write([]byte("newer geo payload"))
	_ = zw.Close()
	return b.Bytes()
}

// Встроенный набор сам целый: на него опирается проверка.
func TestEmbeddedGeoSetIsValid(t *testing.T) {
	for _, f := range embeddedFiles(t) {
		if !srsOK(f.raw) {
			t.Errorf("%s: встроенный файл не похож на целый .srs", f.rel)
		}
	}
}

// A1-07: усечённый файл (сбой записи / ENOSPC при первой распаковке), а
// revision.txt уже на месте — раньше такой файл жил вечно.
func TestEnsureGeoRepairsTruncatedFile(t *testing.T) {
	f := embeddedFiles(t)[0]
	data := t.TempDir()
	mustEnsure(t, data)

	if err := os.WriteFile(f.onDisk(data), []byte("TRUNC"), 0o600); err != nil {
		t.Fatal(err)
	}
	mustEnsure(t, data)

	got, err := os.ReadFile(f.onDisk(data))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, f.raw) {
		t.Fatalf("%s: после EnsureGeo %d байт, ожидался встроенный файл (%d байт)", f.rel, len(got), len(f.raw))
	}
}

// Пустой, отсутствующий и обрезанный наполовину файлы чинятся за один проход,
// число записанных файлов = число повреждённых.
func TestEnsureGeoRepairsEmptyMissingAndHalfWritten(t *testing.T) {
	files := embeddedFiles(t)
	data := t.TempDir()
	mustEnsure(t, data)

	_ = os.WriteFile(files[0].onDisk(data), nil, 0o600)
	_ = os.Remove(files[1].onDisk(data))
	_ = os.WriteFile(files[2].onDisk(data), files[2].raw[:len(files[2].raw)/2], 0o600)

	n, err := ExtractGeo(filepath.Join(data, "geo"))
	if err != nil || n != 3 {
		t.Fatalf("ExtractGeo записал %d файлов, err=%v; ожидалось 3", n, err)
	}
	for _, f := range files[:3] {
		got, _ := os.ReadFile(f.onDisk(data))
		if !bytes.Equal(got, f.raw) {
			t.Errorf("%s не восстановлен", f.rel)
		}
	}
}

// Размер совпал, содержимое другое (бит в файле): ловит только хэш.
func TestEnsureGeoDetectsSameSizeCorruption(t *testing.T) {
	f := embeddedFiles(t)[0]
	data := t.TempDir()
	mustEnsure(t, data)

	flipped := append([]byte(nil), f.raw...)
	flipped[len(flipped)/2] ^= 0xFF
	if err := os.WriteFile(f.onDisk(data), flipped, 0o600); err != nil {
		t.Fatal(err)
	}
	mustEnsure(t, data)

	if got, _ := os.ReadFile(f.onDisk(data)); !bytes.Equal(got, f.raw) {
		t.Fatal("файл с тем же размером, но другим содержимым не восстановлен")
	}
}

// B2-24: ревизия встроенного набора выше записанной — устаревшие файлы
// заменяются, маркер получает новую ревизию.
func TestEnsureGeoOlderRevisionIsReplaced(t *testing.T) {
	f := embeddedFiles(t)[0]
	data := t.TempDir()
	mustEnsure(t, data)
	writeMarker(t, data, GeoRevision-1)
	if err := os.WriteFile(f.onDisk(data), freshSRS(), 0o600); err != nil { // цельный, но чужой
		t.Fatal(err)
	}
	mustEnsure(t, data)

	if got, _ := os.ReadFile(f.onDisk(data)); !bytes.Equal(got, f.raw) {
		t.Fatalf("устаревший файл не заменён: %d байт", len(got))
	}
	if rev := markerRevision(t, data); rev != GeoRevision {
		t.Fatalf("ревизия в маркере = %d, ожидалась %d", rev, GeoRevision)
	}
}

// Набор более высокой ревизии (положен каналом GEO) не откатывается к
// встроенному, но повреждённый файл в нём чинится.
func TestEnsureGeoKeepsNewerSetAndRepairsBrokenFiles(t *testing.T) {
	files := embeddedFiles(t)
	data := t.TempDir()
	mustEnsure(t, data)
	newer := GeoRevision + 5
	writeMarker(t, data, newer)

	fresh := freshSRS()
	if err := os.WriteFile(files[0].onDisk(data), fresh, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(files[1].onDisk(data), []byte("TRUNC"), 0o600); err != nil {
		t.Fatal(err)
	}
	mustEnsure(t, data)

	if got, _ := os.ReadFile(files[0].onDisk(data)); !bytes.Equal(got, fresh) {
		t.Fatal("файл набора более высокой ревизии откатили к встроенному")
	}
	if got, _ := os.ReadFile(files[1].onDisk(data)); !bytes.Equal(got, files[1].raw) {
		t.Fatal("повреждённый файл набора более высокой ревизии не восстановлен")
	}
	if rev := markerRevision(t, data); rev != newer {
		t.Fatalf("ревизия в маркере = %d, ожидалась прежняя %d (не понижать)", rev, newer)
	}
}

// Запись атомарна: после распаковки в каталоге нет временных файлов, а
// исправный набор повторной проверкой не переписывается.
func TestExtractGeoLeavesNoTempFilesAndIsIdempotent(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "geo")
	total := len(embeddedFiles(t))
	n, err := ExtractGeo(dir)
	if err != nil || n != total {
		t.Fatalf("первая распаковка: записано %d из %d, err=%v", n, total, err)
	}
	n, err = ExtractGeo(dir)
	if err != nil || n != 0 {
		t.Fatalf("повторная проверка исправного набора записала %d файлов, err=%v", n, err)
	}
	_ = filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err == nil && !d.IsDir() && strings.HasSuffix(p, ".tmp") {
			t.Errorf("остался временный файл %s", p)
		}
		return nil
	})
}

// Нечитаемый маркер = «ревизия неизвестна»: набор перепроверяется целиком.
func TestEnsureGeoGarbageMarker(t *testing.T) {
	f := embeddedFiles(t)[0]
	data := t.TempDir()
	mustEnsure(t, data)
	if err := os.WriteFile(filepath.Join(data, "geo", "revision.txt"), []byte("мусор"), 0o600); err != nil {
		t.Fatal(err)
	}
	_ = os.WriteFile(f.onDisk(data), []byte("TRUNC"), 0o600)
	mustEnsure(t, data)
	if got, _ := os.ReadFile(f.onDisk(data)); !bytes.Equal(got, f.raw) {
		t.Fatal("файл не восстановлен при нечитаемом маркере")
	}
	if rev := markerRevision(t, data); rev != GeoRevision {
		t.Fatalf("маркер не переписан: ревизия %d", rev)
	}
}
