// SPDX-License-Identifier: GPL-3.0-only

package api

// stats_events_test.go — события инстанса в потоке StatsService.Subscribe
// (T-79, ревью): залежавшееся событие остановки не даёт ложного финального
// кадра, а Shutdown ядра закрывает поток одним финальным кадром.

import (
	"context"
	"testing"
	"time"

	lavrayv1 "github.com/anatolijlaptev1991-ctrl/lavray-android/core/internal/gen/lavrayv1"
	"github.com/anatolijlaptev1991-ctrl/lavray-android/core/internal/inst"
	"github.com/anatolijlaptev1991-ctrl/lavray-android/core/internal/traffic"
)

func TestSessionOverDecisions(t *testing.T) {
	src, other := traffic.NewFakeSource(), traffic.NewFakeSource()
	ev := func(st inst.State) inst.Event { return inst.Event{Status: inst.Status{State: st}} }
	cases := []struct {
		name            string
		ev              inst.Event
		ok              bool
		cur             traffic.Source
		end, wantClosed bool
	}{
		{"канал закрыт — Shutdown", inst.Event{}, false, src, true, true},
		{"остановка, источника нет", ev(inst.StateIdle), true, nil, true, false},
		{"падение, источника нет", ev(inst.StateCrashed), true, nil, true, false},
		{"остановка, уже новый источник", ev(inst.StateIdle), true, other, true, false},
		{"залежавшаяся остановка прошлой сессии", ev(inst.StateIdle), true, src, false, false},
		{"залежавшееся падение прошлой сессии", ev(inst.StateCrashed), true, src, false, false},
		{"идёт остановка", ev(inst.StateStopping), true, nil, false, false},
		{"запуск", ev(inst.StateStarting), true, nil, false, false},
		{"готов", ev(inst.StateReady), true, src, false, false},
	}
	for _, c := range cases {
		end, closed := sessionOver(c.ev, c.ok, c.cur, src)
		if end != c.end || closed != c.wantClosed {
			t.Errorf("%s: end=%v closed=%v, ожидалось %v/%v", c.name, end, closed, c.end, c.wantClosed)
		}
	}
}

func TestStatsShutdownEndsStreamWithOneFinal(t *testing.T) {
	e := newTrafficEnv(t)
	e.connect()
	e.open(idA, "proxy", "a:1", 1)
	e.src.AddBytes(idA, 10, 20)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var serr error
	ch := e.subscribe(ctx, &lavrayv1.SubscribeStatsRequest{IntervalMs: int32p(250)}, &serr)
	if f, ok := next(ch, 3*time.Second); !ok || f.Final {
		t.Fatalf("обычный кадр: %+v", f)
	}
	if err := e.srv.StopInstance(context.Background()); err != nil {
		t.Fatal(err)
	}
	finals := 0
	deadline := time.After(5 * time.Second)
	for {
		select {
		case f, ok := <-ch:
			if !ok {
				if finals != 1 {
					t.Fatalf("финальных кадров %d, ожидался 1", finals)
				}
				return
			}
			if f.Final {
				finals++
			}
		case <-deadline:
			t.Fatal("поток не закрылся после Shutdown ядра")
		}
	}
}
