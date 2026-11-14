// SPDX-License-Identifier: GPL-3.0-only

package api

// update_comp_presets_test.go — пресеты через апдейтер: Check → Download → Apply →
// Rollback (T-75/7.5). Приложение той же версии, что в манифесте: предлагаются
// только наборы.

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"

	"connectrpc.com/connect"
	"google.golang.org/protobuf/proto"

	lavrayv1 "github.com/anatolijlaptev1991-ctrl/lavray-android/core/internal/gen/lavrayv1"
	"github.com/anatolijlaptev1991-ctrl/lavray-android/core/internal/updates/updatestest"
)

func oneLavRu(t *testing.T, marker string) (map[string]updatestest.PresetArtifact, int) {
	t.Helper()
	rev := builtinRev(t, "lav-ru") + 1
	return map[string]updatestest.PresetArtifact{"lav-ru": presetArtifact(t, "lav-ru", rev, marker)}, rev
}

func TestCheckOffersPresetsNotApp(t *testing.T) {
	f := newUpdFix(t, "0.5.0", nil)
	pr, rev := oneLavRu(t, "full:upd.example")
	f.releaseData(1, pr, nil)
	chk, err := f.check()
	if err != nil {
		t.Fatal(err)
	}
	if offerFor(chk, lavrayv1.UpdateComponent_UPDATE_COMPONENT_APP) != nil {
		t.Fatal("версия приложения та же: APP не предлагается")
	}
	o := offerFor(chk, compPresets)
	if o == nil || o.GetAvailableVersion() == "" || !strings.Contains(o.GetAvailableVersion(), "lav-ru") || !strings.Contains(o.CurrentVersion, "lav-ru") {
		t.Fatalf("предложение PRESETS: %+v (rev %d)", o, rev)
	}
	if offerFor(chk, compGeo) != nil {
		t.Fatal("гео в манифесте нет")
	}
	// Запрос только APP наборов не возвращает.
	only, err := f.cl.CheckUpdates(context.Background(), connect.NewRequest(&lavrayv1.CheckUpdatesRequest{
		Components: []lavrayv1.UpdateComponent{lavrayv1.UpdateComponent_UPDATE_COMPONENT_APP}}))
	if err != nil || offerFor(only.Msg, compPresets) != nil {
		t.Fatalf("фильтр components: %v", err)
	}
}

func TestPresetUpdateAppliesNewRevEndToEnd(t *testing.T) {
	f := newUpdFix(t, "0.5.0", nil)
	pr, rev := oneLavRu(t, "full:upd.example")
	f.releaseData(1, pr, nil)
	evs, err := f.downloadComps(compPresets)
	if err != nil {
		t.Fatal(err)
	}
	ver := verifiedFor(evs, compPresets)
	if len(ver) != 1 || ver[0].Sha256 == "" || !strings.HasPrefix(ver[0].StagedPath, filepath.Join(f.st.Dir(), "updates", "staging")) {
		t.Fatalf("verified набора: %+v", ver)
	}
	if st := f.compState(compPresets); st.Phase != lavrayv1.ComponentState_PHASE_DOWNLOADED || st.GetStagedVersion() == "" {
		t.Fatalf("после загрузки: %+v", st)
	}
	evs, err = f.applyComps(compPresets)
	if err != nil || !slices.Equal(appliedOf(evs), []string{"presets"}) {
		t.Fatalf("Apply(PRESETS): %v %v", evs, err)
	}
	got, err := f.srv.GetPreset(context.Background(), connect.NewRequest(&lavrayv1.GetPresetRequest{Id: "lav-ru"}))
	if err != nil || int(got.Msg.Revision) != rev || !slices.Contains(got.Msg.DirectDomain, "full:upd.example") {
		t.Fatalf("lav-ru после обновления: rev=%d %v", got.Msg.GetRevision(), err)
	}
	list, _ := f.srv.ListPresets(context.Background(), connect.NewRequest(&lavrayv1.ListPresetsRequest{}))
	for _, p := range list.Msg.Presets {
		if p.Id == "lav-ru" && int(p.Revision) != rev {
			t.Fatalf("ListPresets не видит переопределение: %d", p.Revision)
		}
	}
	if st := f.compState(compPresets); !st.RollbackAvailable || st.Phase != lavrayv1.ComponentState_PHASE_IDLE {
		t.Fatalf("после применения: %+v", st)
	}
	if chk, _ := f.check(); offerFor(chk, compPresets) != nil {
		t.Fatal("установленная ревизия больше не предлагается")
	}
	// Активный набор собирается уже из новой ревизии.
	if err := f.srv.setActivePreset("lav-ru"); err != nil {
		t.Fatal(err)
	}
	if p, err := f.srv.activePreset(); err != nil || p.Revision != rev {
		t.Fatalf("активный набор: %v rev=%v", err, p)
	}
}

// S: ревизия не выше установленной не предлагается и не скачивается.
func TestPresetUpdateRejectsOlderRev(t *testing.T) {
	f := newUpdFix(t, "0.5.0", nil)
	base := builtinRev(t, "lav-ru")
	f.releaseData(1, map[string]updatestest.PresetArtifact{"lav-ru": presetArtifact(t, "lav-ru", base, "full:same.example")}, nil)
	chk, err := f.check()
	if err != nil || offerFor(chk, compPresets) != nil {
		t.Fatalf("та же ревизия не предлагается: %v", err)
	}
	if _, err := f.downloadComps(compPresets); connect.CodeOf(err) != connect.CodeFailedPrecondition {
		t.Fatalf("нечего скачивать: %v", err)
	}
	// Манифест не может подсунуть id не встроенного набора.
	f.releaseData(2, map[string]updatestest.PresetArtifact{"evil-set": presetArtifact(t, "lav-ru", base+5, "full:evil.example")}, nil)
	chk, err = f.check()
	if err != nil || offerFor(chk, compPresets) != nil {
		t.Fatalf("чужой id не предлагается: %v", err)
	}
}

// D: пользовательские копии не меняются; source_updated выставляется, повторное применение не действует.
func TestPresetUpdateKeepsCustomCopies(t *testing.T) {
	f := newUpdFix(t, "0.5.0", nil)
	ctx := context.Background()
	made, err := f.srv.CreatePreset(ctx, connect.NewRequest(&lavrayv1.CreatePresetRequest{BuiltinSourceId: strPtr("lav-ru")}))
	if err != nil {
		t.Fatal(err)
	}
	edited := proto.Clone(made.Msg).(*lavrayv1.RoutingPreset)
	edited.DirectDomain = append(edited.DirectDomain, "full:mine.example")
	if _, err := f.srv.UpdatePreset(ctx, connect.NewRequest(&lavrayv1.UpdatePresetRequest{Preset: edited, ExpectedRevision: made.Msg.Revision})); err != nil {
		t.Fatal(err)
	}
	copyBefore := getCustom(t, f, made.Msg.Id)
	if copyBefore.SourceUpdated {
		t.Fatal("до обновления источник не менялся")
	}
	pr, rev := oneLavRu(t, "full:upd.example")
	f.releaseData(1, pr, nil)
	if _, err := f.downloadComps(compPresets); err != nil {
		t.Fatal(err)
	}
	if _, err := f.applyComps(compPresets); err != nil {
		t.Fatal(err)
	}
	after := getCustom(t, f, made.Msg.Id)
	if !after.SourceUpdated || int(after.BuiltinSourceRevision) != rev-1 {
		t.Fatalf("source_updated=%v источник r%d", after.SourceUpdated, after.BuiltinSourceRevision)
	}
	after.SourceUpdated = false
	if !proto.Equal(after, copyBefore) {
		t.Fatal("пользовательская копия изменилась при обновлении источника")
	}
	// Повторное применение того же скачанного набора ничего не делает.
	if _, err := f.applyComps(compPresets); connect.CodeOf(err) != connect.CodeFailedPrecondition {
		t.Fatalf("повторное применение: %v", err)
	}
	// «Сбросить к источнику» подтягивает новые правила и гасит признак.
	reset, err := f.srv.ResetBuiltinPreset(ctx, connect.NewRequest(&lavrayv1.ResetBuiltinPresetRequest{Id: made.Msg.Id}))
	if err != nil || reset.Msg.SourceUpdated || int(reset.Msg.BuiltinSourceRevision) != rev || !slices.Contains(reset.Msg.DirectDomain, "full:upd.example") {
		t.Fatalf("сброс к источнику: %+v %v", reset, err)
	}
}

func getCustom(t *testing.T, f *updFix, id string) *lavrayv1.RoutingPreset {
	t.Helper()
	got, err := f.srv.GetPreset(context.Background(), connect.NewRequest(&lavrayv1.GetPresetRequest{Id: id}))
	if err != nil {
		t.Fatal(err)
	}
	return got.Msg
}

func TestCopySourceUpdatedFlag(t *testing.T) {
	f := newUpdFix(t, "0.5.0", nil)
	ctx := context.Background()
	copyRu, _ := f.srv.CreatePreset(ctx, connect.NewRequest(&lavrayv1.CreatePresetRequest{BuiltinSourceId: strPtr("lav-ru")}))
	copyCn, _ := f.srv.CreatePreset(ctx, connect.NewRequest(&lavrayv1.CreatePresetRequest{BuiltinSourceId: strPtr("lav-cn")}))
	pr, _ := oneLavRu(t, "full:upd.example")
	f.releaseData(1, pr, nil)
	if _, err := f.downloadComps(compPresets); err != nil {
		t.Fatal(err)
	}
	if _, err := f.applyComps(compPresets); err != nil {
		t.Fatal(err)
	}
	if !getCustom(t, f, copyRu.Msg.Id).SourceUpdated || getCustom(t, f, copyCn.Msg.Id).SourceUpdated {
		t.Fatal("source_updated только у копии обновлённого источника")
	}
	list, _ := f.srv.ListPresets(ctx, connect.NewRequest(&lavrayv1.ListPresetsRequest{}))
	for _, p := range list.Msg.Presets {
		if p.SourceUpdated != (p.Id == copyRu.Msg.Id) {
			t.Errorf("%s: source_updated=%v", p.Id, p.SourceUpdated)
		}
	}
	// После отката источник снова прежний: признака нет.
	if _, err := f.rollbackComp(compPresets); err != nil {
		t.Fatal(err)
	}
	if getCustom(t, f, copyRu.Msg.Id).SourceUpdated {
		t.Fatal("после отката источник прежний")
	}
}

// S: подмена файла набора на зеркале и в staging.
func TestPresetDownloadAndStagingTamper(t *testing.T) {
	f := newUpdFix(t, "0.5.0", nil)
	pr, _ := oneLavRu(t, "full:upd.example")
	f.releaseData(1, pr, nil)
	art := pr["lav-ru"]
	f.src.Put("presets/lav-ru-"+strconv.Itoa(art.Rev)+".json", append([]byte(" "), art.Data...))
	evs, err := f.downloadComps(compPresets)
	if err != nil || len(evs) == 0 || evs[len(evs)-1].GetVerifyFailed() == nil || evs[len(evs)-1].GetVerifyFailed().Reason != "sha256_mismatch" {
		t.Fatalf("подмена на зеркале: %v %v", evs, err)
	}
	if _, err := f.applyComps(compPresets); connect.CodeOf(err) != connect.CodeFailedPrecondition {
		t.Fatalf("после отказа применять нечего: %v", err)
	}
	// Файл подменили уже в staging.
	f.src.Put("presets/lav-ru-"+strconv.Itoa(art.Rev)+".json", art.Data)
	evs, err = f.downloadComps(compPresets)
	if err != nil {
		t.Fatal(err)
	}
	p := verifiedFor(evs, compPresets)[0].StagedPath
	raw, _ := os.ReadFile(p)
	raw[len(raw)/2] ^= 0x01
	if err := os.WriteFile(p, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	evs, err = f.applyComps(compPresets)
	if err != nil || len(evs) != 1 || evs[0].GetVerifyFailed() == nil || evs[0].GetVerifyFailed().Reason != "sha256_mismatch" {
		t.Fatalf("подмена в staging: %v %v", evs, err)
	}
	if got := getCustom(t, f, "lav-ru"); int(got.Revision) != builtinRev(t, "lav-ru") {
		t.Fatal("подменённый набор не должен примениться")
	}
}

func TestRollbackPresetsRestoresPreviousAndNeedsPrevious(t *testing.T) {
	f := newUpdFix(t, "0.5.0", nil)
	if _, err := f.rollbackComp(compPresets); connect.CodeOf(err) != connect.CodeFailedPrecondition {
		t.Fatalf("откат без применений: %v", err)
	}
	base := builtinRev(t, "lav-ru")
	for i := 1; i <= 2; i++ {
		f.releaseData(int64(i), map[string]updatestest.PresetArtifact{"lav-ru": presetArtifact(t, "lav-ru", base+i, "full:r"+strconv.Itoa(i)+".example")}, nil)
		if _, err := f.downloadComps(compPresets); err != nil {
			t.Fatal(err)
		}
		if _, err := f.applyComps(compPresets); err != nil {
			t.Fatal(err)
		}
	}
	st := f.compState(compPresets)
	if !st.RollbackAvailable || !strings.Contains(st.GetPreviousVersion(), "lav-ru") {
		t.Fatalf("откат доступен: %+v", st)
	}
	res, err := f.rollbackComp(compPresets)
	if err != nil || !res.Success || res.GetRestoredVersion() == "" {
		t.Fatalf("Rollback(PRESETS): %+v %v", res, err)
	}
	if got := getCustom(t, f, "lav-ru"); int(got.Revision) != base+1 {
		t.Fatalf("после отката rev %d, ожидалась %d", got.Revision, base+1)
	}
	if _, err := f.rollbackComp(compPresets); connect.CodeOf(err) != connect.CodeFailedPrecondition {
		t.Fatalf("второй откат подряд: %v", err)
	}
}

// Перезапуск профиля помечается, когда меняется активный набор при работающей сессии.
func TestPresetApplyFlagsProfileRestart(t *testing.T) {
	cases := []struct {
		name    string
		active  string
		session bool
		want    bool
	}{
		{"active_changed_session", "lav-ru", true, true},
		{"active_changed_idle", "lav-ru", false, false},
		{"other_active_session", "global", true, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			f := newUpdFix(t, "0.5.0", nil)
			if err := f.srv.setActivePreset(c.active); err != nil {
				t.Fatal(err)
			}
			pr, _ := oneLavRu(t, "full:upd.example")
			f.releaseData(1, pr, nil)
			if _, err := f.downloadComps(compPresets); err != nil {
				t.Fatal(err)
			}
			if c.session {
				f.startSession()
			}
			evs, err := f.applyComps(compPresets)
			if err != nil {
				t.Fatal(err)
			}
			got := slices.Equal(restartReasons(evs), []string{"profile"})
			if got != c.want {
				t.Fatalf("ready_for_restart=%v, ожидалось %v (%v)", restartReasons(evs), c.want, evs)
			}
			res, err := f.rollbackComp(compPresets)
			if err != nil || res.NeedsProfileRestart != c.want {
				t.Fatalf("откат: needs_profile_restart=%v err=%v, ожидалось %v", res.GetNeedsProfileRestart(), err, c.want)
			}
		})
	}
}
