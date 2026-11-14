// SPDX-License-Identifier: GPL-3.0-only

package api

import (
	"context"
	"testing"

	"connectrpc.com/connect"

	lavrayv1 "github.com/anatolijlaptev1991-ctrl/lavray-android/core/internal/gen/lavrayv1"
	"github.com/anatolijlaptev1991-ctrl/lavray-android/core/internal/model"
)

func dedupe(t *testing.T, f *svcFixture, group int64, apply bool) *lavrayv1.DeduplicateResponse {
	t.Helper()
	res, err := f.srv.DeduplicateProfiles(context.Background(),
		newReq(&lavrayv1.DeduplicateProfilesRequest{GroupId: group, Apply: apply}))
	if err != nil {
		t.Fatalf("DeduplicateProfiles: %v", err)
	}
	return res.Msg
}

func protoIDs(list []*lavrayv1.Profile) []int64 {
	out := make([]int64, 0, len(list))
	for _, p := range list {
		out = append(out, p.Id)
	}
	return out
}

// chainMembers — звенья сохранённой цепочки.
func chainMembers(t *testing.T, f *svcFixture, id int64) []int64 {
	t.Helper()
	p, err := f.srv.st.GetProfile(id)
	if err != nil {
		t.Fatalf("цепочка %d: %v", id, err)
	}
	return p.Detail.(*model.ChainDetail).ProfileIDs
}

// Остаётся первый по порядку, остальные копии удаляются.
func TestDedupKeepsFirst(t *testing.T) {
	f := newSvcFixture(t, nil)
	a := f.addTrojan(t, "a", "x.example.org", 443, "pw")
	other := f.addTrojan(t, "other", "y.example.org", 443, "pw")
	b := f.addTrojan(t, "b", "x.example.org", 443, "pw")
	c := f.addTrojan(t, "c", "x.example.org", 443, "pw")

	got := dedupe(t, f, 1, true)
	if !sameIDs(got.RemovedIds, []int64{b.ID, c.ID}) || !sameIDs(protoIDs(got.Duplicates), []int64{b.ID, c.ID}) {
		t.Fatalf("removed=%v duplicates=%v", got.RemovedIds, protoIDs(got.Duplicates))
	}
	if want := []int64{a.ID, other.ID}; !sameIDs(f.ids(t), want) {
		t.Fatalf("остались %v, want %v", f.ids(t), want)
	}
}

// Имя, заметки и теги сервер другим не делают.
func TestDedupIgnoresName(t *testing.T) {
	f := newSvcFixture(t, nil)
	a := f.addTrojan(t, "NL-1", "x.example.org", 443, "pw")
	b := f.addTrojan(t, "совсем другое имя", "X.Example.ORG", 443, "pw")
	b.Notes, b.Tags, b.Favorite = "заметка", []string{"t"}, true
	if err := f.srv.st.UpdateProfile(b); err != nil {
		t.Fatal(err)
	}

	got := dedupe(t, f, 1, true)
	if !sameIDs(got.RemovedIds, []int64{b.ID}) {
		t.Fatalf("removed = %v, want [%d]", got.RemovedIds, b.ID)
	}
	if !sameIDs(f.ids(t), []int64{a.ID}) {
		t.Fatalf("остались %v", f.ids(t))
	}
}

// Тот же адрес и порт, но другие учётные данные или транспорт — разные серверы.
func TestDedupDifferentCredentialsNotDup(t *testing.T) {
	f := newSvcFixture(t, nil)
	f.addTrojan(t, "a", "x.example.org", 443, "pw1")
	f.addTrojan(t, "b", "x.example.org", 443, "pw2")
	ws := f.addTrojan(t, "c", "x.example.org", 443, "pw1")
	ws.Stream = &model.StreamSettings{Network: "ws", Path: "/ws"}
	if err := f.srv.st.UpdateProfile(ws); err != nil {
		t.Fatal(err)
	}

	got := dedupe(t, f, 1, true)
	if len(got.RemovedIds) != 0 || len(got.Duplicates) != 0 {
		t.Fatalf("удалены разные серверы: %v", got.RemovedIds)
	}
	if len(f.ids(t)) != 3 {
		t.Fatalf("осталось %d профилей, want 3", len(f.ids(t)))
	}
}

// Ссылки цепочек и front proxy группы переписываются на оставшийся профиль;
// цепочки, ставшие одинаковыми, схлопываются в той же операции.
func TestDedupRewritesChainRefs(t *testing.T) {
	f := newSvcFixture(t, nil)
	a := f.addTrojan(t, "a", "x.example.org", 443, "pw")
	b := f.addTrojan(t, "b", "x.example.org", 443, "pw")
	exit := f.addTrojan(t, "exit", "e.example.org", 443, "e")
	c1 := f.addChain(t, "c1", a.ID, exit.ID)
	c2 := f.addChain(t, "c2", b.ID, exit.ID)
	solo := f.addChain(t, "solo", b.ID)
	g, err := f.srv.st.GetGroup(1)
	if err != nil {
		t.Fatal(err)
	}
	g.FrontProxyID = &b.ID
	if err := f.srv.st.UpdateGroup(g); err != nil {
		t.Fatal(err)
	}

	got := dedupe(t, f, 1, true)
	if !sameIDs(got.RemovedIds, []int64{b.ID, c2.ID}) {
		t.Fatalf("removed = %v, want [%d %d]", got.RemovedIds, b.ID, c2.ID)
	}
	if m := chainMembers(t, f, c1.ID); !sameIDs(m, []int64{a.ID, exit.ID}) {
		t.Errorf("c1 = %v", m)
	}
	if m := chainMembers(t, f, solo.ID); !sameIDs(m, []int64{a.ID}) {
		t.Errorf("solo = %v, want [%d]", m, a.ID)
	}
	g, err = f.srv.st.GetGroup(1)
	if err != nil || g.FrontProxyID == nil || *g.FrontProxyID != a.ID {
		t.Errorf("front proxy группы = %v (err %v), want %d", g.FrontProxyID, err, a.ID)
	}
}

// Цепочка из другой группы тоже получает исправленную ссылку.
func TestDedupRewritesChainInOtherGroup(t *testing.T) {
	f := newSvcFixture(t, nil)
	if err := f.srv.st.CreateGroup(&model.Group{ID: 2, Name: "Two", Kind: model.GroupBasic}); err != nil {
		t.Fatal(err)
	}
	a := f.addTrojan(t, "a", "x.example.org", 443, "pw")
	b := f.addTrojan(t, "b", "x.example.org", 443, "pw")
	remote := &model.Profile{GroupID: 2, Type: model.TypeChain, Name: "remote",
		Detail: &model.ChainDetail{ProfileIDs: []int64{b.ID}}}
	if err := f.srv.st.CreateProfile(remote); err != nil {
		t.Fatal(err)
	}

	dedupe(t, f, 1, true)
	if m := chainMembers(t, f, remote.ID); !sameIDs(m, []int64{a.ID}) {
		t.Fatalf("remote = %v, want [%d]", m, a.ID)
	}
}

// Повторный запуск ничего не находит.
func TestDedupIdempotent(t *testing.T) {
	f := newSvcFixture(t, nil)
	a := f.addTrojan(t, "a", "x.example.org", 443, "pw")
	b := f.addTrojan(t, "b", "x.example.org", 443, "pw")
	f.addChain(t, "c1", a.ID)
	f.addChain(t, "c2", b.ID)

	first := dedupe(t, f, 1, true)
	if len(first.RemovedIds) != 2 {
		t.Fatalf("первый проход удалил %v, want 2 профиля", first.RemovedIds)
	}
	before := f.ids(t)
	second := dedupe(t, f, 1, true)
	if len(second.RemovedIds) != 0 || len(second.Duplicates) != 0 {
		t.Fatalf("второй проход нашёл %v", second.RemovedIds)
	}
	if !sameIDs(f.ids(t), before) {
		t.Fatalf("второй проход изменил группу: %v -> %v", before, f.ids(t))
	}
}

// Пробный прогон показывает те же дубликаты, но ничего не пишет.
func TestDedupDryRunWritesNothing(t *testing.T) {
	f := newSvcFixture(t, nil)
	a := f.addTrojan(t, "a", "x.example.org", 443, "pw")
	b := f.addTrojan(t, "b", "x.example.org", 443, "pw")
	ch := f.addChain(t, "c", b.ID)

	got := dedupe(t, f, 1, false)
	if !sameIDs(protoIDs(got.Duplicates), []int64{b.ID}) || len(got.RemovedIds) != 0 {
		t.Fatalf("duplicates=%v removed=%v", protoIDs(got.Duplicates), got.RemovedIds)
	}
	if !sameIDs(f.ids(t), []int64{a.ID, b.ID, ch.ID}) {
		t.Fatalf("пробный прогон изменил группу: %v", f.ids(t))
	}
	if m := chainMembers(t, f, ch.ID); !sameIDs(m, []int64{b.ID}) {
		t.Fatalf("пробный прогон переписал цепочку: %v", m)
	}
}

// Активный профиль не удаляется: если он копия, вместо него уходит другой.
func TestDedupActiveNotTouched(t *testing.T) {
	f := newSvcFixture(t, nil)
	a := f.addTrojan(t, "a", "x.example.org", 443, "pw")
	b := f.addTrojan(t, "b", "x.example.org", 443, "pw")
	f.activate(t, b.ID)

	got := dedupe(t, f, 1, true)
	if !sameIDs(got.RemovedIds, []int64{a.ID}) {
		t.Fatalf("removed = %v, want [%d]", got.RemovedIds, a.ID)
	}
	if !sameIDs(f.ids(t), []int64{b.ID}) {
		t.Fatalf("остались %v, want [%d]", f.ids(t), b.ID)
	}
}

// Группы друг с другом не сравниваются.
func TestDedupWithinGroupOnly(t *testing.T) {
	f := newSvcFixture(t, nil)
	if err := f.srv.st.CreateGroup(&model.Group{ID: 2, Name: "Two", Kind: model.GroupBasic}); err != nil {
		t.Fatal(err)
	}
	f.addTrojan(t, "a", "x.example.org", 443, "pw")
	twin := &model.Profile{GroupID: 2, Type: model.TypeTrojan, Name: "twin", Address: "x.example.org", Port: 443,
		Detail: &model.TrojanVlessDetail{Password: "pw"}}
	if err := f.srv.st.CreateProfile(twin); err != nil {
		t.Fatal(err)
	}
	if got := dedupe(t, f, 1, true); len(got.RemovedIds) != 0 {
		t.Fatalf("удалён профиль из другой группы: %v", got.RemovedIds)
	}
	if _, err := f.srv.st.GetProfile(twin.ID); err != nil {
		t.Fatal(err)
	}
}

// Несуществующая группа — NOT_FOUND.
func TestDedupUnknownGroup(t *testing.T) {
	f := newSvcFixture(t, nil)
	_, err := f.srv.DeduplicateProfiles(context.Background(),
		newReq(&lavrayv1.DeduplicateProfilesRequest{GroupId: 99, Apply: true}))
	if connect.CodeOf(mapErr(err)) != connect.CodeNotFound {
		t.Fatalf("code = %v, err = %v", connect.CodeOf(mapErr(err)), err)
	}
}
