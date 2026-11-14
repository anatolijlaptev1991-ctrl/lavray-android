// SPDX-License-Identifier: GPL-3.0-only

package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"
)

// TestRestoreChildSortingBeforeParentKeepsRows — таблица-потомок, имя которой
// стоит в алфавите раньше родителя (ON DELETE CASCADE), не теряет строки: очистка
// родителя не должна сносить уже перенесённых потомков.
func TestRestoreChildSortingBeforeParentKeepsRows(t *testing.T) {
	old := migrations
	t.Cleanup(func() { migrations = old })
	migrations = append(append([]migration{}, old...), migration{
		version: old[len(old)-1].version + 1,
		stmts: []string{
			`CREATE TABLE zz_parent (id INTEGER PRIMARY KEY, label TEXT NOT NULL)`,
			`CREATE TABLE aa_child (id INTEGER PRIMARY KEY, parent_id INTEGER NOT NULL REFERENCES zz_parent(id) ON DELETE CASCADE, note TEXT NOT NULL)`,
		},
	})
	src, dst := openTemp(t), openTemp(t)
	seed(t, src, "A")
	seed(t, dst, "B")
	for i := 1; i <= 3; i++ {
		mustExec(t, src.db, `INSERT INTO zz_parent(id, label) VALUES (?, ?)`, i, fmt.Sprintf("src-parent-%d", i))
		mustExec(t, src.db, `INSERT INTO aa_child(parent_id, note) VALUES (?, ?)`, i, fmt.Sprintf("src-child-%d", i))
	}
	mustExec(t, dst.db, `INSERT INTO zz_parent(id, label) VALUES (1, 'dst-parent')`)
	mustExec(t, dst.db, `INSERT INTO aa_child(parent_id, note) VALUES (1, 'dst-child')`)

	if err := dst.Restore(bg, snapshotOf(t, src)); err != nil {
		t.Fatalf("Restore: %v", err)
	}
	got, want := dumpTables(t, dst), dumpTables(t, src)
	for _, table := range []string{"aa_child", "zz_parent"} {
		if len(want[table]) != 3 || !reflect.DeepEqual(got[table], want[table]) {
			t.Errorf("таблица %s: got %q want %q", table, got[table], want[table])
		}
	}
}

// TestSortTablesParentsFirst — порядок переноса: родители раньше потомков, в
// цикле и без зависимостей — по имени, ссылка на себя и на чужую таблицу не
// зависимость.
func TestSortTablesParentsFirst(t *testing.T) {
	parents := map[string][]string{
		"aa_child":      {"zz_parent"},
		"mm_grandchild": {"aa_child", "zz_parent"},
		"self":          {"self"},
		"cyc_a":         {"cyc_b"},
		"cyc_b":         {"cyc_a"},
		"outside":       {"not_a_data_table"},
	}
	tables := []string{"aa_child", "cyc_a", "cyc_b", "mm_grandchild", "outside", "self", "zz_parent"}
	got := sortTablesParentsFirst(tables, parents)
	if len(got) != len(tables) {
		t.Fatalf("потеряны таблицы: %v", got)
	}
	pos := map[string]int{}
	for i, name := range got {
		pos[name] = i
	}
	for _, edge := range [][2]string{{"zz_parent", "aa_child"}, {"aa_child", "mm_grandchild"}, {"zz_parent", "mm_grandchild"}} {
		if pos[edge[0]] > pos[edge[1]] {
			t.Errorf("родитель %s после потомка %s: %v", edge[0], edge[1], got)
		}
	}
	if pos["cyc_a"] > pos["cyc_b"] {
		t.Errorf("цикл не по имени: %v", got)
	}
	if again := sortTablesParentsFirst(tables, parents); !reflect.DeepEqual(got, again) {
		t.Errorf("порядок недетерминирован: %v и %v", got, again)
	}
}

// TestRestoreDetachFailureAfterCommitIsWarning — сбой DETACH после COMMIT не
// отменяет успех: данные заменены, Restore возвращает nil, а соединение с
// прицепленной копией выбрасывается, чтобы следующее восстановление работало.
func TestRestoreDetachFailureAfterCommitIsWarning(t *testing.T) {
	src, dst, snap := pair(t)
	var warns []string
	dst.SetWarnLogger(func(format string, args ...any) { warns = append(warns, fmt.Sprintf(format, args...)) })
	old := detachCopy
	detachCopy = func(context.Context, *sql.Conn) error {
		return errors.New("инъекция: DETACH не удался")
	}
	t.Cleanup(func() { detachCopy = old })

	if err := dst.Restore(bg, snap); err != nil {
		t.Fatalf("Restore после COMMIT вернул ошибку: %v", err)
	}
	if len(warns) != 1 || !strings.Contains(warns[0], "DETACH не удался") {
		t.Errorf("предупреждения: %q", warns)
	}
	want := dumpTables(t, src)
	if got := dumpTables(t, dst); !reflect.DeepEqual(got["profiles"], want["profiles"]) {
		t.Error("данные не заменены")
	}

	detachCopy = old
	seed(t, src, "C") // ещё одна копия: прежнее соединение выброшено, ATTACH снова возможен
	if err := dst.Restore(bg, snapshotOf(t, src)); err != nil {
		t.Fatalf("повторный Restore: %v", err)
	}
}

// TestRestoreDetachFailureDoesNotMaskRestoreError — сбой DETACH не подменяет
// исходную ошибку неудачного восстановления.
func TestRestoreDetachFailureDoesNotMaskRestoreError(t *testing.T) {
	_, dst, snap := pair(t)
	before := dumpTables(t, dst)
	oldHook, oldDetach := afterTableRestored, detachCopy
	afterTableRestored = func(string) error { return errors.New("сбой посреди") }
	detachCopy = func(context.Context, *sql.Conn) error {
		return errors.New("инъекция: DETACH не удался")
	}
	t.Cleanup(func() { afterTableRestored, detachCopy = oldHook, oldDetach })

	err := dst.Restore(bg, snap)
	if err == nil || !strings.Contains(err.Error(), "сбой посреди") {
		t.Fatalf("err = %v", err)
	}
	equalDump(t, "после отката", dumpTables(t, dst), before)
}
