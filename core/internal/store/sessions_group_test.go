// SPDX-License-Identifier: GPL-3.0-only

package store

// sessions_group_test.go — история трафика при удалении группы (T-79, ревью):
// groups.id без AUTOINCREMENT, и новая группа с тем же id не должна
// унаследовать сессии удалённой.

import (
	"testing"

	"github.com/anatolijlaptev1991-ctrl/lavray-android/core/internal/model"
)

func TestDeletedGroupHistoryNotInheritedByReusedID(t *testing.T) {
	s := openTemp(t)
	g, p := groupAndProfile(t, s)
	id, err := s.StartSession(i64p(p.ID), i64p(g.ID), "proxy", 1000)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.EndSession(id, 500, 900, 2000); err != nil {
		t.Fatal(err)
	}
	if err := s.DeleteGroup(g.ID); err != nil {
		t.Fatal(err)
	}
	got, err := s.GetSession(id)
	if err != nil {
		t.Fatalf("сессия пропала вместе с группой: %v", err)
	}
	if got.GroupID != nil || got.UplinkBytes != 500 {
		t.Fatalf("ожидалось group_id=NULL и сохранённые байты: %+v", got)
	}
	reused := newGroup("Новая", model.GroupBasic)
	if err := s.CreateGroup(reused); err != nil {
		t.Fatal(err)
	}
	if sum, _ := s.SumSessions(i64p(reused.ID), 0); len(sum) != 0 {
		t.Fatalf("группа %d (id удалённой %d?) унаследовала историю: %+v", reused.ID, g.ID, sum)
	}
	if sum, _ := s.SumSessions(nil, 0); len(sum) != 0 {
		t.Fatalf("сводка по всем группам показывает удалённую: %+v", sum)
	}
}

func TestStartSessionUnknownGroupRejected(t *testing.T) {
	s := openTemp(t)
	if _, err := s.StartSession(nil, i64p(424242), "proxy", 1000); err == nil {
		t.Fatal("сессия с несуществующей группой записана")
	}
}
