// SPDX-License-Identifier: GPL-3.0-only

package store

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/anatolijlaptev1991-ctrl/lavray-android/core/internal/model"
)

func openTemp(t *testing.T) *Store {
	t.Helper()
	// Не t.TempDir(): на Windows его RemoveAll гоняется с асинхронным
	// освобождением хэндлов -wal/-shm (modernc/sqlite) — флейк «directory
	// is not empty». Свой каталог + ретрай-удаление после Close.
	dir, err := os.MkdirTemp("", "lavray-store-test-*")
	if err != nil {
		t.Fatalf("mkdir temp: %v", err)
	}
	s, err := Open(dir)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() {
		_ = s.Close()
		for i := 0; i < 50; i++ {
			if err := os.RemoveAll(dir); err == nil {
				return
			}
			time.Sleep(10 * time.Millisecond)
		}
	})
	return s
}

// mkTempDir — каталог с ретрай-удалением (см. openTemp: гонка -wal/-shm на Windows).
func mkTempDir(t *testing.T) string {
	t.Helper()
	dir, err := os.MkdirTemp("", "lavray-store-test-*")
	if err != nil {
		t.Fatalf("mkdir temp: %v", err)
	}
	t.Cleanup(func() {
		for i := 0; i < 50; i++ {
			if err := os.RemoveAll(dir); err == nil {
				return
			}
			time.Sleep(10 * time.Millisecond)
		}
	})
	return dir
}

func newGroup(name string, kind model.GroupKind) *model.Group {
	return &model.Group{Name: name, Kind: kind}
}

func newProfile(gid int64, typ model.ProfileType, name, addr string, port int) *model.Profile {
	return &model.Profile{GroupID: gid, Type: typ, Name: name, Address: addr, Port: port, Detail: detailFor(typ)}
}

func detailFor(typ model.ProfileType) model.Detail {
	switch typ {
	case model.TypeSOCKS:
		return &model.SocksHTTPDetail{Proto: "socks5"}
	case model.TypeHysteria2:
		return &model.Hysteria2Detail{Password: "x", ServerPorts: "1:2", HopInterval: "30s"}
	case model.TypeChain:
		return &model.ChainDetail{ProfileIDs: []int64{1, 2}}
	default:
		return &model.TrojanVlessDetail{Password: "x"}
	}
}

func TestStoreMigrationsIdempotent(t *testing.T) {
	dir := mkTempDir(t)
	s1, err := Open(dir)
	if err != nil {
		t.Fatalf("first open: %v", err)
	}
	// Версия = последняя миграция в списке (не хардкод: новые миграции
	// добавляются, идемпотентность проверяется ниже).
	want := migrations[len(migrations)-1].version
	v1, err := s1.SchemaVersion()
	if err != nil || v1 != want {
		t.Fatalf("schema version after first open = %d, %v; want %d", v1, err, want)
	}
	if err := s1.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	// Повторное открытие не должно падать и дублировать миграции.
	s2, err := Open(dir)
	if err != nil {
		t.Fatalf("second open: %v", err)
	}
	defer s2.Close()
	v2, err := s2.SchemaVersion()
	if err != nil || v2 != want {
		t.Fatalf("schema version after second open = %d, %v; want %d", v2, err, want)
	}
	// По строке на каждую применённую миграцию; повторное открытие их не
	// дублирует.
	var n int
	if err := s2.DB().QueryRow(`SELECT COUNT(*) FROM schema_version`).Scan(&n); err != nil || int64(n) != want {
		t.Fatalf("schema_version rows = %d, %v; want %d", n, err, want)
	}
}

func TestFileModes(t *testing.T) {
	if runtime.GOOS == "windows" {
		// Windows: POSIX-режимы не применяются — права проверяются только
		// на unix (см. store.applyFileModes).
		t.Skip("POSIX file modes are not applicable on windows")
	}
	dir := mkTempDir(t)
	s, err := Open(dir)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer s.Close()

	di, err := os.Stat(dir)
	if err != nil {
		t.Fatal(err)
	}
	if di.Mode().Perm() != 0o700 {
		t.Fatalf("dir mode = %o, want 0700", di.Mode().Perm())
	}
	fi, err := os.Stat(filepath.Join(dir, dbFileName))
	if err != nil {
		t.Fatal(err)
	}
	if fi.Mode().Perm() != 0o600 {
		t.Fatalf("db mode = %o, want 0600", fi.Mode().Perm())
	}
}

func TestGroupCRUD(t *testing.T) {
	s := openTemp(t)

	g1 := newGroup("Basic", model.GroupBasic)
	if err := s.CreateGroup(g1); err != nil {
		t.Fatalf("create g1: %v", err)
	}
	if g1.ID == 0 || g1.TabOrder != 1 {
		t.Fatalf("g1 id/tab_order not assigned: %+v", g1)
	}
	g2 := newGroup("Sub", model.GroupSubscription)
	g2.SubscriptionURL = "https://sub.example.com/x"
	if err := s.CreateGroup(g2); err != nil {
		t.Fatalf("create g2: %v", err)
	}

	got2, err := s.GetGroup(g2.ID)
	if err != nil {
		t.Fatalf("get g2: %v", err)
	}
	if got2.Kind != model.GroupSubscription || got2.SubscriptionURL != g2.SubscriptionURL {
		t.Fatalf("g2 mismatch: %+v", got2)
	}

	got2.Name = "Sub2"
	got2.Archive = true
	if err := s.UpdateGroup(got2); err != nil {
		t.Fatalf("update g2: %v", err)
	}
	got2, err = s.GetGroup(g2.ID)
	if err != nil || got2.Name != "Sub2" || !got2.Archive {
		t.Fatalf("update not applied: %+v (%v)", got2, err)
	}

	// Порядок вкладок.
	if err := s.ReorderGroups([]int64{g2.ID, g1.ID}); err != nil {
		t.Fatalf("reorder groups: %v", err)
	}
	list, err := s.ListGroups()
	if err != nil {
		t.Fatalf("list groups: %v", err)
	}
	// Миграция сеет группу Default (id=1) — в списке три группы, порядок
	// переставленных проверяем в хвосте.
	if len(list) != 3 || list[1].ID != g2.ID || list[2].ID != g1.ID {
		t.Fatalf("group order wrong: %+v", list)
	}

	// Последнюю группу удалить нельзя (FR-UI-107). Миграция сеет Default,
	// поэтому «последняя» наступает после удаления обеих созданных.
	if err := s.DeleteGroup(g1.ID); err != nil {
		t.Fatalf("delete g1: %v", err)
	}
	if err := s.DeleteGroup(g2.ID); err != nil {
		t.Fatalf("delete g2: %v", err)
	}
	if err := s.DeleteGroup(1); !errors.Is(err, ErrLastGroup) {
		t.Fatalf("delete last (Default) group: got %v, want ErrLastGroup", err)
	}
}

func TestProfileCRUD(t *testing.T) {
	s := openTemp(t)
	g := newGroup("G", model.GroupBasic)
	if err := s.CreateGroup(g); err != nil {
		t.Fatalf("create group: %v", err)
	}

	p1 := newProfile(g.ID, model.TypeSOCKS, "one", "1.1.1.1", 1080)
	if err := s.CreateProfile(p1); err != nil {
		t.Fatalf("create p1: %v", err)
	}
	if p1.ID == 0 {
		t.Fatal("id not assigned")
	}
	p2 := newProfile(g.ID, model.TypeHysteria2, "two", "2.2.2.2", 443)
	if err := s.CreateProfile(p2); err != nil {
		t.Fatalf("create p2: %v", err)
	}
	p3 := newProfile(g.ID, model.TypeTrojan, "three", "3.3.3.3", 443)
	if err := s.CreateProfile(p3); err != nil {
		t.Fatalf("create p3: %v", err)
	}

	got, err := s.GetProfile(p2.ID)
	if err != nil {
		t.Fatalf("get p2: %v", err)
	}
	d, ok := got.Detail.(*model.Hysteria2Detail)
	if !ok || d.ServerPorts != "1:2" || d.HopInterval != "30s" {
		t.Fatalf("hy2 detail lost: %+v", got.Detail)
	}

	// Канонический порядок — порядок вставки.
	profiles, err := s.ListProfiles(g.ID)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(profiles) != 3 || profiles[0].ID != p1.ID || profiles[2].ID != p3.ID {
		t.Fatalf("unexpected order: %d %d %d", profiles[0].ID, profiles[1].ID, profiles[2].ID)
	}

	// Reorder (FR-UI-074) + самокоррекция от чужого/удалённого id.
	if err := s.ReorderProfiles(g.ID, []int64{p3.ID, 999, p1.ID}); err != nil {
		t.Fatalf("reorder: %v", err)
	}
	profiles, err = s.ListProfiles(g.ID)
	if err != nil {
		t.Fatalf("list after reorder: %v", err)
	}
	want := []int64{p3.ID, p1.ID, p2.ID}
	for i, p := range profiles {
		if p.ID != want[i] {
			t.Fatalf("order after reorder: pos %d = %d, want %d", i, p.ID, want[i])
		}
	}

	// Update.
	got.Name = "renamed"
	got.Port = 2080
	if err := s.UpdateProfile(got); err != nil {
		t.Fatalf("update: %v", err)
	}
	got, err = s.GetProfile(p2.ID)
	if err != nil || got.Name != "renamed" || got.Port != 2080 {
		t.Fatalf("update not applied: %+v (%v)", got, err)
	}

	// Профиль в несуществующую группу — ошибка транзакции.
	bad := newProfile(999, model.TypeSOCKS, "x", "y", 1)
	if err := s.CreateProfile(bad); err == nil {
		t.Fatal("profile into missing group must fail")
	}

	// Каскад: удаление группы удаляет профили.
	if err := s.CreateGroup(newGroup("G2", model.GroupBasic)); err != nil {
		t.Fatal(err)
	}
	if err := s.DeleteGroup(g.ID); err != nil {
		t.Fatalf("delete group: %v", err)
	}
	if _, err := s.GetProfile(p1.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("profile must be cascade-deleted, got %v", err)
	}

	// Delete.
	if err := s.DeleteProfile(12345); !errors.Is(err, ErrNotFound) {
		t.Fatalf("delete missing: %v", err)
	}
}

func TestProfileMoveBetweenGroups(t *testing.T) {
	s := openTemp(t)
	ga := newGroup("A", model.GroupBasic)
	gb := newGroup("B", model.GroupBasic)
	if err := s.CreateGroup(ga); err != nil {
		t.Fatal(err)
	}
	if err := s.CreateGroup(gb); err != nil {
		t.Fatal(err)
	}
	p := newProfile(ga.ID, model.TypeVMess, "vm", "a.io", 1)
	if err := s.CreateProfile(p); err != nil {
		t.Fatal(err)
	}
	p.GroupID = gb.ID
	if err := s.UpdateProfile(p); err != nil {
		t.Fatalf("move: %v", err)
	}
	got, err := s.GetProfile(p.ID)
	if err != nil || got.GroupID != gb.ID {
		t.Fatalf("move not applied: %+v (%v)", got, err)
	}
	// Порядок в новой группе должен содержать профиль.
	g2, err := s.GetGroup(gb.ID)
	if err != nil || len(g2.ProfileOrder) != 1 || g2.ProfileOrder[0] != p.ID {
		t.Fatalf("group B order wrong: %+v (%v)", g2, err)
	}
}

func TestSettingsCRUD(t *testing.T) {
	s := openTemp(t)
	if _, ok, err := s.GetSetting("sub.use_proxy"); err != nil || ok {
		t.Fatalf("get missing: ok=%v err=%v", ok, err)
	}
	if err := s.SetSetting("sub.use_proxy", "true"); err != nil {
		t.Fatalf("set: %v", err)
	}
	v, ok, err := s.GetSetting("sub.use_proxy")
	if err != nil || !ok || v != "true" {
		t.Fatalf("get: v=%q ok=%v err=%v", v, ok, err)
	}
	if err := s.SetSetting("sub.use_proxy", "false"); err != nil {
		t.Fatalf("upsert: %v", err)
	}
	all, err := s.Settings()
	if err != nil || len(all) != 1 || all["sub.use_proxy"] != "false" {
		t.Fatalf("all: %+v (%v)", all, err)
	}
}

func TestMeta(t *testing.T) {
	s := openTemp(t)
	if _, ok, err := s.GetMeta("nekobox.imported"); err != nil || ok {
		t.Fatalf("meta missing expected: ok=%v err=%v", ok, err)
	}
	if err := s.SetMeta("nekobox.imported", "1"); err != nil {
		t.Fatal(err)
	}
	v, ok, err := s.GetMeta("nekobox.imported")
	if err != nil || !ok || v != "1" {
		t.Fatalf("meta get: %q %v %v", v, ok, err)
	}
}
