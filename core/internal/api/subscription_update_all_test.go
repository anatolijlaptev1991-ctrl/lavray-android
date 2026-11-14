// SPDX-License-Identifier: GPL-3.0-only

// subscription_update_all_test.go — UpdateAllSubscriptions по настоящему потоку
// connect-клиента (хвосты B): итоговые кадры DONE и CONFIRM_REQUIRED доходят до
// клиента, а кадры с адресом подписки (REQUESTING) — нет.
package api

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"connectrpc.com/connect"
	lavrayv1 "github.com/anatolijlaptev1991-ctrl/lavray-android/core/internal/gen/lavrayv1"
)

// provider — httptest-провайдер, отвечающий текущим текстом reply.
func provider(t *testing.T, reply *atomic.Value) *httptest.Server {
	t.Helper()
	hs := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, reply.Load().(string))
	}))
	t.Cleanup(hs.Close)
	return hs
}

// updateAll читает поток UpdateAllSubscriptions до конца.
func updateAll(t *testing.T, e *t18Env) []*lavrayv1.SubscriptionProgress {
	t.Helper()
	s, err := e.subs().UpdateAllSubscriptions(context.Background(), connect.NewRequest(&lavrayv1.UpdateAllSubscriptionsRequest{}))
	if err != nil {
		t.Fatal(err)
	}
	var frames []*lavrayv1.SubscriptionProgress
	for s.Receive() {
		frames = append(frames, s.Msg())
	}
	if err := s.Err(); err != nil {
		t.Fatalf("поток закончился ошибкой: %v", err)
	}
	return frames
}

// phasesOf — фазы кадров подписки groupID по порядку.
func phasesOf(frames []*lavrayv1.SubscriptionProgress, groupID int64) []lavrayv1.SubscriptionProgress_Phase {
	var out []lavrayv1.SubscriptionProgress_Phase
	for _, f := range frames {
		if f.GroupId == groupID {
			out = append(out, f.Phase)
		}
	}
	return out
}

func TestUpdateAllStreamsDoneAndConfirmRequired(t *testing.T) {
	e := newT18Env(t)
	if err := e.st.SetSetting("sub.allow_private_urls", "true"); err != nil {
		t.Fatal(err)
	}
	var replyA, replyB atomic.Value
	replyA.Store("trojan://p@a1.example.net:443#a1\ntrojan://p@a2.example.net:443#a2\n")
	replyB.Store("trojan://p@b1.example.net:443#b1\ntrojan://p@b2.example.net:443#b2\ntrojan://p@b3.example.net:443#b3\n")
	pa, pb := provider(t, &replyA), provider(t, &replyB)
	a := e.subGroup(t, "A", pa.URL)
	b := e.subGroup(t, "B", pb.URL)

	// Первый прогон: обе подписки обновились — клиент должен увидеть DONE по каждой.
	first := updateAll(t, e)
	for _, id := range []int64{a, b} {
		if got := phasesOf(first, id); len(got) == 0 || got[len(got)-1] != lavrayv1.SubscriptionProgress_PHASE_DONE {
			t.Fatalf("подписка %d: фазы %v, ожидался итог DONE", id, got)
		}
	}

	// Второй прогон: B меняется целиком (удалит 3 из 3) и ждёт решения, A без изменений.
	replyB.Store("trojan://p@n1.example.net:443#n1\ntrojan://p@n2.example.net:443#n2\ntrojan://p@n3.example.net:443#n3\n")
	second := updateAll(t, e)
	gotA, gotB := phasesOf(second, a), phasesOf(second, b)
	if len(gotA) == 0 || gotA[len(gotA)-1] != lavrayv1.SubscriptionProgress_PHASE_DONE {
		t.Fatalf("A: фазы %v, ожидался DONE", gotA)
	}
	if len(gotB) == 0 || gotB[len(gotB)-1] != lavrayv1.SubscriptionProgress_PHASE_CONFIRM_REQUIRED {
		t.Fatalf("B: фазы %v, ожидался CONFIRM_REQUIRED (удаление без подтверждения не молчит)", gotB)
	}
	if got := e.names(t, b); len(got) != 3 || got[0] != "b1" {
		t.Fatalf("без подтверждения группа B не должна меняться, а стала %v", got)
	}

	// Адреса подписок (в них бывают токены) в поток «Обновить все» не попадают.
	for _, f := range append(first, second...) {
		if f.Phase == lavrayv1.SubscriptionProgress_PHASE_REQUESTING {
			t.Errorf("кадр REQUESTING дошёл до клиента: %v", f)
		}
		for _, u := range []string{pa.URL, pb.URL} {
			if strings.Contains(f.GetDetail(), u) {
				t.Errorf("адрес подписки в кадре %v", f)
			}
		}
	}
}
