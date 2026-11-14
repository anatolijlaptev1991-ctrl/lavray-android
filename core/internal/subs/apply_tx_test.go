// SPDX-License-Identifier: GPL-3.0-only

package subs

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/anatolijlaptev1991-ctrl/lavray-android/core/internal/model"
	"github.com/anatolijlaptev1991-ctrl/lavray-android/core/internal/store"
)

func t51Trojan(name, host string) *model.Profile {
	return &model.Profile{
		Type: model.TypeTrojan, Name: name, Address: host, Port: 443,
		Detail: &model.TrojanVlessDetail{Password: "pw-" + host},
	}
}

// t51Dump — все строки profiles как есть (id, группа, порядок, JSON с
// updated_at_ms): сравнение «до» и «после» ловит любую запись.
func t51Dump(t *testing.T, st *store.Store) string {
	t.Helper()
	rows, err := st.DB().Query(`SELECT id, group_id, sort_key, data FROM profiles ORDER BY id`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var sb strings.Builder
	for rows.Next() {
		var id, gid, sk int64
		var data string
		if err := rows.Scan(&id, &gid, &sk, &data); err != nil {
			t.Fatal(err)
		}
		fmt.Fprintf(&sb, "%d|%d|%d|%s\n", id, gid, sk, data)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	return sb.String()
}

// t51Seed — группа-подписка и профили: name → (host, manual).
func t51Seed(t *testing.T, st *store.Store, specs ...[3]string) *model.Group {
	t.Helper()
	g := &model.Group{Name: "sub", Kind: model.GroupSubscription, SubscriptionURL: "https://example.com/sub"}
	if err := st.CreateGroup(g); err != nil {
		t.Fatal(err)
	}
	for _, sp := range specs { // {name, host, "manual"|""}
		p := t51Trojan(sp[0], sp[1])
		p.GroupID, p.Manual = g.ID, sp[2] == "manual"
		if err := st.CreateProfile(p); err != nil {
			t.Fatal(err)
		}
	}
	return g
}

// T-51 (A1-16, B1-27): сбой посреди Apply оставляет БД побайтно прежней.
// Раньше каждая запись была отдельной транзакцией, а откат переписывал строки
// из снимка (updated_at_ms и порядок другие); при kill между шагами группа
// оставалась смешанной. Сбой — триггер SQLite на профиль FAIL: он стоит
// последним, после обновления и вставки.
func TestApplyIsAtomicInOneTransaction(t *testing.T) {
	st := newTestStore(t)
	g := t51Seed(t, st, [3]string{"O1", "o1.example"}, [3]string{"O2", "o2.example"},
		[3]string{"O3", "o3.example"}, [3]string{"O4", "o4.example"})
	snap, err := TakeSnapshot(st, g.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.DB().Exec(`CREATE TRIGGER t51_fail BEFORE INSERT ON profiles
		WHEN NEW.name = 'FAIL' BEGIN SELECT RAISE(ABORT, 't51 injected failure'); END`); err != nil {
		t.Fatal(err)
	}
	before := t51Dump(t, st)
	time.Sleep(20 * time.Millisecond) // updated_at_ms любой перезаписи будет другим

	next := []*model.Profile{
		t51Trojan("O1-renamed", "o1.example"), // совпадает с O1 → обновление на месте
		t51Trojan("N1", "n1.example"),         // вставка
		t51Trojan("FAIL", "fail.example"),     // вставка падает
	}
	_, err = Apply(st, snap, ApplyParams{Next: next, Confirmed: true})
	if err == nil || !strings.Contains(err.Error(), "t51 injected failure") {
		t.Fatalf("Apply должен вернуть ошибку вставки, получено: %v", err)
	}
	if after := t51Dump(t, st); after != before {
		t.Fatalf("после неудачного Apply строки profiles изменились\n--- до ---\n%s--- после ---\n%s", before, after)
	}
}

// T-51 (B1-35): сервер, добавленный вручную во вкладку подписки, обновление
// не удаляет и не учитывает в пороге «удаление > 50 %».
func TestApplyKeepsManualProfiles(t *testing.T) {
	st := newTestStore(t)
	g := t51Seed(t, st, [3]string{"M1", "m1.example", ""}, [3]string{"M2", "m2.example", ""},
		[3]string{"X", "x.example", "manual"})
	snap, err := TakeSnapshot(st, g.ID)
	if err != nil {
		t.Fatal(err)
	}
	var manualID int64
	for _, p := range snap.Profiles {
		if p.Name == "X" {
			manualID = p.ID
		}
	}
	next := []*model.Profile{t51Trojan("N1", "n1.example"), t51Trojan("N2", "n2.example")}

	diff := ComputeDiff(snap.Profiles, next)
	if len(diff.Deleted) != 2 || diff.DeletionFraction != 1 {
		t.Fatalf("ручной сервер попал в удаляемые: Deleted=%d доля=%v (ожидалось 2 и 1.0)", len(diff.Deleted), diff.DeletionFraction)
	}
	if _, err := Apply(st, snap, ApplyParams{Next: next, Confirmed: true}); err != nil {
		t.Fatal(err)
	}
	list, _ := st.ListProfiles(g.ID)
	got := map[string]*model.Profile{}
	for _, p := range list {
		got[p.Name] = p
	}
	if got["X"] == nil || got["X"].ID != manualID || !got["X"].Manual {
		t.Fatalf("ручной сервер X удалён или пересоздан: %+v", got["X"])
	}
	if got["M1"] != nil || got["M2"] != nil || got["N1"] == nil || got["N2"] == nil || len(list) != 3 {
		t.Fatalf("неверный состав группы после обновления: %d профилей", len(list))
	}
}
