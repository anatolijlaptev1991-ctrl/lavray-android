// SPDX-License-Identifier: GPL-3.0-only

package api

// group_delete_active_test.go — T-12 (F-004, B1-19): DeleteGroup и
// DeleteSubscription отказывают, пока в группе работает профиль активного
// соединения. Удаление группы каскадно убирает её профили: без охраны
// активный профиль исчезал из-под работающего инстанса (статус CONNECTED,
// GetProfile → NotFound, RestartProfile падает).

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"connectrpc.com/connect"
	lavrayv1 "github.com/anatolijlaptev1991-ctrl/lavray-android/core/internal/gen/lavrayv1"
	"github.com/anatolijlaptev1991-ctrl/lavray-android/core/internal/gen/lavrayv1connect"
	"github.com/anatolijlaptev1991-ctrl/lavray-android/core/internal/inst"
	"github.com/anatolijlaptev1991-ctrl/lavray-android/core/internal/store"
)

type delGroupEnv struct {
	profile lavrayv1connect.ProfileServiceClient
	subs    lavrayv1connect.SubscriptionServiceClient
	core    lavrayv1connect.CoreServiceClient
}

func newDelGroupEnv(t *testing.T) *delGroupEnv {
	t.Helper()
	// Не t.TempDir(): гонка RemoveAll с -wal/-shm на Windows (см. store_test).
	dir, err := os.MkdirTemp("", "lavray-delgrp-*")
	if err != nil {
		t.Fatal(err)
	}
	st, err := store.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	srv := NewServer(Deps{Store: st, Runner: &inst.FakeRunner{}})
	mux := http.NewServeMux()
	srv.Register(mux)
	hs := httptest.NewServer(mux)
	t.Cleanup(func() {
		hs.Close()
		_ = st.Close()
		for i := 0; i < 50; i++ {
			if os.RemoveAll(dir) == nil {
				return
			}
			time.Sleep(10 * time.Millisecond)
		}
	})
	c := hs.Client()
	return &delGroupEnv{
		profile: lavrayv1connect.NewProfileServiceClient(c, hs.URL),
		subs:    lavrayv1connect.NewSubscriptionServiceClient(c, hs.URL),
		core:    lavrayv1connect.NewCoreServiceClient(c, hs.URL),
	}
}

func (e *delGroupEnv) newGroup(t *testing.T, name string) int64 {
	t.Helper()
	res, err := e.profile.CreateGroup(context.Background(), connect.NewRequest(&lavrayv1.CreateGroupRequest{
		Group: &lavrayv1.Group{Name: name, Kind: lavrayv1.Group_KIND_BASIC},
	}))
	if err != nil {
		t.Fatalf("CreateGroup %q: %v", name, err)
	}
	return res.Msg.Id
}

func (e *delGroupEnv) newSubscription(t *testing.T, name string) int64 {
	t.Helper()
	res, err := e.subs.CreateSubscription(context.Background(), connect.NewRequest(&lavrayv1.CreateSubscriptionRequest{
		Name: name, Url: "https://sub.example.com/" + name,
	}))
	if err != nil {
		t.Fatalf("CreateSubscription %q: %v", name, err)
	}
	return res.Msg.Id
}

func (e *delGroupEnv) newTrojan(t *testing.T, group int64, name string) int64 {
	t.Helper()
	res, err := e.profile.CreateProfile(context.Background(), connect.NewRequest(&lavrayv1.CreateProfileRequest{
		Profile: &lavrayv1.Profile{
			GroupId: group, Type: lavrayv1.ProfileType_PROFILE_TYPE_TROJAN,
			Name: name, Address: "a.example.org", Port: 443,
			Detail: &lavrayv1.Profile_TrojanVless{TrojanVless: &lavrayv1.TrojanVlessDetail{Password: "pw"}},
		},
	}))
	if err != nil {
		t.Fatalf("CreateProfile %q: %v", name, err)
	}
	return res.Msg.Id
}

func (e *delGroupEnv) connect(t *testing.T, profileID int64) {
	t.Helper()
	if _, err := e.core.Connect(context.Background(), connect.NewRequest(&lavrayv1.ConnectRequest{ProfileId: &profileID})); err != nil {
		t.Fatalf("Connect: %v", err)
	}
}

func (e *delGroupEnv) profileExists(t *testing.T, id int64) bool {
	t.Helper()
	_, err := e.profile.GetProfile(context.Background(), connect.NewRequest(&lavrayv1.GetProfileRequest{Id: id}))
	if err == nil {
		return true
	}
	if connect.CodeOf(err) != connect.CodeNotFound {
		t.Fatalf("GetProfile %d: неожиданная ошибка %v", id, err)
	}
	return false
}

func TestDeleteGroupRefusesActiveProfile(t *testing.T) {
	e := newDelGroupEnv(t)
	ctx := context.Background()
	work := e.newGroup(t, "Work")
	other := e.newGroup(t, "Other")
	active := e.newTrojan(t, work, "Tokyo main")
	e.newTrojan(t, work, "Tokyo spare")
	e.connect(t, active)

	// Группа с активным профилем: отказ, понятное сообщение, ничего не удалено.
	_, err := e.profile.DeleteGroup(ctx, connect.NewRequest(&lavrayv1.DeleteGroupRequest{Id: work}))
	if connect.CodeOf(err) != connect.CodeFailedPrecondition {
		t.Fatalf("DeleteGroup(группа активного профиля): code=%v err=%v, ожидался failed_precondition", connect.CodeOf(err), err)
	}
	if !strings.Contains(err.Error(), "Tokyo main") || !strings.Contains(err.Error(), "отключитесь") {
		t.Fatalf("сообщение должно называть профиль и действие, получено: %v", err)
	}
	if !e.profileExists(t, active) {
		t.Fatal("активный профиль исчез после отказанного DeleteGroup")
	}

	// Другая группа удаляется без помех.
	if _, err := e.profile.DeleteGroup(ctx, connect.NewRequest(&lavrayv1.DeleteGroupRequest{Id: other})); err != nil {
		t.Fatalf("DeleteGroup(посторонняя группа): %v", err)
	}

	// После Disconnect группа удаляется вместе с профилями.
	if _, err := e.core.Disconnect(ctx, connect.NewRequest(&lavrayv1.DisconnectRequest{})); err != nil {
		t.Fatalf("Disconnect: %v", err)
	}
	if _, err := e.profile.DeleteGroup(ctx, connect.NewRequest(&lavrayv1.DeleteGroupRequest{Id: work})); err != nil {
		t.Fatalf("DeleteGroup после Disconnect: %v", err)
	}
	if e.profileExists(t, active) {
		t.Fatal("профиль должен быть удалён каскадом вместе с группой")
	}
}

func TestDeleteSubscriptionRefusesActiveProfile(t *testing.T) {
	e := newDelGroupEnv(t)
	ctx := context.Background()
	sub := e.newSubscription(t, "prov")
	active := e.newTrojan(t, sub, "From subscription")
	e.connect(t, active)

	_, err := e.subs.DeleteSubscription(ctx, connect.NewRequest(&lavrayv1.DeleteSubscriptionRequest{GroupId: sub}))
	if connect.CodeOf(err) != connect.CodeFailedPrecondition {
		t.Fatalf("DeleteSubscription(подписка с активным профилем): code=%v err=%v, ожидался failed_precondition", connect.CodeOf(err), err)
	}
	if !strings.Contains(err.Error(), "From subscription") {
		t.Fatalf("сообщение должно называть профиль, получено: %v", err)
	}
	if !e.profileExists(t, active) {
		t.Fatal("активный профиль исчез после отказанного DeleteSubscription")
	}

	// Состояние ядра не испорчено: соединение живо, RestartProfile работает.
	if _, err := e.core.RestartProfile(ctx, connect.NewRequest(&lavrayv1.RestartProfileRequest{})); err != nil {
		t.Fatalf("RestartProfile после отказа: %v", err)
	}

	if _, err := e.core.Disconnect(ctx, connect.NewRequest(&lavrayv1.DisconnectRequest{})); err != nil {
		t.Fatalf("Disconnect: %v", err)
	}
	if _, err := e.subs.DeleteSubscription(ctx, connect.NewRequest(&lavrayv1.DeleteSubscriptionRequest{GroupId: sub})); err != nil {
		t.Fatalf("DeleteSubscription после Disconnect: %v", err)
	}
}

func TestDeleteGroupAllowedWhenActiveProfileInAnotherGroup(t *testing.T) {
	e := newDelGroupEnv(t)
	ctx := context.Background()
	keep := e.newGroup(t, "Keep")
	drop := e.newGroup(t, "Drop")
	active := e.newTrojan(t, keep, "Active")
	doomed := e.newTrojan(t, drop, "Doomed")
	e.connect(t, active)

	if _, err := e.profile.DeleteGroup(ctx, connect.NewRequest(&lavrayv1.DeleteGroupRequest{Id: drop})); err != nil {
		t.Fatalf("DeleteGroup(группа без активного профиля): %v", err)
	}
	if e.profileExists(t, doomed) {
		t.Fatal("профиль удалённой группы должен исчезнуть")
	}
	if !e.profileExists(t, active) {
		t.Fatal("активный профиль другой группы не должен пострадать")
	}
}
