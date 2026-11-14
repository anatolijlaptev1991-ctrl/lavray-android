// SPDX-License-Identifier: GPL-3.0-only

package updates

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/anatolijlaptev1991-ctrl/lavray-android/core/internal/presets"
)

// presetFx — каталог переопределений во временном каталоге данных.
type presetFx struct {
	t   *testing.T
	dir string
}

func newPresetFx(t *testing.T) *presetFx {
	t.Helper()
	return &presetFx{t: t, dir: presets.OverrideDir(t.TempDir())}
}

// baseRev — встроенная ревизия набора id.
func baseRev(t *testing.T, id string) int {
	t.Helper()
	list, err := presets.BuiltinPresets()
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range list {
		if p.ID == id {
			return p.Revision
		}
	}
	t.Fatalf("нет набора %q", id)
	return 0
}

// file — файл набора id ревизии rev с доменом-маркером и запись манифеста для него.
func (f *presetFx) file(id string, rev int, marker string) (PresetFile, PresetEntry) {
	f.t.Helper()
	list, _ := presets.BuiltinPresets()
	for _, p := range list {
		if p.ID != id {
			continue
		}
		c := p.Clone()
		c.Revision = rev
		c.Routing.DirectDomain = append(c.Routing.DirectDomain, marker)
		return f.pack(id, c, int64(rev))
	}
	f.t.Fatalf("нет набора %q", id)
	return PresetFile{}, PresetEntry{}
}

func (f *presetFx) pack(id string, p *presets.Preset, manifestRev int64) (PresetFile, PresetEntry) {
	f.t.Helper()
	raw, err := json.Marshal(p)
	if err != nil {
		f.t.Fatal(err)
	}
	return packRaw(id, raw, manifestRev)
}

func packRaw(id string, raw []byte, manifestRev int64) (PresetFile, PresetEntry) {
	sum := sha256.Sum256(raw)
	return PresetFile{ID: id, Data: raw}, PresetEntry{Rev: manifestRev, Path: "presets/" + id + ".json", Sha256: hex.EncodeToString(sum[:])}
}

func manifestOf(entries map[string]PresetEntry) *Manifest { return &Manifest{Presets: entries} }

func (f *presetFx) effective(id string) *presets.Preset {
	f.t.Helper()
	list, err := presets.BuiltinPresetsWith(f.dir)
	if err != nil {
		f.t.Fatal(err)
	}
	for _, p := range list {
		if p.ID == id {
			return p
		}
	}
	f.t.Fatalf("нет набора %q", id)
	return nil
}

func TestPresetUpdateAppliesNewRev(t *testing.T) {
	fx := newPresetFx(t)
	rev := baseRev(t, "lav-ru") + 1
	pf, entry := fx.file("lav-ru", rev, "full:new.example")
	changes, err := ApplyPresets(fx.dir, manifestOf(map[string]PresetEntry{"lav-ru": entry}), []PresetFile{pf})
	if err != nil {
		t.Fatal(err)
	}
	if len(changes) != 1 || changes[0] != (PresetChange{ID: "lav-ru", From: rev - 1, To: rev}) {
		t.Fatalf("изменения: %+v", changes)
	}
	got := fx.effective("lav-ru")
	if got.Revision != rev || !strings.Contains(strings.Join(got.Routing.DirectDomain, ","), "full:new.example") {
		t.Fatalf("новая ревизия не действует: rev=%d", got.Revision)
	}
	if got := readFile(t, filepath.Join(fx.dir, "lav-ru.json")); got != string(pf.Data) {
		t.Fatal("файл переопределения должен совпасть с артефактом побайтно")
	}
	if !isDir(PrevDir(fx.dir)) {
		t.Fatal("прошлая ревизия (встроенная, пустой каталог) сохранена для отката")
	}
}

// S: ревизия не выше установленной отвергается, ничего не меняется.
func TestPresetUpdateRejectsOlderRev(t *testing.T) {
	fx := newPresetFx(t)
	cur := baseRev(t, "lav-ru")
	for _, rev := range []int{cur, cur - 1} {
		pf, entry := fx.file("lav-ru", rev, "full:old.example")
		_, err := ApplyPresets(fx.dir, manifestOf(map[string]PresetEntry{"lav-ru": entry}), []PresetFile{pf})
		if !errors.Is(err, ErrPresetNotNewer) {
			t.Fatalf("rev %d при установленной %d: %v", rev, cur, err)
		}
	}
	if exists(fx.dir) || fx.effective("lav-ru").Revision != cur {
		t.Fatal("после отказа ничего не записано")
	}
	// Уже применённая более новая ревизия не откатывается более старой.
	newer, ne := fx.file("lav-ru", cur+5, "full:newer.example")
	if _, err := ApplyPresets(fx.dir, manifestOf(map[string]PresetEntry{"lav-ru": ne}), []PresetFile{newer}); err != nil {
		t.Fatal(err)
	}
	older, oe := fx.file("lav-ru", cur+2, "full:older.example")
	if _, err := ApplyPresets(fx.dir, manifestOf(map[string]PresetEntry{"lav-ru": oe}), []PresetFile{older}); !errors.Is(err, ErrPresetNotNewer) {
		t.Fatalf("откат на старую ревизию: %v", err)
	}
	if fx.effective("lav-ru").Revision != cur+5 {
		t.Fatal("действующая ревизия не должна измениться")
	}
}

func TestPresetUpdateShaMismatch(t *testing.T) {
	fx := newPresetFx(t)
	pf, entry := fx.file("lav-ru", baseRev(t, "lav-ru")+1, "full:x.example")
	pf.Data = append(pf.Data, ' ') // подмена после подписи манифеста
	_, err := ApplyPresets(fx.dir, manifestOf(map[string]PresetEntry{"lav-ru": entry}), []PresetFile{pf})
	if !errors.Is(err, ErrHashMismatch) || exists(fx.dir) {
		t.Fatalf("sha256: %v", err)
	}
}

// S: id не встроенного набора, чужой id внутри файла, builtin=false, ревизия файла ≠ манифеста.
func TestPresetUpdateRejectsForeignAndMismatched(t *testing.T) {
	fx := newPresetFx(t)
	rev := baseRev(t, "lav-ru") + 1
	base, _ := fx.file("lav-ru", rev, "full:x.example")
	var m map[string]any
	_ = json.Unmarshal(base.Data, &m)
	mutate := func(k string, v any) []byte {
		c := map[string]any{}
		for key, val := range m {
			c[key] = val
		}
		c[k] = v
		b, _ := json.Marshal(c)
		return b
	}
	cases := map[string]struct {
		id  string
		raw []byte
		rev int64
	}{
		"non_builtin_id":     {"custom:evil", mutate("id", "custom:evil"), int64(rev)},
		"id_in_file_differs": {"lav-ru", mutate("id", "global"), int64(rev)},
		"not_builtin_flag":   {"lav-ru", mutate("builtin", false), int64(rev)},
		"rev_differs":        {"lav-ru", base.Data, int64(rev) + 1},
		"invalid_preset":     {"lav-ru", mutate("routing", map[string]any{"def_outbound": "bogus"}), int64(rev)},
		"not_json":           {"lav-ru", []byte("{broken"), int64(rev)},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			pf, entry := packRaw(c.id, c.raw, c.rev)
			_, err := ApplyPresets(fx.dir, manifestOf(map[string]PresetEntry{c.id: entry}), []PresetFile{pf})
			if !errors.Is(err, ErrPresetRejected) || exists(fx.dir) {
				t.Fatalf("ожидался отказ без записи: %v", err)
			}
		})
	}
}

func TestPresetUpdateRejectsMissingFromManifestDuplicatesAndHuge(t *testing.T) {
	fx := newPresetFx(t)
	pf, entry := fx.file("lav-ru", baseRev(t, "lav-ru")+1, "full:x.example")
	if _, err := ApplyPresets(fx.dir, manifestOf(nil), []PresetFile{pf}); !errors.Is(err, ErrPresetRejected) {
		t.Fatalf("набора нет в манифесте: %v", err)
	}
	mf := manifestOf(map[string]PresetEntry{"lav-ru": entry})
	if _, err := ApplyPresets(fx.dir, mf, []PresetFile{pf, pf}); !errors.Is(err, ErrPresetRejected) {
		t.Fatalf("дубликат id: %v", err)
	}
	if _, err := ApplyPresets(fx.dir, mf, nil); !errors.Is(err, ErrPresetRejected) {
		t.Fatalf("пустой список: %v", err)
	}
	huge, he := packRaw("lav-ru", make([]byte, presets.MaxImportBytes+1), 9)
	if _, err := ApplyPresets(fx.dir, manifestOf(map[string]PresetEntry{"lav-ru": he}), []PresetFile{huge}); !errors.Is(err, ErrPresetRejected) {
		t.Fatalf("слишком большой файл: %v", err)
	}
}

// R: плохой набор в пачке — не применяется ни один (пачка атомарна).
func TestPresetUpdateBatchIsAtomic(t *testing.T) {
	fx := newPresetFx(t)
	good, ge := fx.file("lav-ru", baseRev(t, "lav-ru")+1, "full:a.example")
	bad, be := fx.file("lav-cn", baseRev(t, "lav-cn")+1, "full:b.example")
	bad.Data = []byte(`{"id":"lav-cn"`)
	be.Sha256 = packSha(bad.Data)
	mf := manifestOf(map[string]PresetEntry{"lav-ru": ge, "lav-cn": be})
	if _, err := ApplyPresets(fx.dir, mf, []PresetFile{good, bad}); err == nil {
		t.Fatal("ожидался отказ")
	}
	if exists(fx.dir) || fx.effective("lav-ru").Revision != baseRev(t, "lav-ru") {
		t.Fatal("хороший набор из пачки не должен примениться")
	}
}

func packSha(b []byte) string { s := sha256.Sum256(b); return hex.EncodeToString(s[:]) }

// R: сбой замены каталога не оставляет частично записанных наборов.
func TestPresetUpdateSwapFailureLeavesNoPartialState(t *testing.T) {
	fx := newPresetFx(t)
	first, fe := fx.file("lav-ru", baseRev(t, "lav-ru")+1, "full:a.example")
	if _, err := ApplyPresets(fx.dir, manifestOf(map[string]PresetEntry{"lav-ru": fe}), []PresetFile{first}); err != nil {
		t.Fatal(err)
	}
	next, ne := fx.file("lav-ru", baseRev(t, "lav-ru")+2, "full:b.example")
	failRenameOn(t, 3)
	if _, err := ApplyPresets(fx.dir, manifestOf(map[string]PresetEntry{"lav-ru": ne}), []PresetFile{next}); !errors.Is(err, ErrDirBusy) {
		t.Fatalf("ожидалась ErrDirBusy: %v", err)
	}
	if fx.effective("lav-ru").Revision != baseRev(t, "lav-ru")+1 {
		t.Fatal("действует прежняя ревизия")
	}
	left, _ := filepath.Glob(fx.dir + ".new-*")
	if len(left) != 0 {
		t.Fatalf("временный каталог не убран: %v", left)
	}
}

func TestPresetUpdateKeepsOtherOverrides(t *testing.T) {
	fx := newPresetFx(t)
	a, ae := fx.file("lav-ru", baseRev(t, "lav-ru")+1, "full:a.example")
	if _, err := ApplyPresets(fx.dir, manifestOf(map[string]PresetEntry{"lav-ru": ae}), []PresetFile{a}); err != nil {
		t.Fatal(err)
	}
	b, be := fx.file("lav-cn", baseRev(t, "lav-cn")+1, "full:b.example")
	if _, err := ApplyPresets(fx.dir, manifestOf(map[string]PresetEntry{"lav-cn": be}), []PresetFile{b}); err != nil {
		t.Fatal(err)
	}
	if fx.effective("lav-ru").Revision != baseRev(t, "lav-ru")+1 || fx.effective("lav-cn").Revision != baseRev(t, "lav-cn")+1 {
		t.Fatal("вторая пачка не должна терять первую")
	}
}

// R: откат возвращает прошлую ревизию; со встроенной — убирает переопределение.
func TestPresetRollbackToPreviousAndToBuiltin(t *testing.T) {
	fx := newPresetFx(t)
	base := baseRev(t, "lav-ru")
	for i, marker := range []string{"full:one.example", "full:two.example"} {
		pf, entry := fx.file("lav-ru", base+1+i, marker)
		if _, err := ApplyPresets(fx.dir, manifestOf(map[string]PresetEntry{"lav-ru": entry}), []PresetFile{pf}); err != nil {
			t.Fatal(err)
		}
	}
	if err := RollbackPresets(fx.dir); err != nil {
		t.Fatal(err)
	}
	if fx.effective("lav-ru").Revision != base+1 {
		t.Fatalf("после отката должна действовать rev %d", base+1)
	}
	if err := RollbackPresets(fx.dir); !errors.Is(err, ErrNoPrevious) {
		t.Fatalf("один шаг отката: %v", err)
	}
}

func TestPresetRollbackWithoutPreviousIsNoPrevious(t *testing.T) {
	fx := newPresetFx(t)
	if err := RollbackPresets(fx.dir); !errors.Is(err, ErrNoPrevious) {
		t.Fatalf("откат без применений: %v", err)
	}
}

func TestPresetRollbackFirstApplyReturnsToBuiltin(t *testing.T) {
	fx := newPresetFx(t)
	pf, entry := fx.file("lav-ru", baseRev(t, "lav-ru")+1, "full:a.example")
	_, _ = ApplyPresets(fx.dir, manifestOf(map[string]PresetEntry{"lav-ru": entry}), []PresetFile{pf})
	if err := RollbackPresets(fx.dir); err != nil {
		t.Fatal(err)
	}
	if fx.effective("lav-ru").Revision != baseRev(t, "lav-ru") {
		t.Fatal("после отката первого применения действует встроенный набор")
	}
}

func TestPendingPresets(t *testing.T) {
	fx := newPresetFx(t)
	base := baseRev(t, "lav-ru")
	installed, _ := presets.BuiltinPresetsWith(fx.dir)
	mf := manifestOf(map[string]PresetEntry{
		"lav-ru":      {Rev: int64(base) + 1, Path: "p/a.json", Sha256: strings.Repeat("a", 64)},
		"lav-cn":      {Rev: int64(baseRev(t, "lav-cn")), Path: "p/b.json", Sha256: strings.Repeat("a", 64)},
		"custom:evil": {Rev: 99, Path: "p/c.json", Sha256: strings.Repeat("a", 64)},
		"global":      {Rev: int64(baseRev(t, "global")) - 1, Path: "p/d.json", Sha256: strings.Repeat("a", 64)},
	})
	got := PendingPresets(mf, installed)
	if len(got) != 1 || got[0] != (PresetOffer{ID: "lav-ru", Installed: base, Rev: base + 1}) {
		t.Fatalf("доступные наборы: %+v", got)
	}
	if PendingPresets(nil, installed) != nil || PendingPresets(&Manifest{}, installed) != nil {
		t.Fatal("нет секции presets — нет предложений")
	}
}
