// SPDX-License-Identifier: GPL-3.0-only

// subscription_confirm_test.go — регрессия T-18: после кадра
// PHASE_CONFIRM_REQUIRED поток UpdateSubscription обязан закончиться штатно
// (код OK), а не FAILED_PRECONDITION: клиент читает кадры циклом, и ошибка в
// конце потока делала диалог подтверждения недостижимым.
package api

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"sync/atomic"
	"testing"
	"time"

	"connectrpc.com/connect"
	lavrayv1 "github.com/anatolijlaptev1991-ctrl/lavray-android/core/internal/gen/lavrayv1"
	"github.com/anatolijlaptev1991-ctrl/lavray-android/core/internal/gen/lavrayv1connect"
	"github.com/anatolijlaptev1991-ctrl/lavray-android/core/internal/inst"
	"github.com/anatolijlaptev1991-ctrl/lavray-android/core/internal/model"
	"github.com/anatolijlaptev1991-ctrl/lavray-android/core/internal/store"
)

// t18Env — ядро на httptest (общий стенд для T-18 и T-21).
type t18Env struct {
	hc  *http.Client
	url string
	st  *store.Store
}

// subGroup создаёт группу подписки прямо в хранилище. CreateSubscription (T-51)
// не принимает адреса на loopback, а провайдер этих тестов — httptest на 127.0.0.1.
func (e *t18Env) subGroup(t *testing.T, name, url string) int64 {
	t.Helper()
	g := &model.Group{Name: name, Kind: model.GroupSubscription, SubscriptionURL: url}
	if err := e.st.CreateGroup(g); err != nil {
		t.Fatal(err)
	}
	return g.ID
}

func newT18Env(t *testing.T) *t18Env {
	t.Helper()
	dir, err := os.MkdirTemp("", "lavray-t18-*")
	if err != nil {
		t.Fatal(err)
	}
	st, err := store.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	NewServer(Deps{Store: st, Runner: &inst.FakeRunner{}}).Register(mux)
	hs := httptest.NewServer(mux)
	t.Cleanup(func() {
		hs.Close()
		_ = st.Close()
		for i := 0; i < 50 && os.RemoveAll(dir) != nil; i++ {
			time.Sleep(10 * time.Millisecond)
		}
	})
	return &t18Env{hc: hs.Client(), url: hs.URL, st: st}
}

func (e *t18Env) profile() lavrayv1connect.ProfileServiceClient {
	return lavrayv1connect.NewProfileServiceClient(e.hc, e.url)
}

func (e *t18Env) subs() lavrayv1connect.SubscriptionServiceClient {
	return lavrayv1connect.NewSubscriptionServiceClient(e.hc, e.url)
}

// names — имена профилей группы по порядку.
func (e *t18Env) names(t *testing.T, group int64) []string {
	t.Helper()
	res, err := e.profile().ListProfiles(context.Background(), connect.NewRequest(&lavrayv1.ListProfilesRequest{GroupId: int64p(group)}))
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, p := range res.Msg.Profiles {
		out = append(out, p.Name)
	}
	return out
}

func TestT18_DestructiveUpdateCanBeConfirmed(t *testing.T) {
	e := newT18Env(t)
	ctx := context.Background()
	// Провайдер отдаёт текущий ответ: сначала o1..o3, потом N1..N3.
	var reply atomic.Value
	reply.Store("trojan://p@o1.example.net:443#o1\ntrojan://p@o2.example.net:443#o2\ntrojan://p@o3.example.net:443#o3\n")
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, reply.Load().(string))
	}))
	defer provider.Close()
	gid := e.subGroup(t, "prov", provider.URL)
	// run читает поток до конца: кадры и итоговая ошибка потока.
	run := func(confirm bool) ([]*lavrayv1.SubscriptionProgress, error) {
		s, err := e.subs().UpdateSubscription(ctx, connect.NewRequest(&lavrayv1.UpdateSubscriptionRequest{
			GroupId: gid, AllowPrivateUrls: boolp(true), ConfirmDestructive: boolp(confirm),
		}))
		if err != nil {
			t.Fatal(err)
		}
		var frames []*lavrayv1.SubscriptionProgress
		for s.Receive() {
			frames = append(frames, s.Msg())
		}
		return frames, s.Err()
	}

	// Серверы o1..o3 приходят от подписки, а не созданы руками: созданные вручную
	// серверы подписки (T-51) при обновлении не удаляются, и вопроса не будет.
	if _, err := run(false); err != nil {
		t.Fatalf("первое обновление: %v", err)
	}
	reply.Store("trojan://p@n1.example.net:443#N1\ntrojan://p@n2.example.net:443#N2\ntrojan://p@n3.example.net:443#N3\n")

	// Без подтверждения: кадр CONFIRM_REQUIRED с числом, штатный конец потока, группа цела.
	frames, err := run(false)
	if err != nil {
		t.Fatalf("поток без подтверждения должен закончиться штатно, а закончился ошибкой: code=%v err=%v", connect.CodeOf(err), err)
	}
	last := frames[len(frames)-1]
	if last.Phase != lavrayv1.SubscriptionProgress_PHASE_CONFIRM_REQUIRED || last.GetWouldRemoveCount() != 3 {
		t.Fatalf("последний кадр: %v, ожидался CONFIRM_REQUIRED с would_remove_count=3", last)
	}
	if got := e.names(t, gid); len(got) != 3 || got[0] != "o1" {
		t.Fatalf("без подтверждения группа не меняется, а стала %v", got)
	}

	// Повтор с confirm_destructive=true применяет обновление.
	frames, err = run(true)
	if err != nil || frames[len(frames)-1].Phase != lavrayv1.SubscriptionProgress_PHASE_DONE {
		t.Fatalf("подтверждённое обновление: err=%v кадры=%v", err, frames)
	}
	if got := e.names(t, gid); len(got) != 3 || got[0] != "N1" || got[2] != "N3" {
		t.Fatalf("после подтверждения ожидались N1..N3, получено %v", got)
	}
}
