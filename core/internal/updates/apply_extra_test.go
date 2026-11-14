// SPDX-License-Identifier: GPL-3.0-only

package updates

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/anatolijlaptev1991-ctrl/lavray-android/core/internal/presets"
)

// Сведения о прошлой ревизии наборов: после двух применений в .prev лежит первая.
func TestPresetsPreviousReportsRevisions(t *testing.T) {
	fx := newPresetFx(t)
	if _, ok := PresetsPrevious(fx.dir); ok {
		t.Fatal("без применений прошлой ревизии нет")
	}
	base := baseRev(t, "lav-ru")
	for i, marker := range []string{"full:a.example", "full:b.example"} {
		pf, entry := fx.file("lav-ru", base+1+i, marker)
		if _, err := ApplyPresets(fx.dir, manifestOf(map[string]PresetEntry{"lav-ru": entry}), []PresetFile{pf}); err != nil {
			t.Fatal(err)
		}
		prev, ok := PresetsPrevious(fx.dir)
		if !ok {
			t.Fatal("после применения прошлая ревизия есть")
		}
		if i == 0 && len(prev) != 0 {
			t.Fatalf("первым применением заменён встроенный набор: %v", prev)
		}
		if i == 1 && prev["lav-ru"] != base+1 {
			t.Fatalf("прошлая ревизия lav-ru: %v", prev)
		}
	}
}

// Каталог переопределений занят обычным файлом: ошибка, временный каталог убран.
func TestApplyPresetsDirBlockedByFile(t *testing.T) {
	fx := newPresetFx(t)
	if err := os.MkdirAll(filepath.Dir(fx.dir), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(fx.dir, []byte("not a dir"), 0o600); err != nil {
		t.Fatal(err)
	}
	pf, entry := fx.file("lav-ru", baseRev(t, "lav-ru")+1, "full:a.example")
	if _, err := ApplyPresets(fx.dir, manifestOf(map[string]PresetEntry{"lav-ru": entry}), []PresetFile{pf}); err == nil {
		t.Fatal("ожидалась ошибка")
	}
	if left, _ := filepath.Glob(fx.dir + ".new-*"); len(left) != 0 {
		t.Fatalf("временный каталог не убран: %v", left)
	}
}

func TestNewSiblingDirParentIsFile(t *testing.T) {
	root := t.TempDir()
	blocker := filepath.Join(root, "file")
	if err := os.WriteFile(blocker, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := NewSiblingDir(filepath.Join(blocker, "geo")); err == nil {
		t.Fatal("родитель — файл: каталог создать нельзя")
	}
}

func TestWriteFileSyncedRefusesOverwrite(t *testing.T) {
	p := filepath.Join(t.TempDir(), "a")
	if err := writeFileSynced(p, []byte("1")); err != nil {
		t.Fatal(err)
	}
	if err := writeFileSynced(p, []byte("2")); err == nil || readFile(t, p) != "1" {
		t.Fatal("существующий файл не перезаписывается (O_EXCL)")
	}
	if err := writeFileSynced(filepath.Join(p, "x"), nil); err == nil {
		t.Fatal("нельзя создать файл внутри файла")
	}
}

func TestInstalledGeoIgnoresCorruptOrHugeMarker(t *testing.T) {
	dir := t.TempDir()
	if got := InstalledGeo(dir); got != (GeoInstalled{}) {
		t.Fatalf("маркера нет: %+v", got)
	}
	writeTree(t, dir, map[string]string{geoSourceFile: "{broken"})
	if got := InstalledGeo(dir); got != (GeoInstalled{}) {
		t.Fatalf("битый маркер: %+v", got)
	}
	writeTree(t, dir, map[string]string{geoSourceFile: `{"sha256":"` + strings.Repeat("a", 5000) + `"}`})
	if got := InstalledGeo(dir); got != (GeoInstalled{}) {
		t.Fatalf("слишком большой маркер: %+v", got)
	}
}

// Пустой архив без списка обязательных файлов — тоже неполный набор.
func TestGeoEmptyArchiveWithoutRequiredList(t *testing.T) {
	g := newGeoFx(t)
	empty := zipOf(t)
	if _, err := ApplyGeo(g.dir, empty, sectionOf(empty), DefaultGeoLimits, nil); !errors.Is(err, ErrGeoIncomplete) {
		t.Fatalf("пустой архив: %v", err)
	}
	g.untouched()
}

// Защита вглубь: даже прошедшее проверку имя не выпускает запись из каталога.
func TestPutGeoFileRefusesNonLocalPath(t *testing.T) {
	for _, rel := range []string{"../x.srs", "/abs/x.srs", "geosite/../../x.srs"} {
		if err := putGeoFile(t.TempDir(), rel, []byte("x")); !errors.Is(err, ErrArchiveEntry) {
			t.Fatalf("%q: %v", rel, err)
		}
	}
}

func TestGeoStoredMethodAndLimitsOnTotals(t *testing.T) {
	g := newGeoFx(t)
	zb := zipOf(t,
		zEntry{Name: "geosite/openai.srs", Data: srs(t, "geosite/openai.srs")},
		zEntry{Name: "geoip/cn.srs", Data: srs(t, "geoip/cn.srs")})
	lim := DefaultGeoLimits
	lim.MaxTotalBytes = 10
	if _, err := ApplyGeo(g.dir, zb, sectionOf(zb), lim, geoRequired); !errors.Is(err, ErrArchiveLimit) {
		t.Fatalf("суммарный предел: %v", err)
	}
	g.untouched()
	files, _ := presets.EmbeddedGeoFiles()
	if len(files) < 15 {
		t.Fatalf("встроенный набор: %v", files)
	}
}

// Пачка из нескольких наборов применяется целиком; изменения отсортированы по id.
func TestApplyPresetsBatchOfTwoSortedByID(t *testing.T) {
	fx := newPresetFx(t)
	ru, re := fx.file("lav-ru", baseRev(t, "lav-ru")+1, "full:a.example")
	cn, ce := fx.file("lav-cn", baseRev(t, "lav-cn")+1, "full:b.example")
	changes, err := ApplyPresets(fx.dir, manifestOf(map[string]PresetEntry{"lav-ru": re, "lav-cn": ce}), []PresetFile{ru, cn})
	if err != nil || len(changes) != 2 || changes[0].ID != "lav-cn" || changes[1].ID != "lav-ru" {
		t.Fatalf("изменения: %+v %v", changes, err)
	}
	if fx.effective("lav-ru").Revision != baseRev(t, "lav-ru")+1 || fx.effective("lav-cn").Revision != baseRev(t, "lav-cn")+1 {
		t.Fatal("обе ревизии действуют")
	}
}

// Родитель каталога переопределений — файл: временный каталог создать нельзя.
func TestApplyPresetsParentBlocked(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "presets"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	dir := presets.OverrideDir(root)
	fx := &presetFx{t: t, dir: dir}
	pf, entry := fx.file("lav-ru", baseRev(t, "lav-ru")+1, "full:a.example")
	if _, err := ApplyPresets(dir, manifestOf(map[string]PresetEntry{"lav-ru": entry}), []PresetFile{pf}); err == nil {
		t.Fatal("ожидалась ошибка")
	}
}
