// SPDX-License-Identifier: GPL-3.0-only

package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/anatolijlaptev1991-ctrl/lavray-android/core/internal/model"
)

var bg = context.Background()

// pair — источник с данными «A», получатель с данными «B» и копия источника.
func pair(t *testing.T) (src, dst *Store, snap string) {
	t.Helper()
	src, dst = openTemp(t), openTemp(t)
	seed(t, src, "A")
	seed(t, dst, "B")
	return src, dst, snapshotOf(t, src)
}

func equalDump(t *testing.T, what string, got, want map[string][]string) {
	t.Helper()
	if !reflect.DeepEqual(got, want) {
		for table := range want {
			if !reflect.DeepEqual(got[table], want[table]) {
				t.Errorf("%s: таблица %s:\n got %q\nwant %q", what, table, got[table], want[table])
			}
		}
		for table := range got {
			if _, ok := want[table]; !ok {
				t.Errorf("%s: лишняя таблица %s", what, table)
			}
		}
	}
}

func TestRestoreReplacesAllTables(t *testing.T) {
	src, dst, snap := pair(t)
	if err := dst.SetSetting("update.serial_stable", `9`); err != nil {
		t.Fatal(err)
	}
	if err := dst.SetMeta("cache.geo", "live-cache"); err != nil {
		t.Fatal(err)
	}
	if err := dst.Restore(bg, snap); err != nil {
		t.Fatalf("Restore: %v", err)
	}
	want := dumpTables(t, src)
	got := dumpTables(t, dst)
	// живые update.* и кэши переживают восстановление, остальное — из копии
	want["settings"] = append(want["settings"], fmt.Sprintf("update.serial_stable|9|%d", mustUpdatedAt(t, dst, "update.serial_stable")))
	want["meta"] = append(want["meta"], "cache.geo|live-cache")
	for _, k := range []string{"settings", "meta"} {
		sort.Strings(want[k])
	}
	equalDump(t, "после Restore", got, want)
}

func mustUpdatedAt(t *testing.T, s *Store, key string) int64 {
	t.Helper()
	var ms int64
	if err := s.db.QueryRow(`SELECT updated_at_ms FROM settings WHERE key = ?`, key).Scan(&ms); err != nil {
		t.Fatal(err)
	}
	return ms
}

// TestRestoreKeepsUpdateSerial — S: восстановление не откатывает защиту от
// downgrade, даже если в чужом файле лежат свои update.*.
func TestRestoreKeepsUpdateSerial(t *testing.T) {
	_, dst, snap := pair(t)
	if err := dst.SetSetting("update.serial_stable", `42`); err != nil {
		t.Fatal(err)
	}
	if err := dst.SetSetting("update.last_url", `"https://live.example/"`); err != nil {
		t.Fatal(err)
	}
	// «враждебная» копия: update.serial_stable=1 вписан в файл вручную
	raw := rawOpen(t, snap)
	mustExec(t, raw, `INSERT INTO settings(key,value,updated_at_ms) VALUES('update.serial_stable','1',1),('update.extra','1',1)`)
	_ = raw.Close()

	if err := dst.Restore(bg, snap); err != nil {
		t.Fatal(err)
	}
	if v, _, _ := dst.GetSetting("update.serial_stable"); v != "42" {
		t.Errorf("update.serial_stable = %q, ждём 42", v)
	}
	if v, _, _ := dst.GetSetting("update.last_url"); v != `"https://live.example/"` {
		t.Errorf("update.last_url = %q", v)
	}
	if _, ok, _ := dst.GetSetting("update.extra"); ok {
		t.Error("update.extra из чужого файла попал в БД")
	}
	if v, _, _ := dst.GetSetting("ui.theme"); v != `"A"` {
		t.Errorf("обычная настройка не восстановлена: %q", v)
	}
}

func TestRestoreRejectsNewerSchema(t *testing.T) {
	_, dst, snap := pair(t)
	before := dumpTables(t, dst)
	raw := rawOpen(t, snap)
	mustExec(t, raw, `INSERT INTO schema_version(version, applied_at_ms) VALUES (?, 1)`, migrations[len(migrations)-1].version+1)
	_ = raw.Close()

	err := dst.Restore(bg, snap)
	var tooNew *SchemaTooNewError
	if !errors.Is(err, ErrSchemaTooNew) || !errors.As(err, &tooNew) {
		t.Fatalf("err = %v, ждём SchemaTooNewError", err)
	}
	equalDump(t, "после отказа", dumpTables(t, dst), before)
}

// TestRestoreMigratesOlderSchema — B: копия схемы v2 поднимается до текущей и
// восстанавливается; в получателе схема не меняется.
func TestRestoreMigratesOlderSchema(t *testing.T) {
	dst := openTemp(t)
	seed(t, dst, "B")
	versionBefore, _ := dst.SchemaVersion()

	old := filepath.Join(mkTempDir(t), "old.db")
	db := rawOpen(t, old)
	mustExec(t, db, `CREATE TABLE schema_version (version INTEGER PRIMARY KEY, applied_at_ms INTEGER NOT NULL)`)
	for _, m := range migrations[:2] {
		for _, stmt := range m.stmts {
			mustExec(t, db, stmt)
		}
		mustExec(t, db, `INSERT INTO schema_version VALUES (?, 1)`, m.version)
	}
	mustExec(t, db, `INSERT INTO groups(id,kind,name,tab_order,created_at_ms,updated_at_ms) VALUES (7,1,'Старая',1,1,1)`)
	oldProfile := newProfile(7, model.TypeTrojan, "old-srv", "old.example", 443)
	oldProfile.ID = 70
	data, err := json.Marshal(oldProfile)
	if err != nil {
		t.Fatal(err)
	}
	mustExec(t, db, `INSERT INTO profiles(id,group_id,type,name,address,port,sort_key,data,created_at_ms,updated_at_ms)
		VALUES (70,7,'trojan','old-srv','old.example',443,1024,?,1,1)`, string(data))
	mustExec(t, db, `INSERT INTO settings VALUES ('ui.theme','"old"',1)`)
	_ = db.Close()

	if err := dst.Restore(bg, old); err != nil {
		t.Fatalf("Restore v2: %v", err)
	}
	if v, _ := dst.SchemaVersion(); v != versionBefore {
		t.Errorf("версия схемы получателя %d → %d", versionBefore, v)
	}
	p, err := dst.GetProfile(70)
	if err != nil || p.Name != "old-srv" {
		t.Fatalf("профиль из v2: %v, %v", p, err)
	}
	if v, _, _ := dst.GetSetting("ui.theme"); v != `"old"` {
		t.Errorf("ui.theme = %q", v)
	}
	if _, err := dst.GetGroup(7); err != nil {
		t.Errorf("группа из v2: %v", err)
	}
}

// TestRestoreIsAtomicOnCorruptFile — R: мусор, обрезанная БД и чужая БД SQLite
// отвергаются, данные получателя остаются как были.
func TestRestoreIsAtomicOnCorruptFile(t *testing.T) {
	_, dst, snap := pair(t)
	before := dumpTables(t, dst)
	good, err := os.ReadFile(snap)
	if err != nil {
		t.Fatal(err)
	}
	foreign := filepath.Join(mkTempDir(t), "foreign.db") // валидная SQLite без таблиц LAVRay
	fdb := rawOpen(t, foreign)
	mustExec(t, fdb, `CREATE TABLE notes (id INTEGER PRIMARY KEY, body TEXT)`)
	_ = fdb.Close()

	write := func(name string, data []byte) string {
		p := filepath.Join(mkTempDir(t), name)
		if err := os.WriteFile(p, data, 0o600); err != nil {
			t.Fatal(err)
		}
		return p
	}
	cases := map[string]string{
		"мусор":        write("garbage.db", []byte("это не база данных SQLite, просто текст на несколько строк")),
		"пустой файл":  write("empty.db", nil),
		"обрезан":      write("cut.db", good[:len(good)/2]),
		"чужая SQLite": foreign,
		"нет файла":    filepath.Join(mkTempDir(t), "missing.db"),
	}
	for name, path := range cases {
		if err := dst.Restore(bg, path); err == nil {
			t.Errorf("%s: Restore принял файл", name)
		}
		equalDump(t, name, dumpTables(t, dst), before)
	}
	// после неудач хранилище исправно, а временные каталоги убраны
	if err := dst.Restore(bg, snap); err != nil {
		t.Fatalf("Restore хорошей копии после отказов: %v", err)
	}
	assertNoRestoreDirs(t, dst)
}

func assertNoRestoreDirs(t *testing.T, s *Store) {
	t.Helper()
	ents, _ := os.ReadDir(s.dir)
	for _, e := range ents {
		if strings.HasPrefix(e.Name(), "restore-") {
			t.Errorf("остался временный каталог %s", e.Name())
		}
	}
}

// TestRestoreRollbackMidway — R: сбой посреди переноса откатывает ВСЕ таблицы.
func TestRestoreRollbackMidway(t *testing.T) {
	_, dst, snap := pair(t)
	before := dumpTables(t, dst)
	calls := 0
	old := afterTableRestored
	afterTableRestored = func(table string) error {
		calls++
		if calls == 3 {
			return fmt.Errorf("сбой на таблице %s", table)
		}
		return nil
	}
	t.Cleanup(func() { afterTableRestored = old })

	err := dst.Restore(bg, snap)
	if err == nil || !strings.Contains(err.Error(), "сбой на таблице") {
		t.Fatalf("err = %v", err)
	}
	if calls != 3 {
		t.Fatalf("перенос не дошёл до сбоя: вызовов %d", calls)
	}
	equalDump(t, "после отката", dumpTables(t, dst), before)

	afterTableRestored = old
	if err := dst.Restore(bg, snap); err != nil {
		t.Fatalf("повторный Restore: %v", err)
	}
}

func TestRestoreForeignKeysValid(t *testing.T) {
	src, dst, snap := pair(t)
	if err := dst.Restore(bg, snap); err != nil {
		t.Fatal(err)
	}
	rows, err := dst.db.Query(`PRAGMA foreign_key_check`)
	if err != nil {
		t.Fatal(err)
	}
	if rows.Next() {
		t.Error("после Restore есть нарушения внешних ключей")
	}
	_ = rows.Close()
	equalSrc, equalDst := dumpTables(t, src)["profiles"], dumpTables(t, dst)["profiles"]
	if !reflect.DeepEqual(equalSrc, equalDst) {
		t.Error("профили не совпали")
	}

	// копия с «висячей» ссылкой отвергается целиком
	bad := snapshotOf(t, src)
	raw := rawOpen(t, bad)
	mustExec(t, raw, `INSERT INTO profiles(id,group_id,type,sort_key,data,created_at_ms,updated_at_ms) VALUES (999999,424242,'trojan',1,'{}',1,1)`)
	_ = raw.Close()
	before := dumpTables(t, dst)
	if err := dst.Restore(bg, bad); err == nil {
		t.Fatal("копия с нарушенным внешним ключом принята")
	}
	equalDump(t, "после отказа по FK", dumpTables(t, dst), before)
}

// TestRestoreCoversEveryTable — D: таблицы, добавленные поздними миграциями
// (в том числе с AUTOINCREMENT), переносятся без правок кода восстановления.
func TestRestoreCoversEveryTable(t *testing.T) {
	old := migrations
	t.Cleanup(func() { migrations = old })
	migrations = append(append([]migration{}, old...), migration{
		version: old[len(old)-1].version + 1,
		stmts: []string{
			`CREATE TABLE zz_future (id INTEGER PRIMARY KEY AUTOINCREMENT, owner INTEGER REFERENCES groups(id) ON DELETE CASCADE, note TEXT NOT NULL)`,
			`CREATE TABLE zz_plain (k TEXT PRIMARY KEY, v BLOB)`,
		},
	})
	src, dst := openTemp(t), openTemp(t)
	seed(t, src, "A")
	seed(t, dst, "B")
	for _, s := range []*Store{src, dst} {
		g, err := s.ListGroups()
		if err != nil || len(g) == 0 {
			t.Fatal(err)
		}
		for i := 0; i < 3; i++ {
			mustExec(t, s.db, `INSERT INTO zz_future(owner, note) VALUES (?, ?)`, g[0].ID, "note-"+strconv.Itoa(i))
		}
	}
	mustExec(t, src.db, `INSERT INTO zz_plain VALUES ('k','v-from-src')`)
	snap := snapshotOf(t, src)
	mustExec(t, dst.db, `INSERT INTO zz_plain VALUES ('other','v-from-dst')`)
	// счётчик AUTOINCREMENT получателя больше, чем у копии
	for i := 0; i < 7; i++ {
		mustExec(t, dst.db, `INSERT INTO zz_future(owner, note) VALUES (NULL, 'extra')`)
	}
	mustExec(t, dst.db, `DELETE FROM zz_future`)

	if err := dst.Restore(bg, snap); err != nil {
		t.Fatalf("Restore: %v", err)
	}
	got, want := dumpTables(t, dst), dumpTables(t, src)
	for _, table := range []string{"zz_future", "zz_plain", "groups", "profiles", "sessions"} {
		if len(want[table]) == 0 {
			t.Fatalf("тест не наполнил %s", table)
		}
		if !reflect.DeepEqual(got[table], want[table]) {
			t.Errorf("таблица %s: got %q want %q", table, got[table], want[table])
		}
	}
	// счётчик AUTOINCREMENT тоже из копии: следующая строка не занимает id источника
	mustExec(t, dst.db, `INSERT INTO zz_future(owner, note) VALUES (NULL, 'after')`)
	var id int64
	if err := dst.db.QueryRow(`SELECT id FROM zz_future WHERE note = 'after'`).Scan(&id); err != nil || id != 4 {
		t.Errorf("следующий id = %d, %v; ждём 4", id, err)
	}
}

// TestRestoreIsIdempotent — D: повторное восстановление той же копии даёт то же
// состояние.
func TestRestoreIsIdempotent(t *testing.T) {
	_, dst, snap := pair(t)
	if err := dst.Restore(bg, snap); err != nil {
		t.Fatal(err)
	}
	first := dumpTables(t, dst)
	if err := dst.Restore(bg, snap); err != nil {
		t.Fatal(err)
	}
	equalDump(t, "второй Restore", dumpTables(t, dst), first)
}

func TestRestoreCancelledContext(t *testing.T) {
	_, dst, snap := pair(t)
	before := dumpTables(t, dst)
	ctx, cancel := context.WithCancel(bg)
	cancel()
	if err := dst.Restore(ctx, snap); err == nil {
		t.Fatal("Restore с отменённым контекстом не вернул ошибку")
	}
	equalDump(t, "после отмены", dumpTables(t, dst), before)
	assertNoRestoreDirs(t, dst)
}

func TestInspectCopyCounts(t *testing.T) {
	src, dst, snap := pair(t)
	info, err := dst.InspectCopy(bg, snap)
	if err != nil {
		t.Fatal(err)
	}
	want, err := src.Stats(bg)
	if err != nil {
		t.Fatal(err)
	}
	if info.Stats != want {
		t.Errorf("Stats копии %+v, источника %+v", info.Stats, want)
	}
	// Default + Sub-A; три профиля; одна подписка; два набора; три настройки
	if want.Groups != 2 || want.Profiles != 3 || want.Subscriptions != 1 || want.Presets != 2 || want.Settings != 3 {
		t.Errorf("Stats = %+v", want)
	}
	if v, _ := src.SchemaVersion(); info.SchemaVersion != v {
		t.Errorf("SchemaVersion копии %d, ждём %d", info.SchemaVersion, v)
	}
	// InspectCopy ничего не меняет у получателя
	assertNoRestoreDirs(t, dst)
	if v, _, _ := dst.GetSetting("ui.theme"); v != `"B"` {
		t.Errorf("InspectCopy изменил данные: %q", v)
	}
}
