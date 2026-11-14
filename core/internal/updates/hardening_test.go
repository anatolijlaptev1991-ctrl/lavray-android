// SPDX-License-Identifier: GPL-3.0-only

package updates

// hardening_test.go — правки по ревью безопасности §7.5: ограничение
// распаковки внутри .srs, устаревший source.json, откат гео-набора назад по
// ревизии, символы glob в пути данных, утечка путей в ошибках, потолок ревизии
// и реалистичные лимиты.

import (
	"archive/zip"
	"bytes"
	"compress/zlib"
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/anatolijlaptev1991-ctrl/lavray-android/core/internal/presets"
)

// srsBomb — .srs, чей zlib-поток разжимается в mib МиБ нулей (сжатый размер — десятки КиБ).
func srsBomb(mib int) []byte {
	var zb bytes.Buffer
	zw, _ := zlib.NewWriterLevel(&zb, zlib.BestCompression)
	chunk := make([]byte, 1<<20)
	for i := 0; i < mib; i++ {
		_, _ = zw.Write(chunk)
	}
	_ = zw.Close()
	return append([]byte{'S', 'R', 'S', 1}, zb.Bytes()...)
}

// zipStore — архив с одной записью методом Store (коэффициент сжатия на уровне zip равен 1).
func zipStore(t *testing.T, name string, data []byte) []byte {
	t.Helper()
	var buf bytes.Buffer
	w := zip.NewWriter(&buf)
	fw, err := w.CreateHeader(&zip.FileHeader{Name: name, Method: zip.Store})
	if err != nil {
		t.Fatal(err)
	}
	_, _ = fw.Write(data)
	_ = w.Close()
	return buf.Bytes()
}

// S: .srs в Store-записи, разжимающийся в гигабайты, не должен ни распаковываться, ни зависать.
func TestGeoApplyRejectsInflationBomb(t *testing.T) {
	g := newGeoFx(t)
	zb := zipStore(t, "geosite/bomb.srs", srsBomb(80)) // 80 МиБ > предела суммарной распаковки
	start := time.Now()
	_, err := ApplyGeo(g.dir, zb, sectionOf(zb), DefaultGeoLimits, nil)
	if !errors.Is(err, ErrArchiveLimit) {
		t.Fatalf("ожидалась ErrArchiveLimit: %v", err)
	}
	if time.Since(start) > 10*time.Second {
		t.Fatalf("отказ занял %v: распаковка не ограничена", time.Since(start))
	}
	g.untouched()
}

// Разжатые байты .srs считаются в общий лимит: много «безобидных» файлов тоже упираются в него.
func TestGeoInflatedBytesCountAgainstTotal(t *testing.T) {
	g := newGeoFx(t)
	zb := zipOf(t, zEntry{Name: "geosite/a.srs", Data: srsBomb(3)}, zEntry{Name: "geosite/b.srs", Data: srsBomb(3)})
	lim := DefaultGeoLimits
	lim.MaxTotalBytes = 5 << 20
	if _, err := ApplyGeo(g.dir, zb, sectionOf(zb), lim, nil); !errors.Is(err, ErrArchiveLimit) {
		t.Fatalf("суммарно 6 МиБ разжатых при пределе 5: %v", err)
	}
	lim.MaxTotalBytes = 7 << 20
	if _, err := ApplyGeo(g.dir, zb, sectionOf(zb), lim, nil); err != nil {
		t.Fatalf("в пределах лимита: %v", err)
	}
}

// Устаревший source.json: после того как EnsureGeo нового бинарника переписал набор,
// установленным он больше не считается — обновление снова предлагается.
func TestStaleSourceJSONAfterEnsureGeoRewrite(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "geo")
	if _, err := presets.ExtractGeo(dir); err != nil {
		t.Fatal(err)
	}
	files, _ := presets.EmbeddedGeoFiles()
	var es []zEntry
	for _, rel := range files {
		es = append(es, zEntry{Name: rel, Data: srs(t, "geosite/google.srs")})
	}
	zb := zipOf(t, es...)
	sec := sectionOf(zb)
	if _, err := ApplyGeo(dir, zb, sec, DefaultGeoLimits, files); err != nil {
		t.Fatal(err)
	}
	if InstalledGeo(dir).Sha256 != sec.Sha256 {
		t.Fatal("после применения набор установлен")
	}
	// Новый бинарник: его встроенная ревизия не ниже маркера → EnsureGeo переписывает файлы.
	if err := os.WriteFile(filepath.Join(dir, presets.GeoMarkerFile), []byte("1 2026-01-01T00:00:00Z\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if n, err := presets.ExtractGeo(dir); err != nil || n == 0 {
		t.Fatalf("ExtractGeo должен переписать файлы: %d %v", n, err)
	}
	if got := InstalledGeo(dir); got.Sha256 != "" {
		t.Fatalf("source.json устарел, но набор считается установленным: %+v", got)
	}
	if PendingGeo(&Manifest{Geo: &sec}, InstalledGeo(dir)) == nil {
		t.Fatal("обновление должно предлагаться снова")
	}
}

// S: гео-набор не откатывается назад по ревизии, равный по sha ревизией не нужен.
func TestGeoDowngradeRefused(t *testing.T) {
	g := newGeoFx(t)
	newer := goodZip(t, "newer")
	older := goodZip(t, "older")
	if _, err := g.apply(newer, sectionRev(newer, 5)); err != nil {
		t.Fatal(err)
	}
	if got := InstalledGeo(g.dir); got.Rev != 5 {
		t.Fatalf("ревизия установленного набора: %+v", got)
	}
	secOld := sectionRev(older, 3)
	if _, err := g.apply(older, secOld); !errors.Is(err, ErrGeoNotNewer) {
		t.Fatalf("откат на старую ревизию: %v", err)
	}
	if _, err := g.apply(older, sectionRev(older, 5)); !errors.Is(err, ErrGeoNotNewer) {
		t.Fatalf("та же ревизия с другим содержимым: %v", err)
	}
	if !exists(filepath.Join(g.dir, "geosite", "newer.srs")) {
		t.Fatal("действующий набор не изменился")
	}
	mf := &Manifest{Geo: &secOld}
	if PendingGeo(mf, InstalledGeo(g.dir)) != nil {
		t.Fatal("старая ревизия не предлагается")
	}
	secNew := sectionRev(older, 6)
	if PendingGeo(&Manifest{Geo: &secNew}, InstalledGeo(g.dir)) == nil {
		t.Fatal("более новая ревизия предлагается")
	}
	// Тот же архив повторно — не ошибка и не смена набора.
	if res, err := g.apply(newer, sectionRev(newer, 5)); err != nil || res.Changed {
		t.Fatalf("повтор: %+v %v", res, err)
	}
}

// Ревизии в манифесте: geo.rev обязателен и ограничен, rev набора ограничен.
func TestManifestRevisionBounds(t *testing.T) {
	base := map[string]any{"type": "manifest", "product": "p", "channel": "stable", "serial": 1,
		"expires": time.Now().Add(time.Hour).UTC().Format(time.RFC3339), "mandatory": false}
	sha := strings.Repeat("a", 64)
	parse := func(extra map[string]any) error {
		doc := map[string]any{}
		for k, v := range base {
			doc[k] = v
		}
		for k, v := range extra {
			doc[k] = v
		}
		raw, _ := json.Marshal(doc)
		_, err := ParseManifest(raw)
		return err
	}
	geo := func(rev any) map[string]any {
		g := map[string]any{"path": "geo/a.zip", "sha256": sha, "length": 10}
		if rev != nil {
			g["rev"] = rev
		}
		return map[string]any{"geo": g}
	}
	if err := parse(geo(1)); err != nil {
		t.Fatalf("корректный geo: %v", err)
	}
	for name, rev := range map[string]any{"missing": nil, "zero": 0, "negative": -1, "huge": int64(1) << 62} {
		if err := parse(geo(rev)); err == nil {
			t.Errorf("geo.rev %s должен отвергаться", name)
		}
	}
	preset := func(rev int64) map[string]any {
		return map[string]any{"presets": map[string]any{"lav-ru": map[string]any{"rev": rev, "path": "p/a.json", "sha256": sha}}}
	}
	if err := parse(preset(presets.MaxRevision)); err != nil {
		t.Fatalf("rev на потолке: %v", err)
	}
	if err := parse(preset(presets.MaxRevision + 1)); err == nil {
		t.Fatal("rev выше потолка должен отвергаться: один подписанный rev навсегда закрыл бы набор")
	}
}

// Файл набора с ревизией выше потолка отвергается при проверке.
func TestPresetRevisionCapped(t *testing.T) {
	fx := newPresetFx(t)
	huge := presets.MaxRevision + 1
	pf, entry := fx.file("lav-ru", huge, "full:x.example")
	if _, err := ApplyPresets(fx.dir, manifestOf(map[string]PresetEntry{"lav-ru": entry}), []PresetFile{pf}); !errors.Is(err, ErrPresetRejected) {
		t.Fatalf("ревизия выше потолка: %v", err)
	}
}

// Остатки замены убираются и тогда, когда в пути каталога данных есть символы glob.
func TestCleanSwapLeftoversWithGlobMetachars(t *testing.T) {
	for _, name := range []string{"user[1]", "user[", "a?b*c", "x]"} {
		root := filepath.Join(t.TempDir(), name)
		cur := filepath.Join(root, "geo")
		left := []string{cur + ".new-123", cur + ".discard", cur + ".rbk"}
		for _, l := range left {
			if err := os.MkdirAll(l, 0o700); err != nil {
				t.Skipf("имя %q недопустимо в этой ОС: %v", name, err)
			}
		}
		if err := os.MkdirAll(cur, 0o700); err != nil {
			t.Fatal(err)
		}
		CleanSwapLeftovers(cur)
		for _, l := range left {
			if exists(l) {
				t.Errorf("%q: остаток %s не убран", name, filepath.Base(l))
			}
		}
		if !exists(cur) {
			t.Errorf("%q: активный каталог удалён", name)
		}
	}
}

// Ошибки замены не несут абсолютных путей: они уходят по RPC. Подробности — отдельно, для журнала.
func TestSwapDirErrorsCarryNoPaths(t *testing.T) {
	cur, next := newTree(t)
	secret := filepath.Join(filepath.Dir(cur), "SECRET-user-dir")
	orig := renameDir
	renameDir = func(a, b string) error {
		return &fs.PathError{Op: "rename", Path: secret, Err: errors.New("Access is denied")}
	}
	defer func() { renameDir = orig }()
	err := SwapDir(cur, next)
	if !errors.Is(err, ErrDirBusy) {
		t.Fatalf("ожидалась ErrDirBusy: %v", err)
	}
	if strings.Contains(err.Error(), "SECRET") || strings.Contains(err.Error(), cur) {
		t.Fatalf("путь утёк в сообщение ошибки: %q", err.Error())
	}
	if d := ErrorDetail(err); !strings.Contains(d, "SECRET-user-dir") || !strings.Contains(d, "Access is denied") {
		t.Fatalf("подробности для журнала: %q", d)
	}
}

// Неудачный возврат состояния после сбоя замены не теряется: он входит в ошибку (и в журнал).
func TestSwapDirReportsUndoFailure(t *testing.T) {
	cur, next := newTree(t)
	orig, calls := renameDir, 0
	renameDir = func(a, b string) error {
		calls++
		if calls >= 2 { // cur→prev прошло (1), дальше next→cur и возврат prev→cur падают
			return errors.New("busy-" + filepath.Base(a))
		}
		return orig(a, b)
	}
	defer func() { renameDir = orig }()
	err := SwapDir(cur, next)
	d := ErrorDetail(err)
	if !errors.Is(err, ErrDirBusy) || !strings.Contains(d, "busy-"+filepath.Base(next)) || !strings.Contains(d, "busy-"+filepath.Base(PrevDir(cur))) {
		t.Fatalf("возврат состояния не отражён: %v / %q", err, d)
	}
}

// То же, когда есть и прошлая ревизия: сбой её возврата из .discard тоже попадает в подробности.
func TestSwapDirReportsUndoOfPreviousFailure(t *testing.T) {
	cur, next := newTree(t)
	writeTree(t, PrevDir(cur), map[string]string{"a.txt": "older"})
	orig, calls := renameDir, 0
	renameDir = func(a, b string) error {
		calls++
		if calls >= 3 { // prev→discard (1) и cur→prev (2) прошли; next→cur, возвраты падают
			return errors.New("busy-" + filepath.Base(a))
		}
		return orig(a, b)
	}
	defer func() { renameDir = orig }()
	d := ErrorDetail(SwapDir(cur, next))
	for _, want := range []string{"busy-" + filepath.Base(next), "busy-" + filepath.Base(PrevDir(cur)), "busy-" + filepath.Base(cur+discardSuffix)} {
		if !strings.Contains(d, want) {
			t.Fatalf("в подробностях нет %q: %q", want, d)
		}
	}
}

// После отката набор дополняется файлами, ставшими встроенными позже (ExtractGeo чинит
// недостающее и повреждённое); целые файлы прошлой ревизии остаются.
func TestRollbackGeoRepairsMissingEmbeddedFiles(t *testing.T) {
	g := newGeoFx(t) // в прошлой ревизии один файл, остальных встроенных нет
	zb := goodZip(t, "extra")
	if _, err := g.apply(zb, sectionOf(zb)); err != nil {
		t.Fatal(err)
	}
	if err := RollbackGeo(g.dir); err != nil {
		t.Fatal(err)
	}
	files, _ := presets.EmbeddedGeoFiles()
	for _, rel := range files {
		if !exists(filepath.Join(g.dir, filepath.FromSlash(rel))) {
			t.Fatalf("после отката нет встроенного файла %s", rel)
		}
	}
	if readFile(t, filepath.Join(g.dir, "geosite", "openai.srs")) != string(curOpenAI(t)) {
		t.Fatal("целый файл прошлой ревизии должен сохраниться")
	}
}

// Лимиты распаковки соответствуют настоящим наборам (меньше мегабайта), а не сотням мегабайт.
func TestDefaultGeoLimitsAreRealistic(t *testing.T) {
	if MaxGeoArchiveBytes > 16<<20 || DefaultGeoLimits.MaxFileBytes > 8<<20 || DefaultGeoLimits.MaxTotalBytes > 32<<20 {
		t.Fatalf("лимиты слишком велики: архив %d, файл %d, всего %d", MaxGeoArchiveBytes, DefaultGeoLimits.MaxFileBytes, DefaultGeoLimits.MaxTotalBytes)
	}
	files, _ := presets.EmbeddedGeoFiles()
	var total int
	for _, rel := range files {
		total += len(srs(t, rel))
	}
	if total*4 > int(DefaultGeoLimits.MaxTotalBytes) {
		t.Fatalf("встроенный набор (%d байт) занимает слишком большую долю лимита", total)
	}
}
