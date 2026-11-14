// SPDX-License-Identifier: GPL-3.0-only

package store

import (
	"encoding/json"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/anatolijlaptev1991-ctrl/lavray-android/core/internal/model"
)

// customProfile — профиль с custom_config в группе Default.
func customProfile(cfg string) *model.Profile {
	p := newProfile(DefaultGroupID, model.TypeTrojan, "c", "example.com", 443)
	p.CustomConfigJSON = cfg
	return p
}

func confirm(t *testing.T, s *Store, p *model.Profile) *model.Profile {
	t.Helper()
	h, err := model.CustomFieldsHash(p)
	if err != nil {
		t.Fatal(err)
	}
	got, err := s.ConfirmCustomFields(p.ID, h)
	if err != nil {
		t.Fatalf("ConfirmCustomFields: %v", err)
	}
	return got
}

func TestCustomHashRoundTrip(t *testing.T) {
	s := openTemp(t)
	p := customProfile(`{"dns":{"final":"remote"}}`)
	if err := s.CreateProfile(p); err != nil {
		t.Fatal(err)
	}
	if p.CustomFieldsConfirmed {
		t.Fatal("новый профиль не подтверждён")
	}
	got := confirm(t, s, p)
	if !got.CustomFieldsConfirmed || got.CustomHash == "" {
		t.Fatalf("после подтверждения: %+v", got)
	}
	read, err := s.GetProfile(p.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !read.CustomFieldsConfirmed || read.CustomHash != got.CustomHash {
		t.Fatal("подтверждение не пережило чтение")
	}
	// Правка, не задевающая custom-поля, подтверждение сохраняет.
	read.Name = "renamed"
	if err := s.UpdateProfile(read); err != nil {
		t.Fatal(err)
	}
	if again, _ := s.GetProfile(p.ID); !again.CustomFieldsConfirmed {
		t.Fatal("переименование сняло подтверждение")
	}
}

func TestStoreDropsStaleCustomHash(t *testing.T) {
	// A → B → A: устаревший хэш стирается при записи и подтверждение не
	// возвращается вместе с прежним содержимым.
	s := openTemp(t)
	p := customProfile(`{"dns":{"final":"remote"}}`)
	if err := s.CreateProfile(p); err != nil {
		t.Fatal(err)
	}
	a := confirm(t, s, p)
	a.CustomConfigJSON = `{"dns":{"final":"local"}}`
	if err := s.UpdateProfile(a); err != nil {
		t.Fatal(err)
	}
	if a.CustomFieldsConfirmed || a.CustomHash != "" {
		t.Fatalf("после правки: confirmed=%v hash=%q", a.CustomFieldsConfirmed, a.CustomHash)
	}
	b, _ := s.GetProfile(p.ID)
	b.CustomConfigJSON = `{"dns":{"final":"remote"}}`
	if err := s.UpdateProfile(b); err != nil {
		t.Fatal(err)
	}
	if back, _ := s.GetProfile(p.ID); back.CustomFieldsConfirmed {
		t.Fatal("возврат к прежнему содержимому не возвращает подтверждение")
	}
}

func TestScanComputesConfirmedFromHash(t *testing.T) {
	s := openTemp(t)
	p := customProfile(`{"route":{"final":"direct"}}`)
	if err := s.CreateProfile(p); err != nil {
		t.Fatal(err)
	}
	// Флаг и хэш в модели клиента не действуют: хранилище пишет только хэш,
	// совпадающий с содержимым.
	p.CustomFieldsConfirmed = true
	p.CustomHash = "0000000000000000000000000000000000000000000000000000000000000000"
	if err := s.UpdateProfile(p); err != nil {
		t.Fatal(err)
	}
	if got, _ := s.GetProfile(p.ID); got.CustomFieldsConfirmed || got.CustomHash != "" {
		t.Fatalf("чужой хэш принят: %+v", got)
	}
	// Хэш в колонке, не совпадающий с содержимым (правка в обход UpdateProfile).
	h, _ := model.CustomFieldsHash(p)
	if _, err := s.DB().Exec(`UPDATE profiles SET custom_hash = ?, data = json_set(data, '$.custom_config_json', '{"x":1}') WHERE id = ?`, h, p.ID); err != nil {
		t.Fatal(err)
	}
	if got, _ := s.GetProfile(p.ID); got.CustomFieldsConfirmed {
		t.Fatal("подтверждение вычисляется по текущему содержимому")
	}
}

func TestConfirmCustomFieldsErrors(t *testing.T) {
	s := openTemp(t)
	if _, err := s.ConfirmCustomFields(999, "x"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("нет профиля: %v", err)
	}
	plain := newProfile(DefaultGroupID, model.TypeTrojan, "p", "h", 1)
	if err := s.CreateProfile(plain); err != nil {
		t.Fatal(err)
	}
	h, _ := model.CustomFieldsHash(plain)
	if _, err := s.ConfirmCustomFields(plain.ID, h); !errors.Is(err, ErrNoCustomContent) {
		t.Fatalf("без custom-полей: %v", err)
	}
	p := customProfile(`{"dns":{}}`)
	p.CustomConfigJSON = `{"dns":{"final":"remote"}}`
	if err := s.CreateProfile(p); err != nil {
		t.Fatal(err)
	}
	if _, err := s.ConfirmCustomFields(p.ID, h); !errors.Is(err, ErrCustomHashStale) {
		t.Fatalf("чужой хэш: %v", err)
	}
	if got, _ := s.GetProfile(p.ID); got.CustomFieldsConfirmed {
		t.Fatal("неудачное подтверждение ничего не записывает")
	}
}

// openAtVersion создаёт БД со схемой не выше version (как у старой сборки).
func openAtVersion(t *testing.T, dir string, version int) *Store {
	t.Helper()
	saved := migrations
	migrations = migrations[:version]
	defer func() { migrations = saved }()
	s, err := Open(dir)
	if err != nil {
		t.Fatalf("open v%d: %v", version, err)
	}
	return s
}

func TestMigrationV3AddsHashAndResetsConfirmations(t *testing.T) {
	dir, err := os.MkdirTemp("", "lavray-store-v3-*")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		for i := 0; i < 50 && os.RemoveAll(dir) != nil; i++ {
			time.Sleep(10 * time.Millisecond)
		}
	})
	old := openAtVersion(t, dir, 2)
	p := customProfile(`{"dns":{"final":"remote"}}`)
	p.ID = 7
	data, _ := json.Marshal(p)
	// Сборка 0.2.x: флаг custom_confirmed=1 мог выставить сам клиент.
	if _, err := old.DB().Exec(`INSERT INTO profiles (id, group_id, type, name, sort_key, data, custom_confirmed, created_at_ms, updated_at_ms)
		VALUES (7, ?, 'trojan', 'c', 1024, ?, 1, 1, 1)`, DefaultGroupID, string(data)); err != nil {
		t.Fatal(err)
	}
	if err := old.Close(); err != nil {
		t.Fatal(err)
	}
	s, err := Open(dir)
	if err != nil {
		t.Fatalf("миграция v3: %v", err)
	}
	defer func() { _ = s.Close() }()
	if v, _ := s.SchemaVersion(); v < 3 {
		t.Fatalf("schema_version = %d", v)
	}
	var flag int
	if err := s.DB().QueryRow(`SELECT custom_confirmed FROM profiles WHERE id = 7`).Scan(&flag); err != nil || flag != 0 {
		t.Fatalf("custom_confirmed = %d (%v), ждём 0", flag, err)
	}
	got, err := s.GetProfile(7)
	if err != nil {
		t.Fatal(err)
	}
	if got.CustomFieldsConfirmed || got.CustomHash != "" {
		t.Fatal("старое подтверждение не переносится: подтвердить заново через diff")
	}
	if _, err := s.ConfirmCustomFields(7, mustCustomHash(t, got)); err != nil {
		t.Fatalf("после миграции подтверждение работает: %v", err)
	}
}

func mustCustomHash(t *testing.T, p *model.Profile) string {
	t.Helper()
	h, err := model.CustomFieldsHash(p)
	if err != nil {
		t.Fatal(err)
	}
	return h
}

func TestStaleWriteDoesNotReviveConfirmation(t *testing.T) {
	// R1 прочитал подтверждённое A; R2 правит на B, R3 обратно на A
	// (подтверждение снято); затем R1 пишет свою устаревшую копию с хэшем A.
	s := openTemp(t)
	p := customProfile(`{"dns":{"final":"remote"}}`)
	if err := s.CreateProfile(p); err != nil {
		t.Fatal(err)
	}
	stale := confirm(t, s, p) // R1: A + H(A)
	for _, cfg := range []string{`{"dns":{"final":"local"}}`, `{"dns":{"final":"remote"}}`} {
		cur, _ := s.GetProfile(p.ID)
		cur.CustomConfigJSON = cfg
		if err := s.UpdateProfile(cur); err != nil {
			t.Fatal(err)
		}
	}
	stale.Name = "R1"
	if err := s.UpdateProfile(stale); err != nil {
		t.Fatal(err)
	}
	if got, _ := s.GetProfile(p.ID); got.CustomFieldsConfirmed || got.Name != "R1" {
		t.Fatalf("устаревшая копия вернула подтверждение: %+v", got)
	}
}

func TestCreateProfileDropsHash(t *testing.T) {
	// Клон и любой новый профиль не подтверждены, даже если модель несёт
	// верный хэш содержимого.
	s := openTemp(t)
	p := customProfile(`{"dns":{"final":"remote"}}`)
	p.CustomHash = mustCustomHash(t, p)
	if err := s.CreateProfile(p); err != nil {
		t.Fatal(err)
	}
	if got, _ := s.GetProfile(p.ID); got.CustomFieldsConfirmed || got.CustomHash != "" {
		t.Fatal("CreateProfile принял хэш подтверждения")
	}
}

func TestImportProfileKeepsValidHash(t *testing.T) {
	// ImportProfile — восстановление снимка (откат подписки): прежнее
	// подтверждение возвращается, если содержимое то же.
	s := openTemp(t)
	p := customProfile(`{"dns":{"final":"remote"}}`)
	p.ID = 42
	p.CustomHash = mustCustomHash(t, p)
	if err := s.ImportProfile(p); err != nil {
		t.Fatal(err)
	}
	if got, _ := s.GetProfile(42); !got.CustomFieldsConfirmed {
		t.Fatal("снимок с верным хэшем должен остаться подтверждённым")
	}
}
