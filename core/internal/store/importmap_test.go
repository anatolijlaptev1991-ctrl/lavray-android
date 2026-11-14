// SPDX-License-Identifier: GPL-3.0-only

package store

// importmap_test.go — миграция import_map (T-78) и доступ к таблице. Номер
// миграции в имени теста и в importMapSchemaVersion при слиянии веток может
// сместиться: правится одна константа и одна строка migrations.sha256.

import (
	"strings"
	"testing"

	"github.com/anatolijlaptev1991-ctrl/lavray-android/core/internal/model"
)

func TestMigrationV6(t *testing.T) {
	s := openTemp(t)
	if v, err := s.SchemaVersion(); err != nil || v < importMapSchemaVersion {
		t.Fatalf("версия схемы = %d, %v; ждём не меньше %d", v, err, importMapSchemaVersion)
	}
	rows, err := s.db.Query(`SELECT name FROM pragma_table_info('import_map')`)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = rows.Close() }()
	cols := map[string]bool{}
	for rows.Next() {
		var n string
		if err := rows.Scan(&n); err != nil {
			t.Fatal(err)
		}
		cols[n] = true
	}
	for _, want := range []string{"source", "kind", "source_key", "local_id", "imported_at_ms"} {
		if !cols[want] {
			t.Errorf("в import_map нет колонки %q (есть %v)", want, cols)
		}
	}
	// kind ограничен тремя значениями (I).
	_, err = s.db.Exec(`INSERT INTO import_map VALUES ('nekobox', 'route', 'k', '1', 1)`)
	if err == nil {
		t.Error("kind='route' принят, ждём отказ CHECK")
	}
}

func TestImportMapPutAndList(t *testing.T) {
	s := openTemp(t)
	row := ImportMapRow{Source: "nekobox", Kind: "profile", SourceKey: "k1", LocalID: "7"}
	if err := s.PutImportMap(row); err != nil {
		t.Fatal(err)
	}
	// Повторная запись того же ключа заменяет local_id (D): запись после удаления
	// локальной копии и повторного переноса.
	row.LocalID = "9"
	if err := s.PutImportMap(row); err != nil {
		t.Fatal(err)
	}
	if err := s.PutImportMap(ImportMapRow{Source: "other", Kind: "profile", SourceKey: "k1", LocalID: "1"}); err != nil {
		t.Fatal(err)
	}
	got, err := s.ImportMapRows("nekobox")
	if err != nil || len(got) != 1 || got[0].LocalID != "9" || got[0].ImportedAtMs == 0 {
		t.Fatalf("строки nekobox = %+v, %v; ждём одну с local_id=9", got, err)
	}
	if err := s.PutImportMap(ImportMapRow{Source: "nekobox", Kind: "bogus", SourceKey: "x", LocalID: "1"}); err == nil {
		t.Error("kind=bogus принят")
	}
	if err := s.PutImportMap(ImportMapRow{Source: "", Kind: "group", SourceKey: "x", LocalID: "1"}); err == nil {
		t.Error("пустой source принят")
	}
}

// Запись import_map входит в транзакцию серии: откат серии откатывает и её.
func TestImportMapInsideInTx(t *testing.T) {
	s := openTemp(t)
	err := s.InTx(func(tx *Store) error {
		if err := tx.PutImportMap(ImportMapRow{Source: "nekobox", Kind: "group", SourceKey: "g", LocalID: "1"}); err != nil {
			return err
		}
		return ErrConstraint
	})
	if err == nil {
		t.Fatal("ждали ошибку fn")
	}
	if rows, _ := s.ImportMapRows("nekobox"); len(rows) != 0 {
		t.Fatalf("после отката в import_map %+v", rows)
	}
}

func TestEntityIDsAndFloor(t *testing.T) {
	s, g, a, _ := t51Seed(t)
	ids, err := s.EntityIDs("profiles")
	if err != nil || len(ids) != 2 || !ids[a.ID] {
		t.Fatalf("EntityIDs(profiles) = %v, %v", ids, err)
	}
	if _, err := s.EntityIDs("sqlite_master"); err == nil {
		t.Error("EntityIDs принял чужую таблицу")
	}
	floor, err := s.IDFloor("profiles")
	if err != nil || floor < a.ID {
		t.Fatalf("IDFloor = %d, %v; ждём не меньше %d", floor, err, a.ID)
	}
	// Отметка растёт и не убывает; удалённый id не возвращается в оборот.
	if err := s.RaiseIDFloor("profiles", floor+50); err != nil {
		t.Fatal(err)
	}
	if err := s.RaiseIDFloor("profiles", 3); err != nil {
		t.Fatal(err)
	}
	if got, _ := s.IDFloor("profiles"); got != floor+50 {
		t.Fatalf("после RaiseIDFloor = %d, ждём %d", got, floor+50)
	}
	p := newProfile(g.ID, model.TypeSOCKS, "N", "n.example", 1)
	if err := s.CreateProfile(p); err != nil || p.ID != floor+51 {
		t.Fatalf("следующий id = %d, %v; ждём %d", p.ID, err, floor+51)
	}
	if err := s.RaiseIDFloor("nope", 1); err == nil || !strings.Contains(err.Error(), "nope") {
		t.Errorf("RaiseIDFloor(nope) = %v", err)
	}
}
