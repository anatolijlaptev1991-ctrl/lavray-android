// SPDX-License-Identifier: GPL-3.0-only

package api

// migration_fixture_test.go — оснастка тестов MigrationService: сервер на
// временном хранилище с подставным окружением поиска (процессы и %APPDATA%
// задаются тестом — настоящие не читаются) и копия обезличенного каталога
// config NekoBox из пакета nekoimport.

import (
	"context"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"testing"
	"time"

	"connectrpc.com/connect"

	lavrayv1 "github.com/anatolijlaptev1991-ctrl/lavray-android/core/internal/gen/lavrayv1"
	"github.com/anatolijlaptev1991-ctrl/lavray-android/core/internal/gen/lavrayv1connect"
	"github.com/anatolijlaptev1991-ctrl/lavray-android/core/internal/inst"
	"github.com/anatolijlaptev1991-ctrl/lavray-android/core/internal/store"
	"github.com/anatolijlaptev1991-ctrl/lavray-android/core/internal/store/nekoimport"
)

// nekoFixtureDir — фикстура nekoimport (путь от каталога пакета api).
var nekoFixtureDir = filepath.Join("..", "store", "nekoimport", "testdata", "nekobox", "config")

type migrationFixture struct {
	srv      *Server
	st       *store.Store
	migrate  lavrayv1connect.MigrationServiceClient
	routing  lavrayv1connect.RoutingServiceClient
	settings lavrayv1connect.SettingsServiceClient
	url      string
}

// newMigrationFixture поднимает сервер; env — окружение поиска (nil → пустое,
// без процессов и без каталогов профиля).
func newMigrationFixture(t *testing.T, env *nekoimport.Env) *migrationFixture {
	t.Helper()
	var srv *Server
	st, httpSrv := newE2EServer(t, Deps{Runner: &inst.FakeRunner{}}, func(s *Server) {
		srv = s
		s.migration.env = nekoimport.Env{Processes: func() ([]nekoimport.Process, error) { return nil, nil }}
		if env != nil {
			s.migration.env = *env
		}
	})
	hc := httpSrv.Client()
	return &migrationFixture{
		srv: srv, st: st, url: httpSrv.URL,
		migrate:  lavrayv1connect.NewMigrationServiceClient(hc, httpSrv.URL),
		routing:  lavrayv1connect.NewRoutingServiceClient(hc, httpSrv.URL),
		settings: lavrayv1connect.NewSettingsServiceClient(hc, httpSrv.URL),
	}
}

func (f *migrationFixture) importNeko(t *testing.T, req *lavrayv1.NekoBoxImportRequest) *lavrayv1.NekoBoxReport {
	t.Helper()
	res, err := f.migrate.ImportNekoBox(context.Background(), connect.NewRequest(req))
	if err != nil {
		t.Fatalf("ImportNekoBox: %v", err)
	}
	return res.Msg
}

func (f *migrationFixture) previewNeko(t *testing.T, req *lavrayv1.NekoBoxImportRequest) *lavrayv1.NekoBoxReport {
	t.Helper()
	res, err := f.migrate.PreviewNekoBoxImport(context.Background(), connect.NewRequest(req))
	if err != nil {
		t.Fatalf("PreviewNekoBoxImport: %v", err)
	}
	return res.Msg
}

// copyNekoFixture копирует фикстуру во временный каталог (тест может её править).
func copyNekoFixture(t *testing.T) string {
	t.Helper()
	dst := mkNekoTemp(t)
	copyNekoFixtureInto(t, dst)
	return dst
}

func mkNekoTemp(t *testing.T) string {
	t.Helper()
	dst, err := os.MkdirTemp("", "lavray-api-neko-*")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		for i := 0; i < 50; i++ {
			if os.RemoveAll(dst) == nil {
				return
			}
			time.Sleep(10 * time.Millisecond)
		}
	})
	return dst
}

func copyNekoFixtureInto(t *testing.T, dst string) {
	t.Helper()
	err := filepath.WalkDir(nekoFixtureDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(nekoFixtureDir, path)
		target := filepath.Join(dst, rel)
		if d.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		in, err := os.Open(path)
		if err != nil {
			return err
		}
		defer func() { _ = in.Close() }()
		out, err := os.Create(target)
		if err != nil {
			return err
		}
		defer func() { _ = out.Close() }()
		_, err = io.Copy(out, in)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
}

// profileCount — число профилей по всем группам.
func profileCount(t *testing.T, st *store.Store) int {
	t.Helper()
	groups, err := st.ListGroups()
	if err != nil {
		t.Fatal(err)
	}
	n := 0
	for _, g := range groups {
		list, err := st.ListProfiles(g.ID)
		if err != nil {
			t.Fatal(err)
		}
		n += len(list)
	}
	return n
}
