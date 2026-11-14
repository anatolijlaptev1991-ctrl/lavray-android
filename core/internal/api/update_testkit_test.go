// SPDX-License-Identifier: GPL-3.0-only

package api

import (
	"context"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"testing"

	"connectrpc.com/connect"

	lavrayv1 "github.com/anatolijlaptev1991-ctrl/lavray-android/core/internal/gen/lavrayv1"
	"github.com/anatolijlaptev1991-ctrl/lavray-android/core/internal/gen/lavrayv1connect"
	"github.com/anatolijlaptev1991-ctrl/lavray-android/core/internal/inst"
	"github.com/anatolijlaptev1991-ctrl/lavray-android/core/internal/store"
	"github.com/anatolijlaptev1991-ctrl/lavray-android/core/internal/updates"
	"github.com/anatolijlaptev1991-ctrl/lavray-android/core/internal/updates/updatestest"
)

// updFix — сервер ядра с подсистемой обновлений и подписанным «источником».
type updFix struct {
	t   *testing.T
	srv *Server
	st  *store.Store
	env *updatestest.Env
	src *updatestest.Source
	cl  lavrayv1connect.UpdateServiceClient
	url string
}

// newUpdFix поднимает сервер с установленной версией appVersion; mutate правит
// конфигурацию обновлений до старта.
func newUpdFix(t *testing.T, appVersion string, mutate func(*UpdaterConf)) *updFix {
	t.Helper()
	env, src := updatestest.New(t), updatestest.NewSource()
	conf := &UpdaterConf{RootJSON: env.RootJSON, Product: env.Product, AppVersion: appVersion,
		Platform: "windows-x64", BootstrapSources: []updates.Source{src}}
	if mutate != nil {
		mutate(conf)
	}
	f := &updFix{t: t, env: env, src: src}
	st, hs := newE2EServer(t, Deps{Runner: &inst.FakeRunner{}, Updater: conf}, func(s *Server) { f.srv = s })
	f.st, f.url = st, hs.URL
	f.cl = lavrayv1connect.NewUpdateServiceClient(http.DefaultClient, hs.URL)
	return f
}

// release публикует релиз на источник.
func (f *updFix) release(r updatestest.Release) string {
	f.t.Helper()
	return f.env.Publish(f.t, f.src, r)
}

// stagedFiles — имена файлов в каталоге staging.
func (f *updFix) stagedFiles() []string {
	es, _ := os.ReadDir(filepath.Join(f.st.Dir(), "updates", "staging"))
	var out []string
	for _, e := range es {
		out = append(out, e.Name())
	}
	return out
}

func (f *updFix) check() (*lavrayv1.CheckUpdatesResponse, error) {
	r, err := f.cl.CheckUpdates(context.Background(), connect.NewRequest(&lavrayv1.CheckUpdatesRequest{}))
	if err != nil {
		return nil, err
	}
	return r.Msg, nil
}

// download выполняет DownloadUpdates и возвращает все события потока.
func (f *updFix) download() ([]*lavrayv1.UpdateEvent, error) {
	return collectEvents(f.cl.DownloadUpdates(context.Background(), connect.NewRequest(&lavrayv1.DownloadUpdatesRequest{})))
}

func (f *updFix) apply(confirmed bool) ([]*lavrayv1.UpdateEvent, error) {
	return collectEvents(f.cl.ApplyUpdates(context.Background(), connect.NewRequest(&lavrayv1.ApplyUpdatesRequest{ActiveSessionConfirmed: confirmed})))
}

func collectEvents(st *connect.ServerStreamForClient[lavrayv1.UpdateEvent], err error) ([]*lavrayv1.UpdateEvent, error) {
	if err != nil {
		return nil, err
	}
	var out []*lavrayv1.UpdateEvent
	for st.Receive() {
		out = append(out, st.Msg())
	}
	return out, st.Err()
}

// verified — последнее событие verified потока (или nil).
func verified(evs []*lavrayv1.UpdateEvent) *lavrayv1.VerificationPassed {
	for i := len(evs) - 1; i >= 0; i-- {
		if v := evs[i].GetVerified(); v != nil {
			return v
		}
	}
	return nil
}

func asConnect(err error, target **connect.Error) bool { return errors.As(err, target) }

func newSettingsClient(t *testing.T, f *updFix) lavrayv1connect.SettingsServiceClient {
	t.Helper()
	return lavrayv1connect.NewSettingsServiceClient(http.DefaultClient, f.url)
}
