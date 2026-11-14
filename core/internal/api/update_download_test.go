// SPDX-License-Identifier: GPL-3.0-only

package api

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"connectrpc.com/connect"

	lavrayv1 "github.com/anatolijlaptev1991-ctrl/lavray-android/core/internal/gen/lavrayv1"
	"github.com/anatolijlaptev1991-ctrl/lavray-android/core/internal/inst"
	"github.com/anatolijlaptev1991-ctrl/lavray-android/core/internal/updates"
	"github.com/anatolijlaptev1991-ctrl/lavray-android/core/internal/updates/updatestest"
)

func bigInstaller() []byte { return bytes.Repeat([]byte("LAVRAY-INSTALLER-"), 20_000) }

func TestDownloadEventsOrder(t *testing.T) {
	f := newUpdFix(t, "0.5.0", nil)
	f.release(updatestest.Release{Version: "0.6.0", Serial: 1, TSVersion: 1, Data: bigInstaller()})
	evs, err := f.download()
	if err != nil || len(evs) < 2 {
		t.Fatalf("события загрузки: %d %v", len(evs), err)
	}
	if p := evs[0].GetProgress(); p == nil || p.TotalBytes != int64(len(bigInstaller())) || p.DownloadedBytes != 0 {
		t.Fatalf("первым идёт progress с total: %+v", evs[0])
	}
	var last int64
	for _, e := range evs[:len(evs)-1] {
		p := e.GetProgress()
		if p == nil || p.DownloadedBytes < last {
			t.Fatalf("до verified только возрастающий progress: %+v", e)
		}
		last = p.DownloadedBytes
	}
	v := evs[len(evs)-1].GetVerified()
	if v == nil || v.Version != "0.6.0" || len(v.Sha256) != 64 {
		t.Fatalf("последним идёт verified: %+v", evs[len(evs)-1])
	}
	staging := filepath.Join(f.st.Dir(), "updates", "staging")
	if filepath.Dir(v.StagedPath) != staging || filepath.Base(v.StagedPath) != v.Sha256+"-lavray-setup.exe" {
		t.Fatalf("staged_path должен быть внутри staging: %q", v.StagedPath)
	}
	if got, _ := os.ReadFile(v.StagedPath); !bytes.Equal(got, bigInstaller()) {
		t.Fatal("содержимое установщика в staging")
	}
	state, _ := f.cl.GetUpdateState(context.Background(), connect.NewRequest(&lavrayv1.GetUpdateStateRequest{}))
	c := state.Msg.Components[0]
	if c.Phase != lavrayv1.ComponentState_PHASE_DOWNLOADED || c.GetStagedVersion() != "0.6.0" || c.RollbackAvailable {
		t.Fatalf("состояние после загрузки: %+v", c)
	}
}

func TestDownloadWithoutUpdateIsPrecondition(t *testing.T) {
	f := newUpdFix(t, "0.5.0", nil)
	f.release(updatestest.Release{Version: "0.5.0", Serial: 1, TSVersion: 1})
	if _, err := f.download(); connect.CodeOf(err) != connect.CodeFailedPrecondition {
		t.Fatalf("нечего скачивать: %v", err)
	}
	g := newUpdFix(t, "0.5.0", func(c *UpdaterConf) { c.RootJSON = nil })
	if _, err := g.download(); connect.CodeOf(err) != connect.CodeFailedPrecondition {
		t.Fatalf("не настроено: %v", err)
	}
}

func TestConcurrentDownloadAborted(t *testing.T) {
	f := newUpdFix(t, "0.5.0", nil)
	f.release(updatestest.Release{Version: "0.6.0", Serial: 1, TSVersion: 1})
	f.src.Gate = make(chan struct{})
	first := make(chan error, 1)
	go func() { _, err := f.download(); first <- err }()
	select {
	case <-f.src.Opened:
	case <-time.After(5 * time.Second):
		t.Fatal("первая загрузка не дошла до источника")
	}
	if _, err := f.download(); connect.CodeOf(err) != connect.CodeAborted {
		t.Fatalf("вторая загрузка при идущей первой: ожидался ABORTED, получено %v", err)
	}
	close(f.src.Gate)
	if err := <-first; err != nil {
		t.Fatalf("первая загрузка должна завершиться: %v", err)
	}
	if _, err := f.download(); err != nil {
		t.Fatalf("после завершения новая загрузка допустима: %v", err)
	}
}

func TestDownloadVerifyFailedEvent(t *testing.T) {
	f := newUpdFix(t, "0.5.0", nil)
	path := f.release(updatestest.Release{Version: "0.6.0", Serial: 1, TSVersion: 1, Data: bigInstaller()})
	evil := bigInstaller()
	evil[777] ^= 0xff
	f.src.Put(path, evil)
	evs, err := f.download()
	if err != nil {
		t.Fatal(err)
	}
	vf := evs[len(evs)-1].GetVerifyFailed()
	if vf == nil || vf.Reason != "sha256_mismatch" {
		t.Fatalf("подменённый установщик: %+v", evs[len(evs)-1])
	}
	if names := f.stagedFiles(); len(names) != 0 {
		t.Fatalf("после verify_failed в staging ничего нет: %v", names)
	}
}

func TestApplyAppReadyForRestart(t *testing.T) {
	f := newUpdFix(t, "0.5.0", nil)
	f.release(updatestest.Release{Version: "0.6.0", Serial: 1, TSVersion: 1})
	if _, err := f.download(); err != nil {
		t.Fatal(err)
	}
	evs, err := f.apply(false)
	if err != nil || len(evs) != 2 || evs[0].GetVerified() == nil || evs[1].GetReadyForRestart() != "app" {
		t.Fatalf("Apply(APP): %v %v", evs, err)
	}
	if p := evs[0].GetVerified().StagedPath; !strings.HasSuffix(p, "-lavray-setup.exe") {
		t.Fatalf("staged_path в verified: %q", p)
	}
	state, _ := f.cl.GetUpdateState(context.Background(), connect.NewRequest(&lavrayv1.GetUpdateStateRequest{}))
	if state.Msg.Components[0].Phase != lavrayv1.ComponentState_PHASE_READY_FOR_RESTART {
		t.Fatalf("фаза: %v", state.Msg.Components[0].Phase)
	}
}

func TestApplyWithoutDownloadAndOtherComponents(t *testing.T) {
	f := newUpdFix(t, "0.5.0", nil)
	if _, err := f.apply(false); connect.CodeOf(err) != connect.CodeFailedPrecondition {
		t.Fatalf("нечего применять: %v", err)
	}
	for comp, want := range map[lavrayv1.UpdateComponent]connect.Code{
		lavrayv1.UpdateComponent_UPDATE_COMPONENT_CORE:    connect.CodeFailedPrecondition,
		lavrayv1.UpdateComponent_UPDATE_COMPONENT_PRESETS: connect.CodeFailedPrecondition, // T-75/7.5: нужна загрузка
		lavrayv1.UpdateComponent_UPDATE_COMPONENT_GEO:     connect.CodeFailedPrecondition,
		lavrayv1.UpdateComponent_UPDATE_COMPONENT_WEBUI:   connect.CodeUnimplemented,
	} {
		_, err := collectEvents(f.cl.ApplyUpdates(context.Background(), connect.NewRequest(
			&lavrayv1.ApplyUpdatesRequest{Components: []lavrayv1.UpdateComponent{comp}})))
		if connect.CodeOf(err) != want {
			t.Errorf("Apply(%v): %v, ожидался %v", comp, connect.CodeOf(err), want)
		}
	}
}

func TestApplyCoreFailsPrecondition(t *testing.T) {
	f := newUpdFix(t, "0.5.0", nil)
	f.release(updatestest.Release{Version: "0.6.0", Serial: 1, TSVersion: 1})
	if _, err := f.download(); err != nil {
		t.Fatal(err)
	}
	_, err := collectEvents(f.cl.ApplyUpdates(context.Background(), connect.NewRequest(
		&lavrayv1.ApplyUpdatesRequest{Components: []lavrayv1.UpdateComponent{lavrayv1.UpdateComponent_UPDATE_COMPONENT_CORE}})))
	if connect.CodeOf(err) != connect.CodeFailedPrecondition {
		t.Fatalf("CORE: %v", err)
	}
}

func TestApplyActiveSessionNeedsConfirmation(t *testing.T) {
	f := newUpdFix(t, "0.5.0", nil)
	f.release(updatestest.Release{Version: "0.6.0", Serial: 1, TSVersion: 1})
	if _, err := f.download(); err != nil {
		t.Fatal(err)
	}
	cfg := []byte(`{"inbounds":[{"tag":"mixed-in","type":"mixed","listen":"127.0.0.1","listen_port":2080}]}`)
	if err := f.srv.inst.Start(context.Background(), inst.RunInfo{Mode: "system_proxy", ConfigJSON: cfg}); err != nil {
		t.Fatal(err)
	}
	_, err := f.apply(false)
	var ce *connect.Error
	if connect.CodeOf(err) != connect.CodeFailedPrecondition || !asConnect(err, &ce) || !strings.HasPrefix(ce.Message(), ReasonActiveSession+": ") {
		t.Fatalf("активная сессия без подтверждения: %v", err)
	}
	if evs, err := f.apply(true); err != nil || evs[len(evs)-1].GetReadyForRestart() == "" {
		t.Fatalf("с подтверждением: %v %v", evs, err)
	}
}

func TestApplyDetectsTamperedStaging(t *testing.T) {
	f := newUpdFix(t, "0.5.0", nil)
	f.release(updatestest.Release{Version: "0.6.0", Serial: 1, TSVersion: 1, Data: bigInstaller()})
	evs, err := f.download()
	if err != nil {
		t.Fatal(err)
	}
	p := verified(evs).StagedPath
	raw, _ := os.ReadFile(p)
	raw[100] ^= 0xff
	if err := os.WriteFile(p, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	evs, err = f.apply(false)
	if err != nil || len(evs) != 1 || evs[0].GetVerifyFailed() == nil || evs[0].GetVerifyFailed().Reason != "sha256_mismatch" {
		t.Fatalf("подмена в staging должна давать verify_failed без ready_for_restart: %v %v", evs, err)
	}
	if _, err := f.apply(false); connect.CodeOf(err) != connect.CodeFailedPrecondition {
		t.Fatalf("после отказа повторно применять нечего: %v", err)
	}
}

// Рядом с проверенным установщиком лежит его подпись: по ней оболочка перепроверит файл.
func TestDownloadStagesSignatureNextToInstaller(t *testing.T) {
	f := newUpdFix(t, "0.5.0", nil)
	f.release(updatestest.Release{Version: "0.6.0", Serial: 1, TSVersion: 1})
	evs, err := f.download()
	if err != nil {
		t.Fatal(err)
	}
	p := verified(evs).StagedPath
	sig, err := os.ReadFile(p + updates.MinisigSuffix)
	if err != nil || !strings.Contains(string(sig), "trusted comment: lavray-windows;windows-x64;stable;0.6.0;1") {
		t.Fatalf("подпись рядом с установщиком: %v %q", err, sig)
	}
}

// Часы клиента отстали больше чем на сутки: причина clock_skew, а не «просрочено».
func TestCheckClockSkewIsDistinctReason(t *testing.T) {
	f := newUpdFix(t, "0.5.0", nil)
	f.release(updatestest.Release{Version: "0.6.0", Serial: 1, TSVersion: 1, TSExpires: time.Now().Add(9 * 24 * time.Hour)})
	_, err := f.check()
	var ce *connect.Error
	if !asConnect(err, &ce) || ce.Meta().Get(UpdateReasonHeader) != ReasonClockSkew || !strings.HasPrefix(ce.Message(), "clock_skew: ") {
		t.Fatalf("ожидалась причина clock_skew: %v", err)
	}
}
