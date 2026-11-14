// SPDX-License-Identifier: GPL-3.0-only

package updates

import (
	"archive/zip"
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/anatolijlaptev1991-ctrl/lavray-android/core/internal/presets"
)

// zEntry — запись тестового архива.
type zEntry struct {
	Name string
	Data []byte
	Mode os.FileMode // 0 — обычный файл
}

func zipOf(t *testing.T, entries ...zEntry) []byte {
	t.Helper()
	var buf bytes.Buffer
	w := zip.NewWriter(&buf)
	for _, e := range entries {
		h := &zip.FileHeader{Name: e.Name, Method: zip.Deflate}
		if e.Mode != 0 {
			h.SetMode(e.Mode)
		}
		fw, err := w.CreateHeader(h)
		if err != nil {
			t.Fatal(err)
		}
		if strings.HasSuffix(e.Name, "/") {
			continue // запись-каталог данных не несёт
		}
		if _, err := fw.Write(e.Data); err != nil {
			t.Fatal(err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func srs(t *testing.T, rel string) []byte {
	t.Helper()
	b, err := presets.GeoFile(rel)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func sectionOf(archive []byte) GeoSection { return sectionRev(archive, 1) }

// sectionRev — запись geo манифеста для archive с ревизией rev.
func sectionRev(archive []byte, rev int64) GeoSection {
	sum := sha256.Sum256(archive)
	return GeoSection{Path: "geo/set.zip", Sha256: hex.EncodeToString(sum[:]), Length: int64(len(archive)), Rev: rev}
}

// curOpenAI — «действующий» файл набора в фикстуре: корректный .srs, не совпадающий со встроенным.
func curOpenAI(t *testing.T) []byte { return srs(t, "geosite/google.srs") }

var geoRequired = []string{"geosite/openai.srs", "geoip/cn.srs"}

// goodZip — полный набор плюс файл extra (признак ревизии).
func goodZip(t *testing.T, extra string) []byte {
	t.Helper()
	es := []zEntry{{Name: "geosite/openai.srs", Data: srs(t, "geosite/openai.srs")}, {Name: "geoip/cn.srs", Data: srs(t, "geoip/cn.srs")}}
	if extra != "" {
		es = append(es, zEntry{Name: "geosite/" + extra + ".srs", Data: srs(t, "geosite/google.srs")})
	}
	return zipOf(t, es...)
}

// geoFx — каталог geo с «текущей» ревизией внутри временного каталога данных.
type geoFx struct {
	t    *testing.T
	root string
	dir  string
}

func newGeoFx(t *testing.T) *geoFx {
	t.Helper()
	root := t.TempDir()
	dir := filepath.Join(root, "geo")
	writeTree(t, dir, map[string]string{"geosite/openai.srs": string(curOpenAI(t)), "revision.txt": "2 2026-01-01T00:00:00Z\n"})
	return &geoFx{t: t, root: root, dir: dir}
}

func (g *geoFx) apply(archive []byte, sec GeoSection) (*GeoApplied, error) {
	return ApplyGeo(g.dir, archive, sec, DefaultGeoLimits, geoRequired)
}

// rootNames — что лежит рядом с geo: после отказа только сам каталог.
func (g *geoFx) rootNames() []string {
	es, err := os.ReadDir(g.root)
	if err != nil {
		g.t.Fatal(err)
	}
	var out []string
	for _, e := range es {
		out = append(out, e.Name())
	}
	sort.Strings(out)
	return out
}

func (g *geoFx) untouched() {
	g.t.Helper()
	if readFile(g.t, filepath.Join(g.dir, "geosite", "openai.srs")) != string(curOpenAI(g.t)) {
		g.t.Fatal("действующий каталог geo изменился")
	}
	if names := g.rootNames(); len(names) != 1 || names[0] != "geo" {
		g.t.Fatalf("рядом с geo остались лишние файлы: %v", names)
	}
}

func TestGeoAppliesArchive(t *testing.T) {
	g := newGeoFx(t)
	zb := goodZip(t, "extra")
	res, err := g.apply(zb, sectionOf(zb))
	if err != nil || !res.Changed {
		t.Fatalf("ApplyGeo: %+v %v", res, err)
	}
	if !exists(filepath.Join(g.dir, "geosite", "extra.srs")) || !exists(filepath.Join(g.dir, "geoip", "cn.srs")) {
		t.Fatal("файлы набора на месте")
	}
	if readFile(t, filepath.Join(PrevDir(g.dir), "geosite", "openai.srs")) != string(curOpenAI(t)) {
		t.Fatal("прошлая ревизия хранится в geo.prev")
	}
	if rev := presets.GeoRevisionOf(g.dir); rev <= presets.GeoRevision {
		t.Fatalf("ревизия в маркере %d должна быть выше встроенной %d", rev, presets.GeoRevision)
	}
	if got := InstalledGeo(g.dir); got.Sha256 != sectionOf(zb).Sha256 {
		t.Fatalf("установленный набор: %+v", got)
	}
}

// Набор, положенный апдейтером, переживает EnsureGeo: цельные файлы не затираются встроенными.
func TestGeoAppliedSetSurvivesEnsureGeo(t *testing.T) {
	root := t.TempDir()
	if err := presets.EnsureGeo(root); err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(root, "geo")
	files, _ := presets.EmbeddedGeoFiles()
	var es []zEntry
	for _, rel := range files {
		es = append(es, zEntry{Name: rel, Data: srs(t, rel)})
	}
	es = append(es, zEntry{Name: "geosite/new-only.srs", Data: srs(t, "geosite/google.srs")})
	zb := zipOf(t, es...)
	if _, err := ApplyGeo(dir, zb, sectionOf(zb), DefaultGeoLimits, files); err != nil {
		t.Fatal(err)
	}
	if err := presets.EnsureGeo(root); err != nil {
		t.Fatal(err)
	}
	if !exists(filepath.Join(dir, "geosite", "new-only.srs")) {
		t.Fatal("EnsureGeo не должен удалять файлы набора канала GEO")
	}
}

func TestGeoApplyIsIdempotent(t *testing.T) {
	g := newGeoFx(t)
	zb := goodZip(t, "extra")
	if _, err := g.apply(zb, sectionOf(zb)); err != nil {
		t.Fatal(err)
	}
	res, err := g.apply(zb, sectionOf(zb))
	if err != nil || res.Changed {
		t.Fatalf("повторное применение того же архива не меняет набор: %+v %v", res, err)
	}
	if readFile(t, filepath.Join(PrevDir(g.dir), "geosite", "openai.srs")) != string(curOpenAI(t)) {
		t.Fatal("повтор не должен затирать прошлую ревизию")
	}
}

// S: sha256 архива не совпал с манифестом — ничего не распаковано.
func TestGeoShaMismatch(t *testing.T) {
	g := newGeoFx(t)
	zb := goodZip(t, "extra")
	sec := sectionOf(zb)
	sec.Sha256 = strings.Repeat("0", 64)
	if _, err := g.apply(zb, sec); !errors.Is(err, ErrHashMismatch) {
		t.Fatalf("ожидалась ErrHashMismatch: %v", err)
	}
	g.untouched()
	tampered := append([]byte(nil), zb...)
	tampered[len(tampered)/2] ^= 0xff
	sec = sectionOf(zb)
	if _, err := g.apply(tampered, sec); !errors.Is(err, ErrHashMismatch) {
		t.Fatalf("подмена байта: %v", err)
	}
	g.untouched()
}

func TestGeoLengthMismatch(t *testing.T) {
	g := newGeoFx(t)
	zb := goodZip(t, "")
	sec := sectionOf(zb)
	sec.Length++
	if _, err := g.apply(zb, sec); !errors.Is(err, ErrLengthMismatch) {
		t.Fatalf("длина: %v", err)
	}
	g.untouched()
}

// S: пути за пределами набора, ссылки и прочие недопустимые имена записей.
func TestGeoZipSlipRejected(t *testing.T) {
	srsData := srs(t, "geosite/openai.srs")
	names := []string{
		"../evil.srs", "geosite/../../evil.srs", "geoip/../geosite/x.srs", "/abs.srs", "/geosite/x.srs",
		"C:/evil.srs", `C:\evil.srs`, `geosite\x.srs`, `..\evil.srs`, "geosite/sub/x.srs", "x.srs", "geosite/x.txt",
		"geosite/.srs", "geosite/x.srs/", "geosite/", "other/x.srs", "geosite/CON.srs", "geosite/nul.txt.srs",
		"geosite/a b.srs", "geosite/x\x00.srs", "geosite/x\n.srs", "geosite/..srs", "geosite/a..b.srs", "./geosite/x.srs",
		"geosite//x.srs", "geosite/x.srs::$DATA", "~/x.srs",
	}
	for _, name := range names {
		t.Run(strings.ReplaceAll(name, "/", "_"), func(t *testing.T) {
			g := newGeoFx(t)
			zb := zipOf(t, zEntry{Name: "geoip/cn.srs", Data: srs(t, "geoip/cn.srs")}, zEntry{Name: name, Data: srsData})
			_, err := g.apply(zb, sectionOf(zb))
			if !errors.Is(err, ErrArchiveEntry) {
				t.Fatalf("имя %q: ожидалась ErrArchiveEntry, получено %v", name, err)
			}
			g.untouched()
			if exists(filepath.Join(filepath.Dir(g.root), "evil.srs")) || exists(filepath.Join(g.root, "evil.srs")) {
				t.Fatal("файл записан за пределами набора")
			}
		})
	}
}

func TestGeoCaseInsensitiveDuplicatesRejected(t *testing.T) {
	g := newGeoFx(t)
	zb := zipOf(t, zEntry{Name: "geosite/A.srs", Data: srs(t, "geosite/google.srs")}, zEntry{Name: "geosite/a.srs", Data: srs(t, "geosite/google.srs")})
	if _, err := g.apply(zb, sectionOf(zb)); !errors.Is(err, ErrArchiveEntry) {
		t.Fatalf("дубликаты без учёта регистра: %v", err)
	}
	g.untouched()
}

// S: символическая ссылка и прочие не обычные файлы.
func TestGeoSymlinkEntryRejected(t *testing.T) {
	for name, mode := range map[string]os.FileMode{
		"symlink": os.ModeSymlink | 0o777, "device": os.ModeDevice | 0o600, "pipe": os.ModeNamedPipe | 0o600,
		"socket": os.ModeSocket | 0o600, "chardev": os.ModeCharDevice | os.ModeDevice | 0o600, "dir_mode": os.ModeDir | 0o755,
	} {
		t.Run(name, func(t *testing.T) {
			g := newGeoFx(t)
			zb := zipOf(t, zEntry{Name: "geosite/link.srs", Data: []byte("/etc/passwd"), Mode: mode})
			if _, err := g.apply(zb, sectionOf(zb)); !errors.Is(err, ErrArchiveEntry) {
				t.Fatalf("%s: %v", name, err)
			}
			g.untouched()
		})
	}
}

// S: лимиты распаковки (zip-бомба).
func TestGeoZipBombCaps(t *testing.T) {
	good := func(name string) zEntry { return zEntry{Name: name, Data: srs(t, "geosite/openai.srs")} }
	cases := map[string]struct {
		zb  []byte
		lim func(*GeoLimits)
	}{
		"entry_count": {zipOf(t, good("geosite/a.srs"), good("geosite/b.srs"), good("geosite/c.srs")), func(l *GeoLimits) { l.MaxEntries = 2 }},
		"file_size":   {zipOf(t, good("geosite/a.srs")), func(l *GeoLimits) { l.MaxFileBytes = 8 }},
		"total_size":  {zipOf(t, good("geosite/a.srs"), good("geosite/b.srs")), func(l *GeoLimits) { l.MaxTotalBytes = int64(len(srs(t, "geosite/openai.srs"))) + 1 }},
		"ratio":       {zipOf(t, zEntry{Name: "geosite/zero.srs", Data: make([]byte, 5<<20)}), nil},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			g := newGeoFx(t)
			lim := DefaultGeoLimits
			if c.lim != nil {
				c.lim(&lim)
			}
			_, err := ApplyGeo(g.dir, c.zb, sectionOf(c.zb), lim, nil)
			if !errors.Is(err, ErrArchiveLimit) {
				t.Fatalf("ожидалась ErrArchiveLimit: %v", err)
			}
			g.untouched()
		})
	}
}

// S: заголовок занижает размер — распаковка не читает лишнего и отказывает.
func TestGeoLyingHeaderRejected(t *testing.T) {
	g := newGeoFx(t)
	zb := zipOf(t, zEntry{Name: "geosite/openai.srs", Data: srs(t, "geosite/openai.srs")}, zEntry{Name: "geoip/cn.srs", Data: srs(t, "geoip/cn.srs")})
	patched := append([]byte(nil), zb...)
	i := bytes.Index(patched, []byte("PK\x01\x02"))
	binary.LittleEndian.PutUint32(patched[i+24:], 5)
	if _, err := g.apply(patched, sectionOf(patched)); err == nil {
		t.Fatal("архив с неверным размером в заголовке должен быть отвергнут")
	}
	g.untouched()
}

func TestGeoEncryptedAndCorruptRejected(t *testing.T) {
	g := newGeoFx(t)
	zb := goodZip(t, "")
	enc := append([]byte(nil), zb...)
	i := bytes.Index(enc, []byte("PK\x01\x02"))
	enc[i+8] |= 0x01 // флаг шифрования в центральном каталоге
	if _, err := g.apply(enc, sectionOf(enc)); !errors.Is(err, ErrArchiveEntry) {
		t.Fatalf("зашифрованная запись: %v", err)
	}
	trunc := zb[:len(zb)/2]
	if _, err := g.apply(trunc, sectionOf(trunc)); err == nil {
		t.Fatal("усечённый архив")
	}
	junk := []byte("not a zip at all")
	if _, err := g.apply(junk, sectionOf(junk)); err == nil {
		t.Fatal("не архив")
	}
	g.untouched()
}

func TestGeoIncompleteAndInvalidSRSRejected(t *testing.T) {
	g := newGeoFx(t)
	partial := zipOf(t, zEntry{Name: "geosite/openai.srs", Data: srs(t, "geosite/openai.srs")})
	if _, err := g.apply(partial, sectionOf(partial)); !errors.Is(err, ErrGeoIncomplete) {
		t.Fatalf("неполный набор: %v", err)
	}
	bad := zipOf(t, zEntry{Name: "geosite/openai.srs", Data: srs(t, "geosite/openai.srs")}, zEntry{Name: "geoip/cn.srs", Data: []byte("garbage, not srs")})
	if _, err := g.apply(bad, sectionOf(bad)); !errors.Is(err, ErrGeoInvalid) {
		t.Fatalf("негодный .srs: %v", err)
	}
	empty := zipOf(t)
	if _, err := g.apply(empty, sectionOf(empty)); !errors.Is(err, ErrGeoIncomplete) {
		t.Fatalf("пустой архив: %v", err)
	}
	g.untouched()
}

// R: сбой замены каталога возвращает прежнюю ревизию на место и убирает временный каталог.
func TestGeoSwapFailureRollsBack(t *testing.T) {
	g := newGeoFx(t)
	zb := goodZip(t, "extra")
	failRenameOn(t, 2) // 1 — geo→geo.prev, 2 — новый→geo (падает)
	if _, err := g.apply(zb, sectionOf(zb)); !errors.Is(err, ErrDirBusy) {
		t.Fatalf("ожидалась ErrDirBusy: %v", err)
	}
	g.untouched()
}

// R: откат возвращает прошлую ревизию; повторный откат — ErrNoPrevious.
func TestGeoRollbackPrevious(t *testing.T) {
	g := newGeoFx(t)
	zb := goodZip(t, "extra")
	if _, err := g.apply(zb, sectionOf(zb)); err != nil {
		t.Fatal(err)
	}
	if err := RollbackGeo(g.dir); err != nil {
		t.Fatal(err)
	}
	if readFile(t, filepath.Join(g.dir, "geosite", "openai.srs")) != string(curOpenAI(t)) || exists(filepath.Join(g.dir, "geosite", "extra.srs")) {
		t.Fatal("после отката действует прошлая ревизия")
	}
	if got := InstalledGeo(g.dir); got.Sha256 != "" {
		t.Fatalf("установленный набор после отката: %+v", got)
	}
	if err := RollbackGeo(g.dir); !errors.Is(err, ErrNoPrevious) {
		t.Fatalf("повторный откат: %v", err)
	}
}

func TestGeoRollbackWithoutPreviousIsNoPrevious(t *testing.T) {
	g := newGeoFx(t)
	if err := RollbackGeo(g.dir); !errors.Is(err, ErrNoPrevious) {
		t.Fatalf("откат без прошлой ревизии: %v", err)
	}
	g.untouched()
}

func TestPendingGeoAndPreviousInfo(t *testing.T) {
	g := newGeoFx(t)
	zb := goodZip(t, "extra")
	sec := sectionOf(zb)
	mf := &Manifest{Geo: &sec}
	if got := PendingGeo(mf, InstalledGeo(g.dir)); got == nil || got.Sha256 != sec.Sha256 {
		t.Fatalf("новый набор предлагается: %+v", got)
	}
	if _, err := g.apply(zb, sec); err != nil {
		t.Fatal(err)
	}
	if PendingGeo(mf, InstalledGeo(g.dir)) != nil || PendingGeo(&Manifest{}, GeoInstalled{}) != nil || PendingGeo(nil, GeoInstalled{}) != nil {
		t.Fatal("установленный или отсутствующий набор не предлагается")
	}
	prev, ok := GeoPrevious(g.dir)
	if !ok || prev.Sha256 != "" {
		t.Fatalf("прошлая ревизия — встроенный набор: %+v %v", prev, ok)
	}
	if _, ok := GeoPrevious(filepath.Join(g.root, "none")); ok {
		t.Fatal("без .prev прошлой ревизии нет")
	}
}
