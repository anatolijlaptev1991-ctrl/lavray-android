// SPDX-License-Identifier: GPL-3.0-only

package store

// sessions_test.go — миграция v4 и таблица sessions (T-79).

import (
	"errors"
	"os"
	"sync"
	"testing"

	"github.com/anatolijlaptev1991-ctrl/lavray-android/core/internal/model"
)

func i64p(v int64) *int64 { return &v }

// groupAndProfile — группа и профиль для сессий.
func groupAndProfile(t *testing.T, s *Store) (*model.Group, *model.Profile) {
	t.Helper()
	g := newGroup("G", model.GroupBasic)
	if err := s.CreateGroup(g); err != nil {
		t.Fatal(err)
	}
	p := newProfile(g.ID, model.TypeTrojan, "srv", "1.1.1.1", 443)
	if err := s.CreateProfile(p); err != nil {
		t.Fatal(err)
	}
	return g, p
}

func TestMigrationV4CreatesSessions(t *testing.T) {
	s := openTemp(t)
	if v, err := s.SchemaVersion(); err != nil || v < 4 {
		t.Fatalf("версия схемы %d (err=%v), ожидалась не меньше 4", v, err)
	}
	cols := map[string]bool{}
	rows, err := s.db.Query(`SELECT name FROM pragma_table_info('sessions')`)
	if err != nil {
		t.Fatal(err)
	}
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			t.Fatal(err)
		}
		cols[name] = true
	}
	_ = rows.Close()
	for _, want := range []string{"id", "profile_id", "group_id", "mode", "started_at_ms", "ended_at_ms",
		"uplink_bytes", "downlink_bytes", "updated_at_ms"} {
		if !cols[want] {
			t.Errorf("в sessions нет колонки %q (есть %v)", want, cols)
		}
	}
	var idx int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE type = 'index' AND name = 'idx_sessions_group'`).Scan(&idx); err != nil || idx != 1 {
		t.Errorf("индекса idx_sessions_group нет (n=%d, err=%v)", idx, err)
	}
}

// База версии 3 (без sessions) обновляется до 4 при открытии, данные целы.
func TestMigrationV4UpgradesVersion3Database(t *testing.T) {
	dir, err := os.MkdirTemp("", "lavray-store-v3-*")
	if err != nil {
		t.Fatal(err)
	}
	s, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	_, p := groupAndProfile(t, s)
	for _, q := range []string{`DROP INDEX idx_sessions_group`, `DROP TABLE sessions`, `DROP TABLE web_users`, `DROP TABLE import_map`, `DELETE FROM schema_version WHERE version >= 4`} {
		if _, err := s.db.Exec(q); err != nil {
			t.Fatalf("%s: %v", q, err)
		}
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	s, err = Open(dir)
	if err != nil {
		t.Fatalf("повторное открытие базы версии 3: %v", err)
	}
	t.Cleanup(func() { _ = s.Close(); _ = os.RemoveAll(dir) })
	if v, _ := s.SchemaVersion(); v < 4 {
		t.Fatalf("версия схемы %d после открытия, ожидалась не меньше 4", v)
	}
	if _, err := s.GetProfile(p.ID); err != nil {
		t.Fatalf("профиль потерян при миграции: %v", err)
	}
	if _, err := s.StartSession(nil, nil, "x", 1); err != nil {
		t.Fatalf("sessions после миграции не работает: %v", err)
	}
}

func TestSessionLifecycle(t *testing.T) {
	s := openTemp(t)
	g, p := groupAndProfile(t, s)
	id, err := s.StartSession(i64p(p.ID), i64p(g.ID), "tun", 1000)
	if err != nil {
		t.Fatal(err)
	}
	got, err := s.GetSession(id)
	if err != nil {
		t.Fatal(err)
	}
	if got.EndedAtMs != nil || got.UplinkBytes != 0 || got.UpdatedAtMs != 1000 || got.Mode != "tun" ||
		got.ProfileID == nil || *got.ProfileID != p.ID || got.GroupID == nil || *got.GroupID != g.ID {
		t.Fatalf("новая сессия: %+v", got)
	}
	if err := s.UpdateSession(id, 10, 20, 2000); err != nil {
		t.Fatal(err)
	}
	if got, _ = s.GetSession(id); got.UplinkBytes != 10 || got.DownlinkBytes != 20 || got.UpdatedAtMs != 2000 || got.EndedAtMs != nil {
		t.Fatalf("после UpdateSession: %+v", got)
	}
	if err := s.EndSession(id, 15, 30, 3000); err != nil {
		t.Fatal(err)
	}
	if got, _ = s.GetSession(id); got.EndedAtMs == nil || *got.EndedAtMs != 3000 || got.UplinkBytes != 15 || got.DownlinkBytes != 30 {
		t.Fatalf("после EndSession: %+v", got)
	}
	// Повтор тех же чисел — не ошибка и ничего не меняет (идемпотентность).
	if err := s.EndSession(id, 15, 30, 3000); err != nil {
		t.Fatalf("повторный EndSession: %v", err)
	}
	if err := s.UpdateSession(9999, 1, 1, 1); !errors.Is(err, ErrNotFound) {
		t.Fatalf("несуществующая сессия: %v, ожидалась ErrNotFound", err)
	}
}

// Сессия без сохранённого профиля (inline) допустима: обе ссылки пусты.
func TestSessionWithoutProfile(t *testing.T) {
	s := openTemp(t)
	id, err := s.StartSession(nil, nil, "system_proxy", 5)
	if err != nil {
		t.Fatal(err)
	}
	got, _ := s.GetSession(id)
	if got.ProfileID != nil || got.GroupID != nil {
		t.Fatalf("ссылки должны быть пустыми: %+v", got)
	}
}

func TestCloseDanglingSessionsOnOpen(t *testing.T) {
	dir, err := os.MkdirTemp("", "lavray-store-dangling-*")
	if err != nil {
		t.Fatal(err)
	}
	s, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	open, _ := s.StartSession(nil, nil, "tun", 1000)
	_ = s.UpdateSession(open, 5, 6, 4000) // последний сброс — 4000
	done, _ := s.StartSession(nil, nil, "tun", 100)
	_ = s.EndSession(done, 1, 1, 200)
	if err := s.Close(); err != nil { // «процесс убили»: EndSession не вызван
		t.Fatal(err)
	}
	s, err = Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close(); _ = os.RemoveAll(dir) })
	if got, _ := s.GetSession(open); got.EndedAtMs == nil || *got.EndedAtMs != 4000 || got.UplinkBytes != 5 {
		t.Fatalf("повисшая сессия не закрыта по последнему сбросу: %+v", got)
	}
	if got, _ := s.GetSession(done); got.EndedAtMs == nil || *got.EndedAtMs != 200 {
		t.Fatalf("завершённая сессия изменена: %+v", got)
	}
	if n, err := s.CloseDanglingSessions(); err != nil || n != 0 {
		t.Fatalf("повторное закрытие: n=%d err=%v, ожидалось 0 (идемпотентно)", n, err)
	}
}

func TestSumSessionsSinceInclusive(t *testing.T) {
	s := openTemp(t)
	g, _ := groupAndProfile(t, s)
	other := newGroup("O", model.GroupBasic)
	if err := s.CreateGroup(other); err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct {
		group     int64
		start     int64
		up, down  int64
		noProfile bool
	}{{g.ID, 1000, 1, 10, false}, {g.ID, 2000, 2, 20, false}, {g.ID, 3000, 4, 40, false}, {other.ID, 2500, 8, 80, false}} {
		id, err := s.StartSession(nil, i64p(c.group), "tun", c.start)
		if err != nil {
			t.Fatal(err)
		}
		if err := s.EndSession(id, c.up, c.down, c.start+1); err != nil {
			t.Fatal(err)
		}
	}
	// Сессия без группы в сводку не входит.
	if id, err := s.StartSession(nil, nil, "tun", 2000); err == nil {
		_ = s.EndSession(id, 999, 999, 2001)
	}

	sum := func(group *int64, since int64) []GroupTraffic {
		t.Helper()
		out, err := s.SumSessions(group, since)
		if err != nil {
			t.Fatal(err)
		}
		return out
	}
	// since == начало сессии: сессия входит (граница включительно).
	got := sum(i64p(g.ID), 2000)
	if len(got) != 1 || got[0].SessionCount != 2 || got[0].UplinkBytes != 6 || got[0].DownlinkBytes != 60 ||
		got[0].FirstStartMs != 2000 || got[0].LastStartMs != 3000 {
		t.Fatalf("since=2000: %+v", got)
	}
	if got = sum(i64p(g.ID), 2001); len(got) != 1 || got[0].SessionCount != 1 || got[0].UplinkBytes != 4 {
		t.Fatalf("since=2001: %+v", got)
	}
	if got = sum(i64p(g.ID), 3001); len(got) != 0 {
		t.Fatalf("since позже всех сессий: %+v", got)
	}
	if got = sum(nil, 0); len(got) != 2 || got[0].GroupID != g.ID || got[1].GroupID != other.ID || got[0].SessionCount != 3 {
		t.Fatalf("все группы: %+v", got)
	}
}

func TestPruneKeepsNewest(t *testing.T) {
	s := openTemp(t)
	var ids []int64
	for _, start := range []int64{500, 100, 300, 200, 400} { // порядок вставки не совпадает с порядком времени
		id, err := s.StartSession(nil, nil, "tun", start)
		if err != nil {
			t.Fatal(err)
		}
		ids = append(ids, id)
	}
	if n, err := s.PruneSessions(10); err != nil || n != 0 {
		t.Fatalf("keep больше числа сессий: n=%d err=%v", n, err)
	}
	if n, err := s.PruneSessions(3); err != nil || n != 2 {
		t.Fatalf("удалено %d (err=%v), ожидалось 2", n, err)
	}
	for i, start := range []int64{500, 100, 300, 200, 400} {
		_, err := s.GetSession(ids[i])
		kept := err == nil
		if want := start >= 300; kept != want {
			t.Errorf("сессия с началом %d: сохранена=%v, ожидалось %v", start, kept, want)
		}
	}
	if _, err := s.PruneSessions(0); err == nil {
		t.Error("keep=0 должен быть ошибкой: иначе одной командой сотрётся вся история")
	}
}

// Удаление профиля не удаляет его сессии: ссылка обнуляется, сводка по группе цела.
func TestSessionSurvivesProfileDelete(t *testing.T) {
	s := openTemp(t)
	g, p := groupAndProfile(t, s)
	id, _ := s.StartSession(i64p(p.ID), i64p(g.ID), "tun", 1000)
	_ = s.EndSession(id, 7, 9, 2000)
	if err := s.DeleteProfile(p.ID); err != nil {
		t.Fatal(err)
	}
	got, err := s.GetSession(id)
	if err != nil {
		t.Fatalf("сессия пропала вместе с профилем: %v", err)
	}
	if got.ProfileID != nil || got.GroupID == nil {
		t.Fatalf("ожидалось profile_id=NULL, group_id сохранён: %+v", got)
	}
	if sum, _ := s.SumSessions(i64p(g.ID), 0); len(sum) != 1 || sum[0].UplinkBytes != 7 {
		t.Fatalf("сводка после удаления профиля: %+v", sum)
	}
}

func TestAddTrafficConcurrentExactSum(t *testing.T) {
	s := openTemp(t)
	_, p := groupAndProfile(t, s)
	const workers, perWorker = 16, 50
	var wg sync.WaitGroup
	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < perWorker; i++ {
				if err := s.AddTraffic(p.ID, 1, 2); err != nil {
					t.Error(err)
					return
				}
			}
		}()
	}
	wg.Wait()
	got, _ := s.GetProfile(p.ID)
	if want := int64(workers * perWorker); got.Traffic.UplinkBytes != want || got.Traffic.DownlinkBytes != 2*want {
		t.Fatalf("счётчики %+v, ожидалось ↑%d ↓%d", got.Traffic, want, 2*want)
	}
}
