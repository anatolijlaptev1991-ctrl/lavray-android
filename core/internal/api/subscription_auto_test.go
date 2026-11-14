// SPDX-License-Identifier: GPL-3.0-only

package api

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"sync/atomic"
	"testing"
	"time"

	"connectrpc.com/connect"
	lavrayv1 "github.com/anatolijlaptev1991-ctrl/lavray-android/core/internal/gen/lavrayv1"
	"github.com/anatolijlaptev1991-ctrl/lavray-android/core/internal/inst"
	"github.com/anatolijlaptev1991-ctrl/lavray-android/core/internal/model"
	"github.com/anatolijlaptev1991-ctrl/lavray-android/core/internal/store"
	"github.com/anatolijlaptev1991-ctrl/lavray-android/core/internal/subs"
)

// T-20: обновление подписки планировщиком (AutoUpdateSubscription).

func subAutoTestServer(t *testing.T) (*Server, *store.Store) {
	t.Helper()
	// Не t.TempDir(): гонка RemoveAll с -wal/-shm на Windows (см. newE2E).
	dir, err := os.MkdirTemp("", "lavray-subauto-*")
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
	// httptest слушает 127.0.0.1: без этого флага URL-гард отвергнет подписку.
	if err := st.SetSetting("sub.allow_private_urls", "true"); err != nil {
		t.Fatal(err)
	}
	return NewServer(Deps{Store: st, Runner: &inst.FakeRunner{}}), st
}

func subAutoGroup(t *testing.T, st *store.Store, url string, names ...string) *model.Group {
	t.Helper()
	g := &model.Group{Name: "sub", Kind: model.GroupSubscription, SubscriptionURL: url}
	if err := st.CreateGroup(g); err != nil {
		t.Fatal(err)
	}
	for i, name := range names {
		p := &model.Profile{GroupID: g.ID, Type: model.TypeTrojan, Name: name,
			Address: fmt.Sprintf("%s%d.example.com", name, i), Port: 443,
			Detail: &model.TrojanVlessDetail{Password: "x"}}
		if err := st.CreateProfile(p); err != nil {
			t.Fatal(err)
		}
	}
	return g
}

func TestAutoUpdateSubscriptionAppliesAndKeepsRollbackSnapshot(t *testing.T) {
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		// a0 остался (обновится на месте под именем a2), b1 пропал, new1 новый.
		fmt.Fprint(w, "trojan://x@a0.example.com:443#a2\ntrojan://x@new1.example.org:443#new1\n")
	}))
	defer srv.Close()

	s, st := subAutoTestServer(t)
	g := subAutoGroup(t, st, srv.URL, "a", "b")
	if err := s.AutoUpdateSubscription(context.Background(), g.ID); err != nil {
		t.Fatalf("AutoUpdateSubscription: %v", err)
	}
	list, _ := st.ListProfiles(g.ID)
	if hits.Load() != 1 || len(list) != 2 || list[0].Name != "a2" || list[1].Name != "new1" {
		t.Fatalf("запросов %d, профилей %d, ожидалось a2 + new1", hits.Load(), len(list))
	}
	if got, _ := st.GetGroup(g.ID); got.LastUpdateMs == 0 {
		t.Fatal("LastUpdateMs не записан")
	}
	// «Откатить подписку» после автообновления возвращает прежний состав.
	if _, err := s.RollbackSubscription(context.Background(),
		connect.NewRequest(&lavrayv1.RollbackSubscriptionRequest{GroupId: g.ID})); err != nil {
		t.Fatalf("RollbackSubscription после автообновления: %v", err)
	}
	if list, _ := st.ListProfiles(g.ID); len(list) != 2 || list[0].Name != "a" || list[1].Name != "b" {
		t.Fatalf("после отката профилей %d, ожидались a и b", len(list))
	}
}

// Деструктивное обновление без подтверждения: ничего не удалено.
func TestAutoUpdateSubscriptionNeverAppliesDestructive(t *testing.T) {
	cases := []struct{ name, body string }{
		{"удаление 3 из 4", "trojan://only@example.org:443#only"},
		{"пустой ответ", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				fmt.Fprint(w, tc.body)
			}))
			defer srv.Close()
			s, st := subAutoTestServer(t)
			g := subAutoGroup(t, st, srv.URL, "p0", "p1", "p2", "p3")

			err := s.AutoUpdateSubscription(context.Background(), g.ID)
			var confirm *subs.ConfirmRequiredError
			if tc.body != "" && !errors.As(err, &confirm) {
				t.Fatalf("err = %v, ожидалась *subs.ConfirmRequiredError", err)
			}
			if tc.body == "" && err == nil {
				t.Fatal("пустой ответ принят как успех")
			}
			if list, _ := st.ListProfiles(g.ID); len(list) != 4 {
				t.Fatalf("без подтверждения группа изменилась: %d профилей", len(list))
			}
		})
	}
}

func TestAutoUpdateSubscriptionBusyAndSkipped(t *testing.T) {
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		fmt.Fprint(w, "trojan://x@new.example.org:443#new")
	}))
	defer srv.Close()
	s, st := subAutoTestServer(t)
	g := subAutoGroup(t, st, srv.URL, "a")

	// Идёт ручное обновление этой подписки: планировщик уступает.
	s.subsMu.Lock()
	s.subsRunning[g.ID] = true
	s.subsMu.Unlock()
	if err := s.AutoUpdateSubscription(context.Background(), g.ID); !errors.Is(err, subs.ErrBusy) {
		t.Fatalf("при занятой подписке err = %v, ожидалась subs.ErrBusy", err)
	}
	s.subsMu.Lock()
	delete(s.subsRunning, g.ID)
	s.subsMu.Unlock()

	// skip_auto_update включили после того, как планировщик выбрал группу.
	g.SkipAutoUpdate = true
	if err := st.UpdateGroup(g); err != nil {
		t.Fatal(err)
	}
	if err := s.AutoUpdateSubscription(context.Background(), g.ID); err != nil {
		t.Fatal(err)
	}
	if hits.Load() != 0 {
		t.Fatalf("запросов к провайдеру %d, ожидалось 0 для skip_auto_update", hits.Load())
	}
	// Метка «обновляется» снята: ручное обновление не получит ALREADY_EXISTS.
	s.subsMu.Lock()
	running := s.subsRunning[g.ID]
	s.subsMu.Unlock()
	if running {
		t.Fatal("subsRunning не очищен после AutoUpdateSubscription")
	}
}

// Планировщик стартует и останавливается без зависаний.
func TestStartSubscriptionSchedulerStops(t *testing.T) {
	s, _ := subAutoTestServer(t)
	stop := s.StartSubscriptionScheduler(context.Background())
	done := make(chan struct{})
	go func() { stop(); close(done) }()
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("остановка планировщика зависла")
	}
}
