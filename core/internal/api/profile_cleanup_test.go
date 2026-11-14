// SPDX-License-Identifier: GPL-3.0-only

package api

import (
	"context"
	"testing"

	"connectrpc.com/connect"

	lavrayv1 "github.com/anatolijlaptev1991-ctrl/lavray-android/core/internal/gen/lavrayv1"
	"github.com/anatolijlaptev1991-ctrl/lavray-android/core/internal/model"
)

func removeUnavailable(t *testing.T, f *svcFixture, group int64, apply bool) *lavrayv1.RemoveUnavailableResponse {
	t.Helper()
	res, err := f.srv.RemoveUnavailableProfiles(context.Background(),
		newReq(&lavrayv1.RemoveUnavailableProfilesRequest{GroupId: group, Apply: apply}))
	if err != nil {
		t.Fatalf("RemoveUnavailableProfiles: %v", err)
	}
	return res.Msg
}

// Удаляются только профили с latency_ms < 0; живые и нулевые остаются.
func TestRemoveUnavailableOnlyNegative(t *testing.T) {
	f := newSvcFixture(t, nil)
	dead := f.addTrojan(t, "dead", "a.example.org", 443, "1")
	dead2 := f.addTrojan(t, "dead2", "b.example.org", 443, "2")
	alive := f.addTrojan(t, "alive", "c.example.org", 443, "3")
	zero := f.addTrojan(t, "zero", "d.example.org", 443, "4")
	f.setLatency(t, dead.ID, -1)
	f.setLatency(t, dead2.ID, -1)
	f.setLatency(t, alive.ID, 120)
	f.setLatency(t, zero.ID, 0)

	got := removeUnavailable(t, f, 1, true)
	if !sameIDs(got.RemovedIds, []int64{dead.ID, dead2.ID}) {
		t.Fatalf("removed = %v, want [%d %d]", got.RemovedIds, dead.ID, dead2.ID)
	}
	if want := []int64{alive.ID, zero.ID}; !sameIDs(f.ids(t), want) {
		t.Fatalf("остались %v, want %v", f.ids(t), want)
	}
}

// Активный недоступный профиль не удаляется и попадает в skipped_ids.
func TestRemoveUnavailableSkipsActive(t *testing.T) {
	f := newSvcFixture(t, nil)
	active := f.addTrojan(t, "active", "a.example.org", 443, "1")
	dead := f.addTrojan(t, "dead", "b.example.org", 443, "2")
	f.setLatency(t, active.ID, -1)
	f.setLatency(t, dead.ID, -1)
	f.activate(t, active.ID)

	got := removeUnavailable(t, f, 1, true)
	if !sameIDs(got.RemovedIds, []int64{dead.ID}) || !sameIDs(got.SkippedIds, []int64{active.ID}) {
		t.Fatalf("removed=%v skipped=%v", got.RemovedIds, got.SkippedIds)
	}
	if _, err := f.srv.st.GetProfile(active.ID); err != nil {
		t.Fatalf("активный профиль удалён: %v", err)
	}
}

// Звено цепочки (из любой группы) и front proxy группы не удаляются.
func TestRemoveUnavailableSkipsChainReferenced(t *testing.T) {
	f := newSvcFixture(t, nil)
	member := f.addTrojan(t, "member", "a.example.org", 443, "1")
	front := f.addTrojan(t, "front", "f.example.org", 443, "5")
	free := f.addTrojan(t, "free", "b.example.org", 443, "2")
	for _, id := range []int64{member.ID, front.ID, free.ID} {
		f.setLatency(t, id, -1)
	}
	f.addChain(t, "chain", member.ID)
	g, err := f.srv.st.GetGroup(1)
	if err != nil {
		t.Fatal(err)
	}
	g.FrontProxyID = &front.ID
	if err := f.srv.st.UpdateGroup(g); err != nil {
		t.Fatal(err)
	}

	got := removeUnavailable(t, f, 1, true)
	if !sameIDs(got.RemovedIds, []int64{free.ID}) {
		t.Fatalf("removed = %v, want [%d]", got.RemovedIds, free.ID)
	}
	if !sameIDs(got.SkippedIds, []int64{member.ID, front.ID}) {
		t.Fatalf("skipped = %v, want [%d %d]", got.SkippedIds, member.ID, front.ID)
	}
}

// Непроверенные (latency не задана) остаются: об их доступности ничего не известно.
func TestRemoveUnavailableUntestedKept(t *testing.T) {
	f := newSvcFixture(t, nil)
	untested := f.addTrojan(t, "untested", "a.example.org", 443, "1")

	got := removeUnavailable(t, f, 1, true)
	if len(got.RemovedIds) != 0 || len(got.WouldRemoveIds) != 0 || len(got.SkippedIds) != 0 {
		t.Fatalf("ответ не пуст: %v", got)
	}
	if !sameIDs(f.ids(t), []int64{untested.ID}) {
		t.Fatalf("профиль удалён: %v", f.ids(t))
	}
}

// Пробный прогон перечисляет профили, но ничего не пишет.
func TestRemoveUnavailableDryRunWritesNothing(t *testing.T) {
	f := newSvcFixture(t, nil)
	dead := f.addTrojan(t, "dead", "a.example.org", 443, "1")
	f.setLatency(t, dead.ID, -1)

	got := removeUnavailable(t, f, 1, false)
	if !sameIDs(got.WouldRemoveIds, []int64{dead.ID}) || len(got.RemovedIds) != 0 {
		t.Fatalf("would=%v removed=%v", got.WouldRemoveIds, got.RemovedIds)
	}
	if !sameIDs(f.ids(t), []int64{dead.ID}) {
		t.Fatalf("пробный прогон удалил профиль: %v", f.ids(t))
	}
	// Тот же запрос с apply=true удаляет ровно то, что обещал пробный прогон.
	if applied := removeUnavailable(t, f, 1, true); !sameIDs(applied.RemovedIds, got.WouldRemoveIds) {
		t.Fatalf("applied %v != would %v", applied.RemovedIds, got.WouldRemoveIds)
	}
}

// Очистка касается только своей группы; чужая группа не затрагивается.
func TestRemoveUnavailableOtherGroupUntouched(t *testing.T) {
	f := newSvcFixture(t, nil)
	if err := f.srv.st.CreateGroup(&model.Group{ID: 2, Name: "Two", Kind: model.GroupBasic}); err != nil {
		t.Fatal(err)
	}
	other := &model.Profile{GroupID: 2, Type: model.TypeTrojan, Name: "o", Address: "o.example.org", Port: 443,
		Detail: &model.TrojanVlessDetail{Password: "x"}}
	if err := f.srv.st.CreateProfile(other); err != nil {
		t.Fatal(err)
	}
	f.setLatency(t, other.ID, -1)
	mine := f.addTrojan(t, "mine", "m.example.org", 443, "y")
	f.setLatency(t, mine.ID, -1)

	got := removeUnavailable(t, f, 1, true)
	if !sameIDs(got.RemovedIds, []int64{mine.ID}) {
		t.Fatalf("removed = %v", got.RemovedIds)
	}
	if _, err := f.srv.st.GetProfile(other.ID); err != nil {
		t.Fatalf("профиль чужой группы удалён: %v", err)
	}
}

// Несуществующая группа — NOT_FOUND.
func TestRemoveUnavailableUnknownGroup(t *testing.T) {
	f := newSvcFixture(t, nil)
	_, err := f.srv.RemoveUnavailableProfiles(context.Background(),
		newReq(&lavrayv1.RemoveUnavailableProfilesRequest{GroupId: 99, Apply: true}))
	if connect.CodeOf(mapErr(err)) != connect.CodeNotFound {
		t.Fatalf("code = %v, err = %v", connect.CodeOf(mapErr(err)), err)
	}
}

// Неудачный тест задержки сохраняет только время и ошибку (test_instance.go):
// такой профиль недоступен. Прерванный тест (без времени) — нет.
func TestRemoveUnavailableCountsFailedTest(t *testing.T) {
	f := newSvcFixture(t, nil)
	failed := f.addTrojan(t, "failed", "a.example.org", 443, "1")
	cancelled := f.addTrojan(t, "cancelled", "b.example.org", 443, "2")
	now := int64(1_700_000_000_000)
	if err := f.srv.st.SaveTestResult(failed.ID, model.TestResult{TestedAtMs: &now, LastError: "dial tcp: timeout"}); err != nil {
		t.Fatal(err)
	}
	if err := f.srv.st.SaveTestResult(cancelled.ID, model.TestResult{LastError: "context canceled"}); err != nil {
		t.Fatal(err)
	}

	got := removeUnavailable(t, f, 1, false)
	if !sameIDs(got.WouldRemoveIds, []int64{failed.ID}) {
		t.Fatalf("would_remove = %v, want [%d]", got.WouldRemoveIds, failed.ID)
	}
}
