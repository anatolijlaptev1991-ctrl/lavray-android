// SPDX-License-Identifier: GPL-3.0-only

package api

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"connectrpc.com/connect"

	"github.com/anatolijlaptev1991-ctrl/lavray-android/core/internal/gen/lavrayv1"
	"github.com/anatolijlaptev1991-ctrl/lavray-android/core/internal/inst"
	"github.com/anatolijlaptev1991-ctrl/lavray-android/core/internal/model"
	"github.com/anatolijlaptev1991-ctrl/lavray-android/core/internal/store"
)

// t51Server — сервер на FakeRunner; группы: 1 — обычная, 2 — подписка.
func t51Server(t *testing.T) (*Server, *store.Store) {
	t.Helper()
	dir, err := os.MkdirTemp("", "lavray-t51-*")
	if err != nil {
		t.Fatal(err)
	}
	st, err := store.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = st.Close()
		for i := 0; i < 50; i++ {
			if os.RemoveAll(dir) == nil {
				return
			}
			time.Sleep(10 * time.Millisecond)
		}
	})
	for _, g := range []*model.Group{
		{ID: 1, Name: "Default", Kind: model.GroupBasic},
		{ID: 2, Name: "sub", Kind: model.GroupSubscription, SubscriptionURL: "https://panel.example.com/sub/x"},
	} {
		if err := st.CreateGroup(g); err != nil {
			t.Fatal(err)
		}
	}
	return NewServer(Deps{Store: st, Runner: &inst.FakeRunner{}}), st
}

func t51Import(s *Server, groupID int64, content string) (*lavrayv1.ImportContentResponse, error) {
	resp, err := s.ImportContent(context.Background(), connect.NewRequest(&lavrayv1.ImportContentRequest{
		Content: content, TargetGroupId: int64p(groupID),
	}))
	if err != nil {
		return nil, err
	}
	return resp.Msg, nil
}

const (
	t51LinkA = "trojan://secret-a@a.example.org:443?security=tls&sni=a.example.org#Server%20A"
	t51LinkB = "trojan://secret-b@b.example.org:443?security=tls&sni=b.example.org#Server%20B"
)

// T-51 (B1-27): повторный импорт тех же ссылок дубли не создаёт; одна и та же
// ссылка дважды в одном списке тоже даёт один профиль.
func TestImportContentTwiceDoesNotDuplicate(t *testing.T) {
	s, st := t51Server(t)
	first, err := t51Import(s, 1, t51LinkA+"\n"+t51LinkB+"\n"+t51LinkA)
	if err != nil || len(first.AddedProfileIds) != 2 {
		t.Fatalf("первый импорт: добавлено %v, err=%v, ожидалось 2", first, err)
	}
	again, err := t51Import(s, 1, t51LinkB+"\n"+t51LinkA)
	if err != nil || len(again.AddedProfileIds) != 0 {
		t.Fatalf("повторный импорт: %v, err=%v, ожидалось 0 добавленных", again, err)
	}
	if !strings.Contains(strings.Join(again.Log, "\n"), "пропущено") {
		t.Errorf("в ответе нет сообщения о пропущенных дублях: %v", again.Log)
	}
	if list, _ := st.ListProfiles(1); len(list) != 2 {
		t.Fatalf("в группе %d профилей, ожидалось 2", len(list))
	}
}

// T-51 (A1-16): сбой на середине списка не оставляет половину серверов.
func TestImportContentIsAtomic(t *testing.T) {
	s, st := t51Server(t)
	if _, err := st.DB().Exec(`CREATE TRIGGER t51_fail BEFORE INSERT ON profiles
		WHEN NEW.name = 'FAIL' BEGIN SELECT RAISE(ABORT, 't51 injected failure'); END`); err != nil {
		t.Fatal(err)
	}
	failLink := strings.Replace(t51LinkB, "#Server%20B", "#FAIL", 1)
	if _, err := t51Import(s, 1, t51LinkA+"\n"+failLink); err == nil {
		t.Fatal("ожидалась ошибка вставки")
	}
	if list, _ := st.ListProfiles(1); len(list) != 0 {
		t.Fatalf("после сбоя в группе осталось %d профилей, ожидалось 0 (всё или ничего)", len(list))
	}
}

// T-51 (B1-35): профили, которые пользователь кладёт в группу-подписку сам
// (создание, импорт, перенос), помечаются «ручными»; редактирование флаг
// сохраняет, обычные группы флаг не получают.
func TestManualFlagForProfilesInSubscriptionGroup(t *testing.T) {
	s, st := t51Server(t)
	ctx := context.Background()
	create := func(groupID int64, name string) int64 {
		resp, err := s.CreateProfile(ctx, connect.NewRequest(&lavrayv1.CreateProfileRequest{
			Profile: &lavrayv1.Profile{
				GroupId: groupID, Type: lavrayv1.ProfileType_PROFILE_TYPE_TROJAN, Name: name,
				Address: name + ".example", Port: 443,
				Detail: &lavrayv1.Profile_TrojanVless{TrojanVless: &lavrayv1.TrojanVlessDetail{Password: "pw"}},
			},
		}))
		if err != nil {
			t.Fatalf("CreateProfile: %v", err)
		}
		return resp.Msg.Id
	}
	isManual := func(id int64) bool {
		p, err := st.GetProfile(id)
		if err != nil {
			t.Fatal(err)
		}
		return p.Manual
	}

	if isManual(create(1, "plain")) {
		t.Error("профиль обычной группы помечен ручным")
	}
	mine := create(2, "mine")
	if !isManual(mine) {
		t.Error("CreateProfile в группу-подписку: профиль не помечен ручным")
	}
	imp, err := t51Import(s, 2, t51LinkA)
	if err != nil || len(imp.AddedProfileIds) != 1 || !isManual(imp.AddedProfileIds[0]) {
		t.Errorf("ImportContent в группу-подписку: профиль не помечен ручным (err=%v)", err)
	}
	moved := create(1, "moved")
	if _, err := s.MoveProfiles(ctx, connect.NewRequest(&lavrayv1.MoveProfilesRequest{Ids: []int64{moved}, TargetGroupId: 2})); err != nil {
		t.Fatal(err)
	}
	if !isManual(moved) {
		t.Error("MoveProfiles в группу-подписку: профиль не помечен ручным")
	}

	// Редактор присылает профиль без серверного флага — он обязан сохраниться.
	cur, _ := s.GetProfile(ctx, connect.NewRequest(&lavrayv1.GetProfileRequest{Id: mine}))
	edited := cur.Msg
	edited.Name = "mine-renamed"
	if _, err := s.UpdateProfile(ctx, connect.NewRequest(&lavrayv1.UpdateProfileRequest{Profile: edited})); err != nil {
		t.Fatal(err)
	}
	if !isManual(mine) {
		t.Error("UpdateProfile стёр флаг «ручной»")
	}
}

// T-51 (C2 §11): подписка с локальным/приватным URL при создании отвергается.
func TestCreateSubscriptionRejectsPrivateURL(t *testing.T) {
	s, st := t51Server(t)
	groupsBefore, _ := st.ListGroups()
	for _, bad := range []string{
		"http://127.0.0.1:8080/sub", "http://localhost/sub", "http://panel.localhost/sub",
		"http://192.168.1.10/sub", "http://10.0.0.5/x", "http://[::1]:8080/sub", "http://169.254.169.254/latest",
		"ftp://panel.example.com/sub", "file:///etc/passwd", "http:///nohost",
	} {
		_, err := s.CreateSubscription(context.Background(), connect.NewRequest(&lavrayv1.CreateSubscriptionRequest{Name: "x", Url: bad}))
		if connect.CodeOf(err) != connect.CodeInvalidArgument {
			t.Errorf("CreateSubscription(%q): код %v (err=%v), ожидался INVALID_ARGUMENT", bad, connect.CodeOf(err), err)
		}
	}
	if groupsAfter, _ := st.ListGroups(); len(groupsAfter) != len(groupsBefore) {
		t.Fatalf("отвергнутые подписки создали группы: было %d, стало %d", len(groupsBefore), len(groupsAfter))
	}
	for _, ok := range []string{"https://panel.example.com/sub/abc", "http://panel.example.com:8080/sub?x=1"} {
		if _, err := s.CreateSubscription(context.Background(), connect.NewRequest(&lavrayv1.CreateSubscriptionRequest{Name: "ok", Url: ok})); err != nil {
			t.Errorf("CreateSubscription(%q) отвергнут: %v", ok, err)
		}
	}
}
