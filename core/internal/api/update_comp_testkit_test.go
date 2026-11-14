// SPDX-License-Identifier: GPL-3.0-only

package api

// update_comp_testkit_test.go — оснастка тестов доставки пресетов и гео-набора
// через апдейтер (T-75/7.5): релиз с секциями presets/geo, вызовы потоков
// Download/Apply по компонентам, активная сессия.

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"path/filepath"
	"testing"

	"connectrpc.com/connect"

	lavrayv1 "github.com/anatolijlaptev1991-ctrl/lavray-android/core/internal/gen/lavrayv1"
	"github.com/anatolijlaptev1991-ctrl/lavray-android/core/internal/inst"
	"github.com/anatolijlaptev1991-ctrl/lavray-android/core/internal/presets"
	"github.com/anatolijlaptev1991-ctrl/lavray-android/core/internal/updates/updatestest"
)

// builtinRev — встроенная (без переопределений) ревизия набора id.
func builtinRev(t *testing.T, id string) int {
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
	t.Fatalf("нет встроенного набора %q", id)
	return 0
}

// presetArtifact — файл встроенного набора id ревизии rev с доменом-маркером.
func presetArtifact(t *testing.T, id string, rev int, marker string) updatestest.PresetArtifact {
	t.Helper()
	list, _ := presets.BuiltinPresets()
	for _, p := range list {
		if p.ID != id {
			continue
		}
		c := p.Clone()
		c.Revision = rev
		c.Routing.DirectDomain = append(c.Routing.DirectDomain, marker)
		raw, err := json.Marshal(c)
		if err != nil {
			t.Fatal(err)
		}
		return updatestest.PresetArtifact{Rev: rev, Data: raw}
	}
	t.Fatalf("нет встроенного набора %q", id)
	return updatestest.PresetArtifact{}
}

// geoArchive — полный набор встроенных .srs плюс файл extra (признак ревизии).
func geoArchive(t *testing.T, extra string) []byte {
	t.Helper()
	files, err := presets.EmbeddedGeoFiles()
	if err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	w := zip.NewWriter(&buf)
	put := func(name string, data []byte) {
		fw, err := w.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := fw.Write(data); err != nil {
			t.Fatal(err)
		}
	}
	for _, rel := range files {
		raw, _ := presets.GeoFile(rel)
		put(rel, raw)
	}
	if extra != "" {
		raw, _ := presets.GeoFile("geosite/google.srs")
		put("geosite/"+extra+".srs", raw)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// releaseData публикует релиз той же версии приложения (APP не предлагается) с
// наборами и/или гео-архивом.
func (f *updFix) releaseData(serial int64, pr map[string]updatestest.PresetArtifact, geo []byte) {
	f.t.Helper()
	f.release(updatestest.Release{Version: "0.5.0", Serial: serial, TSVersion: serial, Presets: pr, Geo: geo})
}

func (f *updFix) downloadComps(comps ...lavrayv1.UpdateComponent) ([]*lavrayv1.UpdateEvent, error) {
	return collectEvents(f.cl.DownloadUpdates(context.Background(), connect.NewRequest(&lavrayv1.DownloadUpdatesRequest{Components: comps})))
}

func (f *updFix) applyComps(comps ...lavrayv1.UpdateComponent) ([]*lavrayv1.UpdateEvent, error) {
	return collectEvents(f.cl.ApplyUpdates(context.Background(), connect.NewRequest(&lavrayv1.ApplyUpdatesRequest{Components: comps})))
}

func (f *updFix) rollbackComp(c lavrayv1.UpdateComponent) (*lavrayv1.RollbackResponse, error) {
	r, err := f.cl.Rollback(context.Background(), connect.NewRequest(&lavrayv1.RollbackRequest{Component: c}))
	if err != nil {
		return nil, err
	}
	return r.Msg, nil
}

// compState — состояние компонента из GetUpdateState.
func (f *updFix) compState(c lavrayv1.UpdateComponent) *lavrayv1.ComponentState {
	f.t.Helper()
	r, err := f.cl.GetUpdateState(context.Background(), connect.NewRequest(&lavrayv1.GetUpdateStateRequest{}))
	if err != nil {
		f.t.Fatal(err)
	}
	for _, cs := range r.Msg.Components {
		if cs.Component == c {
			return cs
		}
	}
	f.t.Fatalf("в GetUpdateState нет компонента %v", c)
	return nil
}

// startSession поднимает «активную сессию» на FakeRunner.
func (f *updFix) startSession() {
	f.t.Helper()
	cfg := []byte(`{"inbounds":[{"tag":"mixed-in","type":"mixed","listen":"127.0.0.1","listen_port":2080}]}`)
	if err := f.srv.inst.Start(context.Background(), inst.RunInfo{Mode: "system_proxy", ConfigJSON: cfg}); err != nil {
		f.t.Fatal(err)
	}
}

func (f *updFix) geoDirPath() string { return filepath.Join(f.st.Dir(), "geo") }

// restartReasons — причины событий ready_for_restart потока.
func restartReasons(evs []*lavrayv1.UpdateEvent) []string {
	var out []string
	for _, e := range evs {
		if r := e.GetReadyForRestart(); r != "" {
			out = append(out, r)
		}
	}
	return out
}

// appliedOf — компоненты из событий applied.
func appliedOf(evs []*lavrayv1.UpdateEvent) []string {
	var out []string
	for _, e := range evs {
		if a := e.GetApplied(); a != "" {
			out = append(out, a)
		}
	}
	return out
}

// verifiedFor — события verified компонента c.
func verifiedFor(evs []*lavrayv1.UpdateEvent, c lavrayv1.UpdateComponent) []*lavrayv1.VerificationPassed {
	var out []*lavrayv1.VerificationPassed
	for _, e := range evs {
		if v := e.GetVerified(); v != nil && v.Component == c {
			out = append(out, v)
		}
	}
	return out
}

// offerFor — предложение компонента c из ответа CheckUpdates (nil — нет).
func offerFor(r *lavrayv1.CheckUpdatesResponse, c lavrayv1.UpdateComponent) *lavrayv1.ComponentUpdate {
	for _, a := range r.Available {
		if a.Component == c {
			return a
		}
	}
	return nil
}
