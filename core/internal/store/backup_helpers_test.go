// SPDX-License-Identifier: GPL-3.0-only

package store

// backup_helpers_test.go — общая оснастка тестов снимка и восстановления (T-83).

import (
	"context"
	"database/sql"
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/anatolijlaptev1991-ctrl/lavray-android/core/internal/model"
)

// rawOpen открывает файл БД «как есть», без миграций и проверок Open.
func rawOpen(t *testing.T, path string) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", plainDSN(path)+"?_pragma=foreign_keys(0)")
	if err != nil {
		t.Fatalf("rawOpen: %v", err)
	}
	db.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func mustExec(t *testing.T, db interface {
	Exec(string, ...any) (sql.Result, error)
}, q string, args ...any) {
	t.Helper()
	if _, err := db.Exec(q, args...); err != nil {
		t.Fatalf("%s: %v", q, err)
	}
}

// seed наполняет хранилище узнаваемыми данными: tag отличает «источник» от
// «получателя». Возвращает id созданной группы-подписки.
func seed(t *testing.T, s *Store, tag string) int64 {
	t.Helper()
	sub := &model.Group{Name: "Sub-" + tag, Kind: model.GroupSubscription,
		SubscriptionURL: "https://sub.example/" + tag + "?token=SUBTOKEN-" + tag}
	if err := s.CreateGroup(sub); err != nil {
		t.Fatal(err)
	}
	var firstProfile int64
	for i, host := range []string{"a." + tag + ".example", "b." + tag + ".example", "c." + tag + ".example"} {
		p := newProfile(sub.ID, model.TypeTrojan, fmt.Sprintf("srv-%s-%d", tag, i), host, 443+i)
		p.Detail = &model.TrojanVlessDetail{Password: "PROFILE-SECRET-" + tag}
		if err := s.CreateProfile(p); err != nil {
			t.Fatal(err)
		}
		if i == 0 {
			firstProfile = p.ID
		}
	}
	for k, v := range map[string]string{
		"inbound.port":      `2080`,
		"inbound.auth.pass": `"proxy-pass-` + tag + `"`,
		"ui.theme":          `"` + tag + `"`,
	} {
		if err := s.SetSetting(k, v); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.SetMeta(MetaCustomPresets, `[{"id":"mine-`+tag+`"},{"id":"mine2-`+tag+`"}]`); err != nil {
		t.Fatal(err)
	}
	sid, err := s.StartSession(&firstProfile, &sub.ID, "tun", 1000)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.EndSession(sid, 10, 20, 2000); err != nil {
		t.Fatal(err)
	}
	return sub.ID
}

// dumpTables возвращает по каждой таблице отсортированные строки-значения.
// schema_version не входит: восстановление её не переносит.
func dumpTables(t *testing.T, s *Store) map[string][]string {
	t.Helper()
	rows, err := s.db.Query(`SELECT name FROM sqlite_master WHERE type='table' AND name NOT LIKE 'sqlite\_%' ESCAPE '\' ORDER BY name`)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for rows.Next() {
		var n string
		if err := rows.Scan(&n); err != nil {
			t.Fatal(err)
		}
		names = append(names, n)
	}
	_ = rows.Close()
	out := map[string][]string{}
	for _, name := range names {
		if name == "schema_version" {
			continue
		}
		out[name] = dumpTable(t, s.db, name)
	}
	return out
}

func dumpTable(t *testing.T, db *sql.DB, name string) []string {
	t.Helper()
	rows, err := db.Query(`SELECT * FROM "` + name + `"`)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = rows.Close() }()
	cols, _ := rows.Columns()
	var lines []string
	for rows.Next() {
		vals := make([]any, len(cols))
		ptrs := make([]any, len(cols))
		for i := range vals {
			ptrs[i] = &vals[i]
		}
		if err := rows.Scan(ptrs...); err != nil {
			t.Fatal(err)
		}
		parts := make([]string, len(vals))
		for i, v := range vals {
			parts[i] = fmt.Sprint(v)
		}
		lines = append(lines, strings.Join(parts, "|"))
	}
	sort.Strings(lines)
	return lines
}

// filterRows оставляет строки, не содержащие ни одной из подстрок.
func filterRows(rows []string, drop ...string) []string {
	var out []string
	for _, r := range rows {
		keep := true
		for _, d := range drop {
			keep = keep && !strings.Contains(r, d)
		}
		if keep {
			out = append(out, r)
		}
	}
	return out
}

// snapshotOf снимает копию хранилища во временный файл.
func snapshotOf(t *testing.T, s *Store) string {
	t.Helper()
	dst := filepath.Join(mkTempDir(t), "snap.db")
	if err := s.Snapshot(context.Background(), dst); err != nil {
		t.Fatalf("Snapshot: %v", err)
	}
	return dst
}
