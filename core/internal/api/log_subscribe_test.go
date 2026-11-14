// SPDX-License-Identifier: GPL-3.0-only

// log_subscribe_test.go — регрессия T-70: LogService.Subscribe выполняет
// контракт tail_lines («сколько истории отдать сразу, 0 = нет») и фильтрует
// историю теми же условиями, что и живой поток. Раньше при tail_lines=0 ядро
// отдавало весь буфер (до 1000 строк) без фильтра по уровню — экран «Журнал»
// получал историю дважды (GetTail + поток).
package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"connectrpc.com/connect"
	lavrayv1 "github.com/anatolijlaptev1991-ctrl/lavray-android/core/internal/gen/lavrayv1"
	"github.com/anatolijlaptev1991-ctrl/lavray-android/core/internal/gen/lavrayv1connect"
	"github.com/anatolijlaptev1991-ctrl/lavray-android/core/internal/inst"
	"github.com/anatolijlaptev1991-ctrl/lavray-android/core/internal/logs"
	"github.com/anatolijlaptev1991-ctrl/lavray-android/core/internal/store"
)

// t70Receive подписывается и читает n сообщений. Ответ потока начинается с
// первого сообщения, поэтому live() публикует строки уже до Subscribe.
func t70Receive(t *testing.T, mux *logs.Mux, req *lavrayv1.SubscribeLogsRequest, n int, live func()) []string {
	t.Helper()
	dir, err := os.MkdirTemp("", "lavray-t70-*")
	if err != nil {
		t.Fatal(err)
	}
	st, err := store.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	h := http.NewServeMux()
	NewServer(Deps{Store: st, Logs: mux, Runner: &inst.FakeRunner{}}).Register(h)
	hs := httptest.NewServer(h)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	stop := make(chan struct{})
	defer func() {
		close(stop)
		cancel()
		hs.Close()
		_ = st.Close()
		for i := 0; i < 50 && os.RemoveAll(dir) != nil; i++ {
			time.Sleep(10 * time.Millisecond)
		}
	}()
	if live != nil {
		go func() {
			for {
				select {
				case <-stop:
					return
				case <-time.After(10 * time.Millisecond):
					live()
				}
			}
		}()
	}
	stream, err := lavrayv1connect.NewLogServiceClient(hs.Client(), hs.URL).Subscribe(ctx, connect.NewRequest(req))
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for len(got) < n && stream.Receive() {
		got = append(got, stream.Msg().Message)
	}
	return got
}

func t70Mux() *logs.Mux {
	m := logs.NewMux(100)
	m.Logf("info", "core", "one")
	m.Logf("warn", "subs", "two")
	m.Logf("error", "core", "three")
	m.Logf("info", "core", "four")
	return m
}

func TestT70_SubscribeWithoutTailSendsNoHistory(t *testing.T) {
	mux := t70Mux()
	got := t70Receive(t, mux, &lavrayv1.SubscribeLogsRequest{}, 1, func() { mux.Logf("info", "core", "live") })
	if len(got) != 1 || got[0] != "live" {
		t.Fatalf("tail_lines не задан — истории быть не должно, а первой пришла %v", got)
	}
}

func TestT70_TailIsFilteredAndLimited(t *testing.T) {
	warn, errLvl := lavrayv1.LogLevel_LOG_LEVEL_WARN, lavrayv1.LogLevel_LOG_LEVEL_ERROR
	got := t70Receive(t, t70Mux(), &lavrayv1.SubscribeLogsRequest{MinLevel: &warn, TailLines: int32p(5)}, 2, nil)
	if len(got) != 2 || got[0] != "two" || got[1] != "three" {
		t.Fatalf("история при min_level=WARN должна быть [two three], получено %v", got)
	}
	got = t70Receive(t, t70Mux(), &lavrayv1.SubscribeLogsRequest{MinLevel: &errLvl, TailLines: int32p(1)}, 1, nil)
	if len(got) != 1 || got[0] != "three" {
		t.Fatalf("tail_lines=1 при min_level=ERROR должна дать [three], получено %v", got)
	}
}
