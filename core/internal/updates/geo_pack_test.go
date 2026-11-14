// SPDX-License-Identifier: GPL-3.0-only

package updates

import (
	"archive/zip"
	"bytes"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"testing"
	"time"

	"github.com/anatolijlaptev1991-ctrl/lavray-android/core/internal/presets"
)

// geoSrcDir раскладывает во временный каталог указанные встроенные файлы набора.
func geoSrcDir(t *testing.T, rels ...string) string {
	t.Helper()
	dir := t.TempDir()
	for _, rel := range rels {
		p := filepath.Join(dir, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, srs(t, rel), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func zipEntries(t *testing.T, raw []byte) []*zip.File {
	t.Helper()
	zr, err := zip.NewReader(bytes.NewReader(raw), int64(len(raw)))
	if err != nil {
		t.Fatal(err)
	}
	return zr.File
}

// Упаковка детерминирована: порядок записей, время и атрибуты не зависят от каталога.
func TestPackGeoZipIsDeterministic(t *testing.T) {
	rels := []string{"geosite/openai.srs", "geoip/cn.srs", "geosite/google.srs"}
	a, err := PackGeoZip(geoSrcDir(t, rels...))
	if err != nil {
		t.Fatal(err)
	}
	// Другой каталог, другой порядок создания, другие mtime файлов.
	dir := geoSrcDir(t, rels[2], rels[0], rels[1])
	old := time.Date(2001, 2, 3, 4, 5, 6, 0, time.UTC)
	for _, rel := range rels {
		if err := os.Chtimes(filepath.Join(dir, filepath.FromSlash(rel)), old, old); err != nil {
			t.Fatal(err)
		}
	}
	b, err := PackGeoZip(dir)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(a, b) {
		t.Fatal("одинаковое содержимое должно давать побайтно одинаковый архив")
	}
	var names []string
	for _, f := range zipEntries(t, a) {
		names = append(names, f.Name)
		if len(f.Extra) != 0 || f.Method != zip.Store || f.Comment != "" || f.Flags&0x1 != 0 {
			t.Errorf("%s: лишние атрибуты: extra=%d method=%d comment=%q", f.Name, len(f.Extra), f.Method, f.Comment)
		}
		if got := f.Modified.UTC().Format("2006-01-02 15:04:05"); got != "1980-01-01 00:00:00" {
			t.Errorf("%s: время %s вместо фиксированного", f.Name, got)
		}
		if !f.Mode().IsRegular() {
			t.Errorf("%s: не обычный файл", f.Name)
		}
	}
	if !sort.StringsAreSorted(names) || len(names) != 3 {
		t.Fatalf("записи не отсортированы: %v", names)
	}
}

// Результат проходит ту же проверку, что и клиент при применении.
func TestPackedZipPassesClientValidation(t *testing.T) {
	files, err := presets.EmbeddedGeoFiles()
	if err != nil {
		t.Fatal(err)
	}
	zb, err := PackGeoZip(geoSrcDir(t, files...))
	if err != nil {
		t.Fatal(err)
	}
	g := newGeoFx(t)
	if res, err := ApplyGeo(g.dir, zb, sectionOf(zb), DefaultGeoLimits, files); err != nil || !res.Changed {
		t.Fatalf("клиент не принял упакованный набор: %+v %v", res, err)
	}
}

func TestPackGeoZipRefusesForeignContent(t *testing.T) {
	good := []string{"geoip/cn.srs"}
	cases := map[string]func(dir string){
		"non_srs_file":   func(dir string) { writeTree(t, dir, map[string]string{"geosite/README.md": "x"}) },
		"top_level_file": func(dir string) { writeTree(t, dir, map[string]string{"revision.txt": "2 x\n"}) },
		"top_level_srs":  func(dir string) { _ = os.WriteFile(filepath.Join(dir, "cn.srs"), srs(t, "geoip/cn.srs"), 0o600) },
		"unknown_dir":    func(dir string) { writeTree(t, dir, map[string]string{"other/x.srs": "x"}) },
		"nested_dir":     func(dir string) { writeTree(t, dir, map[string]string{"geosite/sub/x.srs": "x"}) },
		"bad_name": func(dir string) {
			_ = os.WriteFile(filepath.Join(dir, "geosite", "a b.srs"), srs(t, "geoip/cn.srs"), 0o600)
		},
		"broken_srs": func(dir string) { writeTree(t, dir, map[string]string{"geosite/broken.srs": "not an srs"}) },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			dir := geoSrcDir(t, good...)
			if err := os.MkdirAll(filepath.Join(dir, "geosite"), 0o700); err != nil {
				t.Fatal(err)
			}
			mutate(dir)
			if _, err := PackGeoZip(dir); !errors.Is(err, ErrArchiveEntry) && !errors.Is(err, ErrGeoInvalid) {
				t.Fatalf("ожидался отказ: %v", err)
			}
		})
	}
}

func TestPackGeoZipRefusesSymlink(t *testing.T) {
	dir := geoSrcDir(t, "geoip/cn.srs", "geosite/openai.srs")
	if err := os.Symlink(filepath.Join(dir, "geoip", "cn.srs"), filepath.Join(dir, "geosite", "link.srs")); err != nil {
		t.Skipf("символические ссылки недоступны на этой системе: %v", err)
	}
	if _, err := PackGeoZip(dir); !errors.Is(err, ErrArchiveEntry) {
		t.Fatalf("ссылка в наборе: %v", err)
	}
	// И каталог-ссылка вместо geoip/ тоже недопустим.
	dir2 := geoSrcDir(t, "geosite/openai.srs")
	if err := os.Symlink(dir, filepath.Join(dir2, "geoip")); err != nil {
		t.Skipf("символические ссылки недоступны: %v", err)
	}
	if _, err := PackGeoZip(dir2); !errors.Is(err, ErrArchiveEntry) {
		t.Fatalf("ссылка-каталог: %v", err)
	}
}

func TestPackGeoZipEmptyAndMissingDir(t *testing.T) {
	if _, err := PackGeoZip(t.TempDir()); !errors.Is(err, ErrGeoIncomplete) {
		t.Fatalf("пустой каталог: %v", err)
	}
	if _, err := PackGeoZip(filepath.Join(t.TempDir(), "none")); err == nil {
		t.Fatal("нет каталога")
	}
	file := filepath.Join(t.TempDir(), "f")
	_ = os.WriteFile(file, nil, 0o600)
	if _, err := PackGeoZip(file); err == nil {
		t.Fatal("вместо каталога файл")
	}
}

func TestPackGeoZipHonoursEntryLimit(t *testing.T) {
	old := DefaultGeoLimits
	defer func() { DefaultGeoLimits = old }()
	DefaultGeoLimits.MaxEntries = 1
	if _, err := PackGeoZip(geoSrcDir(t, "geoip/cn.srs", "geoip/ru.srs")); !errors.Is(err, ErrArchiveLimit) {
		t.Fatalf("лимит записей: %v", err)
	}
}

// fakeEntry — запись обхода заданного типа: ссылки и устройства проверяются
// независимо от прав на создание ссылок в системе.
type fakeEntry struct {
	typ fs.FileMode
}

func (f fakeEntry) Name() string               { return "x" }
func (f fakeEntry) IsDir() bool                { return f.typ.IsDir() }
func (f fakeEntry) Type() fs.FileMode          { return f.typ }
func (f fakeEntry) Info() (fs.FileInfo, error) { return nil, errors.New("не нужен") }

func TestCheckGeoEntryRefusesNonRegular(t *testing.T) {
	for name, typ := range map[string]fs.FileMode{
		"symlink": fs.ModeSymlink, "pipe": fs.ModeNamedPipe, "socket": fs.ModeSocket, "device": fs.ModeDevice,
	} {
		if err := checkGeoEntry("geosite/x.srs", fakeEntry{typ}); !errors.Is(err, ErrArchiveEntry) {
			t.Errorf("%s: %v", name, err)
		}
	}
	if err := checkGeoEntry("geosite/x.srs", fakeEntry{0}); err != nil {
		t.Errorf("обычный файл: %v", err)
	}
	if err := checkGeoEntry("geoip", fakeEntry{fs.ModeDir}); err != nil {
		t.Errorf("каталог geoip: %v", err)
	}
	if err := checkGeoEntry("geoip", fakeEntry{fs.ModeDir | fs.ModeSymlink}); !errors.Is(err, ErrArchiveEntry) {
		t.Errorf("ссылка вместо каталога geoip: %v", err)
	}
}
