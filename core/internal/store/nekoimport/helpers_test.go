// SPDX-License-Identifier: GPL-3.0-only

package nekoimport

import (
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"testing"

	"github.com/anatolijlaptev1991-ctrl/lavray-android/core/internal/model"
	"github.com/anatolijlaptev1991-ctrl/lavray-android/core/internal/store"
)

// fixtureConfig — обезличенный каталог config NekoBox: 2 группы (вторая —
// подписка с front_proxy_id), 7 профилей (цепочка 6 → 3, 2), битый и неизвестный
// файлы, два маршрута, groups/nekobox.json.
var fixtureConfig = filepath.Join("testdata", "nekobox", "config")

// copyTree копирует каталог src в новый временный каталог.
func copyTree(t *testing.T, src string) string {
	t.Helper()
	dst := mkTempDir(t)
	err := filepath.WalkDir(src, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(src, path)
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
	return dst
}

// seedProfile создаёт в хранилище профиль с автоматическим id (первый — 1).
func seedProfile(t *testing.T, s *store.Store, name string) *model.Profile {
	t.Helper()
	groups, err := s.ListGroups()
	if err != nil || len(groups) == 0 {
		t.Fatalf("groups: %v %v", groups, err)
	}
	p := &model.Profile{GroupID: groups[0].ID, Type: model.TypeSOCKS, Name: name, Address: "198.51.100.1", Port: 1080,
		Detail: &model.SocksHTTPDetail{Proto: "socks5"}}
	if err := s.CreateProfile(p); err != nil {
		t.Fatal(err)
	}
	return p
}

// allProfiles — все профили всех групп по id.
func allProfiles(t *testing.T, s *store.Store) map[int64]*model.Profile {
	t.Helper()
	groups, err := s.ListGroups()
	if err != nil {
		t.Fatal(err)
	}
	out := map[int64]*model.Profile{}
	for _, g := range groups {
		list, err := s.ListProfiles(g.ID)
		if err != nil {
			t.Fatal(err)
		}
		for _, p := range list {
			out[p.ID] = p
		}
	}
	return out
}

func profileByName(t *testing.T, s *store.Store, name string) *model.Profile {
	t.Helper()
	for _, p := range allProfiles(t, s) {
		if p.Name == name {
			return p
		}
	}
	t.Fatalf("профиль %q не найден", name)
	return nil
}
