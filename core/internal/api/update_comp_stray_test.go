// SPDX-License-Identifier: GPL-3.0-only

package api

// update_comp_stray_test.go — отклонённый набор пресетов не оставляет в staging
// файлов, которые эта попытка уже успела положить (D-7b), и не трогает чужие.

import (
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"

	lavrayv1 "github.com/anatolijlaptev1991-ctrl/lavray-android/core/internal/gen/lavrayv1"
	"github.com/anatolijlaptev1991-ctrl/lavray-android/core/internal/updates/updatestest"
)

// twoPresetPack — два набора с ревизиями выше встроенных; lav-cn идёт в загрузке
// раньше lav-ru (порядок по id).
func twoPresetPack(t *testing.T, bump int, marker string) (map[string]updatestest.PresetArtifact, int) {
	t.Helper()
	cnRev, ruRev := builtinRev(t, "lav-cn")+bump, builtinRev(t, "lav-ru")+bump
	return map[string]updatestest.PresetArtifact{
		"lav-cn": presetArtifact(t, "lav-cn", cnRev, marker),
		"lav-ru": presetArtifact(t, "lav-ru", ruRev, marker),
	}, ruRev
}

// compFiles — имена файлов в staging/components.
func compFiles(t *testing.T, f *updFix) []string {
	t.Helper()
	es, err := os.ReadDir(f.srv.compStagingDir())
	if err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	}
	var out []string
	for _, e := range es {
		out = append(out, e.Name())
	}
	return out
}

func tamperMirror(f *updFix, id string, rev int, art updatestest.PresetArtifact) {
	f.src.Put("presets/"+id+"-"+strconv.Itoa(rev)+".json", append([]byte(" "), art.Data...))
}

// Файл набора подменён на зеркале: lav-cn уже скачан и проверен, lav-ru не прошёл.
// Набор отклонён целиком, в staging/components ничего не остаётся.
func TestRejectedPresetPackLeavesNoStagedFiles(t *testing.T) {
	f := newUpdFix(t, "0.5.0", nil)
	pack, ruRev := twoPresetPack(t, 1, "full:upd.example")
	f.releaseData(1, pack, nil)
	tamperMirror(f, "lav-ru", ruRev, pack["lav-ru"])

	evs, err := f.downloadComps(compPresets)
	if err != nil || len(evs) == 0 || evs[len(evs)-1].GetVerifyFailed() == nil ||
		evs[len(evs)-1].GetVerifyFailed().Reason != "sha256_mismatch" {
		t.Fatalf("подмена на зеркале: %v %v", evs, err)
	}
	if st := f.compState(compPresets); st.Phase != lavrayv1.ComponentState_PHASE_FAILED {
		t.Errorf("фаза = %v, want FAILED", st.Phase)
	}
	if left := compFiles(t, f); len(left) != 0 {
		t.Errorf("в staging/components остались файлы отклонённого набора: %v", left)
	}
}

// Попытка не должна удалять файлы прошлого, ещё действующего скачанного набора
// (тот же sha256 и имя — тот же путь), как и посторонние файлы staging.
func TestRejectedPresetPackKeepsOtherStagedFiles(t *testing.T) {
	f := newUpdFix(t, "0.5.0", nil)
	pack, _ := twoPresetPack(t, 1, "full:upd.example")
	f.releaseData(1, pack, nil)
	evs, err := f.downloadComps(compPresets)
	if err != nil {
		t.Fatal(err)
	}
	prev := verifiedFor(evs, compPresets)
	if len(prev) != 2 {
		t.Fatalf("verified первого набора: %+v", prev)
	}
	// Посторонние файлы: установщик приложения в staging и чужой файл в components.
	installer := filepath.Join(f.srv.stagingDir(), strings.Repeat("a", 64)+"-LAVRay-setup.exe")
	foreign := filepath.Join(f.srv.compStagingDir(), strings.Repeat("b", 64)+"-other.json")
	for _, p := range []string{installer, foreign} {
		if err := os.WriteFile(p, []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	// Второй релиз: lav-cn прежний (тот же путь), lav-ru новее и подменён на зеркале.
	pack2, ruRev2 := twoPresetPack(t, 2, "full:upd.example")
	pack2["lav-cn"] = pack["lav-cn"]
	f.releaseData(2, pack2, nil)
	tamperMirror(f, "lav-ru", ruRev2, pack2["lav-ru"])
	evs, err = f.downloadComps(compPresets)
	if err != nil || evs[len(evs)-1].GetVerifyFailed() == nil {
		t.Fatalf("второй набор должен быть отклонён: %v %v", evs, err)
	}
	for _, v := range prev {
		if _, err := os.Stat(v.StagedPath); err != nil {
			t.Errorf("файл прежнего набора удалён: %v", err)
		}
	}
	for _, p := range []string{installer, foreign} {
		if _, err := os.Stat(p); err != nil {
			t.Errorf("посторонний файл удалён: %v", err)
		}
	}
	// В components только файлы прежнего набора и чужой файл: ни новых, ни .part.
	want := []string{filepath.Base(foreign)}
	for _, v := range prev {
		want = append(want, filepath.Base(v.StagedPath))
	}
	got := compFiles(t, f)
	slices.Sort(want)
	slices.Sort(got)
	if !slices.Equal(got, want) {
		t.Errorf("staging/components = %v, want %v", got, want)
	}
}

// Файл набора подменён уже в staging: применение отклоняет набор целиком, ни
// один файл набора не остаётся и ни один набор не применяется.
func TestStagingTamperedPresetPackRemovesAllFiles(t *testing.T) {
	f := newUpdFix(t, "0.5.0", nil)
	pack, _ := twoPresetPack(t, 1, "full:upd.example")
	f.releaseData(1, pack, nil)
	evs, err := f.downloadComps(compPresets)
	if err != nil {
		t.Fatal(err)
	}
	ver := verifiedFor(evs, compPresets)
	if len(ver) != 2 {
		t.Fatalf("verified: %+v", ver)
	}
	raw, err := os.ReadFile(ver[1].StagedPath)
	if err != nil {
		t.Fatal(err)
	}
	raw[len(raw)/2] ^= 0x01
	if err := os.WriteFile(ver[1].StagedPath, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	evs, err = f.applyComps(compPresets)
	if err != nil || len(evs) != 1 || evs[0].GetVerifyFailed() == nil || evs[0].GetVerifyFailed().Reason != "sha256_mismatch" {
		t.Fatalf("подмена в staging: %v %v", evs, err)
	}
	if st := f.compState(compPresets); st.Phase != lavrayv1.ComponentState_PHASE_FAILED {
		t.Errorf("фаза = %v, want FAILED", st.Phase)
	}
	if left := compFiles(t, f); len(left) != 0 {
		t.Errorf("в staging/components остались файлы: %v", left)
	}
	for _, id := range []string{"lav-cn", "lav-ru"} {
		if got := getCustom(t, f, id); int(got.Revision) != builtinRev(t, id) {
			t.Errorf("%s: набор применился (rev %d)", id, got.Revision)
		}
	}
}
