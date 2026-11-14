// SPDX-License-Identifier: GPL-3.0-only

package api

import (
	"context"
	"testing"

	"connectrpc.com/connect"
	lavrayv1 "github.com/anatolijlaptev1991-ctrl/lavray-android/core/internal/gen/lavrayv1"
	"github.com/anatolijlaptev1991-ctrl/lavray-android/core/internal/model"
	"github.com/anatolijlaptev1991-ctrl/lavray-android/core/internal/store"
)

const importGroupLink = "trojan://pass@example.org:443#T07"

// T-07 (B1-03): диалог импорта из «Подключения», меню титлбара и онбординга
// не знает группу и раньше слал target_group_id=0 → invalid_argument.
// Теперь отсутствующая и нулевая группа — это встроенная «Default».
func TestImportContentWithoutGroupGoesToDefault(t *testing.T) {
	cases := []struct {
		name string
		gid  *int64
	}{
		{"поле target_group_id не задано", nil},
		{"target_group_id = 0", int64p(0)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cs := newE2E(t)
			res, err := cs.profile.ImportContent(context.Background(), connect.NewRequest(&lavrayv1.ImportContentRequest{
				Content:       importGroupLink,
				TargetGroupId: tc.gid,
			}))
			if err != nil {
				t.Fatalf("ImportContent: %v", err)
			}
			if len(res.Msg.AddedProfileIds) != 1 {
				t.Fatalf("added = %v, ожидался один профиль", res.Msg.AddedProfileIds)
			}
			p, err := cs.st.GetProfile(res.Msg.AddedProfileIds[0])
			if err != nil {
				t.Fatalf("GetProfile: %v", err)
			}
			if p.GroupID != store.DefaultGroupID {
				t.Fatalf("group_id = %d, ожидалась Default (%d)", p.GroupID, store.DefaultGroupID)
			}
		})
	}
}

// Явная положительная группа по-прежнему уважается и проверяется;
// отрицательный id — ошибка клиента, а не «Default».
func TestImportContentExplicitGroup(t *testing.T) {
	cs := newE2E(t)
	ctx := context.Background()

	res, err := cs.profile.ImportContent(ctx, connect.NewRequest(&lavrayv1.ImportContentRequest{
		Content:       importGroupLink,
		TargetGroupId: int64p(1),
	}))
	if err != nil {
		t.Fatalf("импорт в группу 1: %v", err)
	}
	if len(res.Msg.AddedProfileIds) != 1 {
		t.Fatalf("added = %v, ожидался один профиль", res.Msg.AddedProfileIds)
	}
	if p, _ := cs.st.GetProfile(res.Msg.AddedProfileIds[0]); p == nil || p.GroupID != 1 {
		t.Fatalf("профиль не в группе 1: %+v", p)
	}

	_, err = cs.profile.ImportContent(ctx, connect.NewRequest(&lavrayv1.ImportContentRequest{
		Content:       importGroupLink,
		TargetGroupId: int64p(987654),
	}))
	if connect.CodeOf(err) != connect.CodeNotFound {
		t.Fatalf("несуществующая группа: code=%v err=%v", connect.CodeOf(err), err)
	}

	_, err = cs.profile.ImportContent(ctx, connect.NewRequest(&lavrayv1.ImportContentRequest{
		Content:       importGroupLink,
		TargetGroupId: int64p(-5),
	}))
	if connect.CodeOf(err) != connect.CodeInvalidArgument {
		t.Fatalf("отрицательная группа: code=%v err=%v", connect.CodeOf(err), err)
	}
}

// Пользователь мог удалить «Default»: тогда импорт без группы идёт в первую
// обычную (не подписочную) группу в порядке вкладок.
func TestImportContentDefaultGroupDeleted(t *testing.T) {
	cs := newE2E(t)
	ctx := context.Background()

	sub := &model.Group{Name: "sub", Kind: model.GroupSubscription, SubscriptionURL: "https://sub.example.org/x"}
	if err := cs.st.CreateGroup(sub); err != nil {
		t.Fatal(err)
	}
	basic := &model.Group{Name: "basic", Kind: model.GroupBasic}
	if err := cs.st.CreateGroup(basic); err != nil {
		t.Fatal(err)
	}
	for _, id := range []int64{store.DefaultGroupID, 1} {
		if err := cs.st.DeleteGroup(id); err != nil {
			t.Fatalf("DeleteGroup(%d): %v", id, err)
		}
	}

	res, err := cs.profile.ImportContent(ctx, connect.NewRequest(&lavrayv1.ImportContentRequest{Content: importGroupLink}))
	if err != nil {
		t.Fatalf("ImportContent: %v", err)
	}
	if len(res.Msg.AddedProfileIds) != 1 {
		t.Fatalf("added = %v, ожидался один профиль", res.Msg.AddedProfileIds)
	}
	p, err := cs.st.GetProfile(res.Msg.AddedProfileIds[0])
	if err != nil {
		t.Fatal(err)
	}
	if p.GroupID != basic.ID {
		t.Fatalf("group_id = %d, ожидалась первая обычная группа %d", p.GroupID, basic.ID)
	}
}
