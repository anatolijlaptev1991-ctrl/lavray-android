// SPDX-License-Identifier: GPL-3.0-only

package api

// log_pump_test.go — LogService.Subscribe соблюдает max_lines_per_second,
// сообщает число пропущенных строк обычной строкой журнала (proto не менялся),
// а история и живой поток не дублируют строки.

import (
	"context"
	"fmt"
	"strings"
	"sync/atomic"
	"testing"
	"time"
	"unicode/utf8"

	lavrayv1 "github.com/anatolijlaptev1991-ctrl/lavray-android/core/internal/gen/lavrayv1"
	"github.com/anatolijlaptev1991-ctrl/lavray-android/core/internal/logs"
	"google.golang.org/protobuf/proto"
)

type pumpRig struct {
	mux  *logs.Mux
	sub  *logs.Subscription
	gate *logs.FloodGate
	keep func(logs.Line) bool
	now  atomic.Pointer[time.Time]
	tick chan time.Time
	got  []logs.Line
}

func newPumpRig(limit int) *pumpRig {
	mux := logs.NewMux(100)
	r := &pumpRig{mux: mux, sub: mux.Open(256, 0, nil), gate: logs.NewFloodGate(limit), tick: make(chan time.Time)}
	r.setNow(time.Now())
	return r
}

func (r *pumpRig) setNow(t time.Time) { r.now.Store(&t) }
func (r *pumpRig) clock() time.Time   { return *r.now.Load() }

// run качает поток, пока ctx не отменён; now управляется тестом.
func (r *pumpRig) run(ctx context.Context, done chan<- struct{}) {
	go func() {
		defer close(done)
		_ = pumpLogs(ctx, r.sub, r.gate, r.clock, r.tick, r.keep,
			func(l logs.Line) error { r.got = append(r.got, l); return nil })
	}()
}

func TestPumpHonoursLineLimitAndReportsDropped(t *testing.T) {
	r := newPumpRig(3)
	for i := 0; i < 10; i++ {
		r.mux.Logf("info", "core", "m%d", i)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	r.run(ctx, done)
	pumpWait(t, func() bool { return len(r.sub.C) == 0 })
	r.setNow(r.clock().Add(2 * time.Second)) // всплеск затих
	r.tick <- r.clock()                      // тик: сводка по затишью
	cancel()
	<-done
	if len(r.got) != 4 {
		t.Fatalf("строк %d, ожидалось 3 + сводка: %v", len(r.got), r.got)
	}
	for i := 0; i < 3; i++ {
		if r.got[i].Message != fmt.Sprintf("m%d", i) {
			t.Fatalf("порядок нарушен: %v", r.got)
		}
	}
	sum := r.got[3]
	if !strings.Contains(sum.Message, "7") || sum.Level != logs.LevelWarn || sum.Source != logs.SourceCore {
		t.Fatalf("сводка: %+v", sum)
	}
}

func TestPumpUnlimitedPassesEverything(t *testing.T) {
	r := newPumpRig(0)
	for i := 0; i < 50; i++ {
		r.mux.Logf("info", "core", "m%d", i)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	r.run(ctx, done)
	pumpWait(t, func() bool { return len(r.sub.C) == 0 })
	cancel()
	<-done
	if len(r.got) != 50 {
		t.Fatalf("без лимита должны пройти все: %d", len(r.got))
	}
}

func pumpWait(t *testing.T, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatal("условие не наступило")
		}
		time.Sleep(2 * time.Millisecond)
	}
	time.Sleep(20 * time.Millisecond) // дать насосу дойти до select
}

func TestSubscribeTailLiveNoDuplicates(t *testing.T) {
	mux := logs.NewMux(100)
	for i := 0; i < 5; i++ {
		mux.Logf("info", "core", "h%d", i)
	}
	tail := int32(5)
	got := t70Receive(t, mux, &lavrayv1.SubscribeLogsRequest{TailLines: &tail}, 6, func() { mux.Logf("info", "core", "live") })
	if len(got) < 6 {
		t.Fatalf("получено %v", got)
	}
	for i := 0; i < 5; i++ {
		if got[i] != fmt.Sprintf("h%d", i) {
			t.Fatalf("история %v", got)
		}
	}
	if got[5] != "live" {
		t.Fatalf("после истории должна идти живая строка: %v", got)
	}
}

// Лимит из запроса доходит до насоса: всплеск режется, потом приходит сводка.
func TestSubscribeRPCHonoursMaxLinesPerSecond(t *testing.T) {
	mux := logs.NewMux(100)
	limit := int32(3)
	got := t70Receive(t, mux, &lavrayv1.SubscribeLogsRequest{MaxLinesPerSecond: &limit}, 5, func() {
		for i := 0; i < 30; i++ {
			mux.Logf("info", "core", "live")
		}
	})
	if len(got) != 5 {
		t.Fatalf("получено %v", got)
	}
	for i := 0; i < 3; i++ {
		if got[i] != "live" {
			t.Fatalf("первые три строки: %v", got)
		}
	}
	if !strings.Contains(got[3], "пропущено") || got[4] != "live" {
		t.Fatalf("после лимита ожидалась сводка и строка: %v", got)
	}
}

// Сводка проходит те же фильтры: при min_level=ERROR или чужом source она не
// приходит, строки лимит по-прежнему режет.
func TestPumpSummaryHonoursFilters(t *testing.T) {
	errLvl := lavrayv1.LogLevel_LOG_LEVEL_ERROR
	for name, req := range map[string]*lavrayv1.SubscribeLogsRequest{
		"min_level": {MinLevel: &errLvl},
		"sources":   {Sources: []string{"subs"}},
	} {
		t.Run(name, func(t *testing.T) {
			r := newPumpRig(1)
			r.keep = logFilter(req)
			for i := 0; i < 5; i++ {
				r.mux.Logf("error", "subs", "m%d", i)
			}
			ctx, cancel := context.WithCancel(context.Background())
			done := make(chan struct{})
			r.run(ctx, done)
			pumpWait(t, func() bool { return len(r.sub.C) == 0 })
			r.setNow(r.clock().Add(2 * time.Second))
			r.tick <- r.clock()
			cancel()
			<-done
			for _, l := range r.got {
				if strings.Contains(l.Message, "пропущено") {
					t.Fatalf("сводка прошла фильтр: %+v", l)
				}
			}
			if len(r.got) != 1 {
				t.Fatalf("строк %d, ожидалась одна (лимит 1): %v", len(r.got), r.got)
			}
		})
	}
}

func TestLogLineToProtoValidUTF8(t *testing.T) {
	l := logLineToProto(logs.Line{Level: "info", Source: "core", Message: "a\xffb"})
	if !utf8.ValidString(l.Message) {
		t.Fatalf("невалидный UTF-8 в кадре: %q", l.Message)
	}
	if _, err := proto.Marshal(l); err != nil {
		t.Fatal(err)
	}
}
