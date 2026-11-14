// SPDX-License-Identifier: GPL-3.0-only

package api

// update_comp_geo_test.go — гео-набор через апдейтер: Check → Download → Apply →
// Rollback (T-75/7.5), общие свойства компонентов (конкурентность, состояние).

import (
	"archive/zip"
	"bytes"
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"connectrpc.com/connect"

	lavrayv1 "github.com/anatolijlaptev1991-ctrl/lavray-android/core/internal/gen/lavrayv1"
	"github.com/anatolijlaptev1991-ctrl/lavray-android/core/internal/presets"
	"github.com/anatolijlaptev1991-ctrl/lavray-android/core/internal/updates"
)

// geoFx — fixture с уже раскладанным встроенным набором в <данные>/geo.
func geoFixture(t *testing.T) *updFix {
	t.Helper()
	f := newUpdFix(t, "0.5.0", nil)
	if err := presets.EnsureGeo(f.st.Dir()); err != nil {
		t.Fatal(err)
	}
	return f
}

func TestCheckOffersGeo(t *testing.T) {
	f := geoFixture(t)
	f.releaseData(1, nil, geoArchive(t, "zz-new"))
	chk, err := f.check()
	if err != nil {
		t.Fatal(err)
	}
	o := offerFor(chk, compGeo)
	if o == nil || o.GetAvailableVersion() != "2026-10-20" {
		t.Fatalf("предложение GEO: %+v", o)
	}
}

func TestGeoUpdateEndToEndAndRollback(t *testing.T) {
	f := geoFixture(t)
	f.releaseData(1, nil, geoArchive(t, "zz-new"))
	evs, err := f.downloadComps(compGeo)
	if err != nil {
		t.Fatal(err)
	}
	var progress int
	for _, e := range evs {
		if p := e.GetProgress(); p != nil && p.Component == compGeo {
			progress++
		}
	}
	if progress == 0 || len(verifiedFor(evs, compGeo)) != 1 {
		t.Fatalf("события загрузки гео: %v", evs)
	}
	evs, err = f.applyComps(compGeo)
	if err != nil || !slices.Equal(appliedOf(evs), []string{"geo"}) || len(restartReasons(evs)) != 0 {
		t.Fatalf("Apply(GEO): %v %v", evs, err)
	}
	// GeoDir конфигурации указывает на действующий каталог.
	if got := f.srv.buildSettings(false).GeoDir; got != filepath.ToSlash(f.geoDirPath()) {
		t.Fatalf("GeoDir=%q", got)
	}
	if _, err := os.Stat(filepath.Join(f.geoDirPath(), "geosite", "zz-new.srs")); err != nil {
		t.Fatal("файл набора на месте:", err)
	}
	if st := f.compState(compGeo); !st.RollbackAvailable || st.Phase != lavrayv1.ComponentState_PHASE_IDLE {
		t.Fatalf("состояние GEO: %+v", st)
	}
	if chk, _ := f.check(); offerFor(chk, compGeo) != nil {
		t.Fatal("установленный набор не предлагается снова")
	}
	res, err := f.rollbackComp(compGeo)
	if err != nil || !res.Success {
		t.Fatalf("Rollback(GEO): %+v %v", res, err)
	}
	if _, err := os.Stat(filepath.Join(f.geoDirPath(), "geosite", "zz-new.srs")); err == nil {
		t.Fatal("после отката нового файла нет")
	}
	if _, err := f.rollbackComp(compGeo); connect.CodeOf(err) != connect.CodeFailedPrecondition {
		t.Fatalf("повторный откат: %v", err)
	}
}

func TestGeoApplyFlagsProfileRestartInSession(t *testing.T) {
	f := geoFixture(t)
	f.releaseData(1, nil, geoArchive(t, "zz-new"))
	if _, err := f.downloadComps(compGeo); err != nil {
		t.Fatal(err)
	}
	f.startSession()
	evs, err := f.applyComps(compGeo)
	if err != nil || !slices.Equal(restartReasons(evs), []string{"profile"}) {
		t.Fatalf("при работающей сессии нужен перезапуск профиля: %v %v", evs, err)
	}
	res, err := f.rollbackComp(compGeo)
	if err != nil || !res.NeedsProfileRestart {
		t.Fatalf("откат при сессии: %+v %v", res, err)
	}
}

// S: sha256 архива на зеркале не совпал — verify_failed, staging пуст.
func TestGeoShaMismatchOnDownload(t *testing.T) {
	f := geoFixture(t)
	f.releaseData(1, nil, geoArchive(t, "zz-new"))
	tampered := geoArchive(t, "zz-new")
	tampered[len(tampered)/3] ^= 0xff
	f.src.Put("geo/set.zip", tampered)
	evs, err := f.downloadComps(compGeo)
	if err != nil || evs[len(evs)-1].GetVerifyFailed() == nil || evs[len(evs)-1].GetVerifyFailed().Reason != "sha256_mismatch" {
		t.Fatalf("подмена архива: %v %v", evs, err)
	}
	if _, err := f.applyComps(compGeo); connect.CodeOf(err) != connect.CodeFailedPrecondition {
		t.Fatalf("применять нечего: %v", err)
	}
}

// S + R: архив с zip-slip подписан, но распаковщик его отвергает; каталог geo не тронут.
func TestGeoUnsafeArchiveRejectedOnApply(t *testing.T) {
	f := geoFixture(t)
	var buf bytes.Buffer
	w := zip.NewWriter(&buf)
	raw, _ := presets.GeoFile("geoip/cn.srs")
	for _, name := range []string{"geoip/cn.srs", "../../evil.srs"} {
		fw, _ := w.Create(name)
		_, _ = fw.Write(raw)
	}
	_ = w.Close()
	f.releaseData(1, nil, buf.Bytes())
	if _, err := f.downloadComps(compGeo); err != nil {
		t.Fatal(err)
	}
	before := dirNames(t, filepath.Dir(f.geoDirPath()))
	evs, err := f.applyComps(compGeo)
	if err != nil || len(evs) != 1 || evs[0].GetVerifyFailed() == nil || evs[0].GetVerifyFailed().Reason != "archive_unsafe" {
		t.Fatalf("небезопасный архив: %v %v", evs, err)
	}
	if after := dirNames(t, filepath.Dir(f.geoDirPath())); !slices.Equal(before, after) {
		t.Fatalf("рядом с geo остались следы: %v → %v", before, after)
	}
	if st := f.compState(compGeo); st.Phase != lavrayv1.ComponentState_PHASE_FAILED || st.RollbackAvailable {
		t.Fatalf("состояние после отказа: %+v", st)
	}
}

// R: набор без файлов встроенного — отказ, активный каталог не тронут.
func TestGeoIncompleteArchiveLeavesDirIntact(t *testing.T) {
	f := geoFixture(t)
	var buf bytes.Buffer
	w := zip.NewWriter(&buf)
	fw, _ := w.Create("geoip/cn.srs")
	raw, _ := presets.GeoFile("geoip/cn.srs")
	_, _ = fw.Write(raw)
	_ = w.Close()
	f.releaseData(1, nil, buf.Bytes())
	if _, err := f.downloadComps(compGeo); err != nil {
		t.Fatal(err)
	}
	evs, err := f.applyComps(compGeo)
	if err != nil || evs[0].GetVerifyFailed() == nil || evs[0].GetVerifyFailed().Reason != "geo_incomplete" {
		t.Fatalf("неполный набор: %v %v", evs, err)
	}
	if _, err := os.Stat(filepath.Join(f.geoDirPath(), "geosite", "openai.srs")); err != nil {
		t.Fatal("действующий набор цел:", err)
	}
}

func dirNames(t *testing.T, dir string) []string {
	t.Helper()
	es, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, e := range es {
		out = append(out, e.Name())
	}
	return out
}

func TestApplyComponentsRequireDownload(t *testing.T) {
	f := newUpdFix(t, "0.5.0", nil)
	for _, c := range []lavrayv1.UpdateComponent{compPresets, compGeo} {
		_, err := f.applyComps(c)
		var ce *connect.Error
		if connect.CodeOf(err) != connect.CodeFailedPrecondition || !asConnect(err, &ce) || !strings.HasPrefix(ce.Message(), ReasonNotStaged+": ") {
			t.Errorf("Apply(%v) без загрузки: %v", c, err)
		}
	}
	// WEBUI по-прежнему не реализован.
	_, err := f.applyComps(lavrayv1.UpdateComponent_UPDATE_COMPONENT_WEBUI)
	if connect.CodeOf(err) != connect.CodeUnimplemented {
		t.Errorf("WEBUI: %v", err)
	}
}

// C: пока идёт применение, второе (и откат) получают ABORTED.
func TestConcurrentApplyAborted(t *testing.T) {
	f := geoFixture(t)
	pr, _ := oneLavRu(t, "full:upd.example")
	f.releaseData(1, pr, geoArchive(t, "zz-new"))
	if _, err := f.downloadComps(compPresets, compGeo); err != nil {
		t.Fatal(err)
	}
	entered, release := make(chan struct{}), make(chan struct{})
	f.srv.upd.hookApply = func() { close(entered); <-release }
	first := make(chan error, 1)
	go func() { _, err := f.applyComps(compPresets); first <- err }()
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("первое применение не началось")
	}
	if _, err := f.applyComps(compGeo); connect.CodeOf(err) != connect.CodeAborted {
		t.Fatalf("второе применение при идущем первом: %v", err)
	}
	if _, err := f.rollbackComp(compPresets); connect.CodeOf(err) != connect.CodeAborted {
		t.Fatalf("откат при идущем применении: %v", err)
	}
	f.srv.upd.hookApply = nil
	close(release)
	if err := <-first; err != nil {
		t.Fatalf("первое применение должно завершиться: %v", err)
	}
	if _, err := f.applyComps(compGeo); err != nil {
		t.Fatalf("после завершения применение допустимо: %v", err)
	}
}

func TestStateListsAllComponents(t *testing.T) {
	f := newUpdFix(t, "0.5.0", nil)
	r, err := f.cl.GetUpdateState(context.Background(), connect.NewRequest(&lavrayv1.GetUpdateStateRequest{}))
	if err != nil {
		t.Fatal(err)
	}
	var got []lavrayv1.UpdateComponent
	for _, c := range r.Msg.Components {
		got = append(got, c.Component)
		if c.Component != lavrayv1.UpdateComponent_UPDATE_COMPONENT_APP && (c.RollbackAvailable || c.Phase != lavrayv1.ComponentState_PHASE_IDLE) {
			t.Errorf("%v: чистое состояние ожидалось: %+v", c.Component, c)
		}
	}
	want := []lavrayv1.UpdateComponent{lavrayv1.UpdateComponent_UPDATE_COMPONENT_APP, compPresets, compGeo}
	if !slices.Equal(got, want) {
		t.Fatalf("компоненты: %v", got)
	}
}

// Один запрос — несколько компонентов: что нечего обновлять, пропускается.
func TestDownloadSeveralComponentsSkipsNothingToDo(t *testing.T) {
	f := geoFixture(t)
	f.releaseData(1, nil, geoArchive(t, "zz-new"))
	evs, err := f.downloadComps(lavrayv1.UpdateComponent_UPDATE_COMPONENT_APP, compPresets, compGeo)
	if err != nil || len(verifiedFor(evs, compGeo)) != 1 || len(verifiedFor(evs, compPresets)) != 0 {
		t.Fatalf("загрузка нескольких компонентов: %v %v", evs, err)
	}
	if _, err := f.downloadComps(compPresets); connect.CodeOf(err) != connect.CodeFailedPrecondition {
		t.Fatalf("наборов к загрузке нет: %v", err)
	}
	if _, err := f.downloadComps(lavrayv1.UpdateComponent_UPDATE_COMPONENT_CORE); connect.CodeOf(err) != connect.CodeUnimplemented {
		t.Fatalf("CORE: %v", err)
	}
}

// Архив из updsign geo-pack (updates.PackGeoZip) целиком проходит клиента:
// загрузка, проверка, применение вместе с набором и откат обоих компонентов.
func TestPackedGeoAndPresetsRoundTripViaRPC(t *testing.T) {
	f := geoFixture(t)
	src := t.TempDir()
	files, _ := presets.EmbeddedGeoFiles()
	for _, rel := range append(files, "geosite/zz-packed.srs") {
		from := rel
		if rel == "geosite/zz-packed.srs" {
			from = "geosite/google.srs"
		}
		raw, _ := presets.GeoFile(from)
		p := filepath.Join(src, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, raw, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	zb, err := updates.PackGeoZip(src)
	if err != nil {
		t.Fatal(err)
	}
	pr, rev := oneLavRu(t, "full:packed.example")
	f.releaseData(1, pr, zb)
	chk, err := f.check()
	if err != nil || offerFor(chk, compPresets) == nil || offerFor(chk, compGeo) == nil {
		t.Fatalf("предложения: %v %v", chk, err)
	}
	if _, err := f.downloadComps(compPresets, compGeo); err != nil {
		t.Fatal(err)
	}
	evs, err := f.applyComps(compPresets, compGeo)
	if err != nil || !slices.Equal(appliedOf(evs), []string{"presets", "geo"}) {
		t.Fatalf("применение: %v %v", evs, err)
	}
	if got := getCustom(t, f, "lav-ru"); int(got.Revision) != rev {
		t.Fatalf("lav-ru r%d, ожидалась %d", got.Revision, rev)
	}
	if _, err := os.Stat(filepath.Join(f.geoDirPath(), "geosite", "zz-packed.srs")); err != nil {
		t.Fatal("файл гео-набора:", err)
	}
	for _, c := range []lavrayv1.UpdateComponent{compPresets, compGeo} {
		if res, err := f.rollbackComp(c); err != nil || !res.Success {
			t.Fatalf("откат %v: %+v %v", c, res, err)
		}
	}
	if got := getCustom(t, f, "lav-ru"); int(got.Revision) != rev-1 {
		t.Fatalf("после отката lav-ru r%d", got.Revision)
	}
	if _, err := os.Stat(filepath.Join(f.geoDirPath(), "geosite", "zz-packed.srs")); err == nil {
		t.Fatal("после отката нового файла нет")
	}
	// Подмена архива на источнике после выпуска не проходит.
	tampered := append([]byte(nil), zb...)
	tampered[len(tampered)/2] ^= 0xff
	f.src.Put("geo/set.zip", tampered)
	evs, err = f.downloadComps(compGeo)
	if err != nil || evs[len(evs)-1].GetVerifyFailed() == nil || evs[len(evs)-1].GetVerifyFailed().Reason != "sha256_mismatch" {
		t.Fatalf("подменённый архив: %v %v", evs, err)
	}
}
