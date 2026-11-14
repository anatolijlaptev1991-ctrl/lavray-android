// SPDX-License-Identifier: GPL-3.0-only

package inst

// D-03: Manager.closed читали под m.mu (Start), а писали под m.subMu
// (Shutdown) — гонка данных. На Windows без cgo `go test -race` недоступен,
// поэтому тесты проверяют инвариант напрямую (тип флага) и гоняют
// конкурентный сценарий, который под -race (Linux CI/локально с gcc) ловил бы
// гонку.

import (
	"context"
	"errors"
	"reflect"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// Флаг читается и пишется под разными мьютексами, поэтому обязан быть
// атомарным; возврат к обычному bool без -race ничем не ловится.
func TestManagerClosedFlagIsAtomic(t *testing.T) {
	f, ok := reflect.TypeOf(Manager{}).FieldByName("closed")
	if !ok {
		t.Fatal("поле Manager.closed исчезло: обновите тест вместе с реализацией")
	}
	if f.Type != reflect.TypeOf(atomic.Bool{}) {
		t.Fatalf("Manager.closed имеет тип %v; читатели Start и писатель Shutdown держат разные мьютексы, нужен atomic.Bool", f.Type)
	}
}

func TestManagerStartShutdownConcurrent(t *testing.T) {
	for i := 0; i < 100; i++ {
		m := NewManager(&FakeRunner{})
		ctx := context.Background()
		var wg sync.WaitGroup
		wg.Add(4)
		go func() {
			defer wg.Done()
			// Start либо успел до Shutdown, либо получил ErrNotRunning, либо
			// Shutdown отменил идущий старт (context.Canceled).
			if err := m.Start(ctx, RunInfo{ProfileID: 1}); err != nil &&
				!errors.Is(err, ErrNotRunning) && !errors.Is(err, context.Canceled) {
				t.Errorf("Start: %v", err)
			}
		}()
		go func() {
			defer wg.Done()
			if err := m.Shutdown(ctx); err != nil {
				t.Errorf("Shutdown: %v", err)
			}
		}()
		go func() {
			defer wg.Done()
			ch, cancel := m.Subscribe()
			defer cancel()
			_ = m.Status()
			_ = ch
		}()
		go func() {
			defer wg.Done()
			_ = m.Shutdown(ctx) // повторный Shutdown — no-op
		}()
		wg.Wait()

		// После Shutdown инстанс не поднимается, подписка отдаёт закрытый канал.
		if err := m.Start(ctx, RunInfo{ProfileID: 2}); !errors.Is(err, ErrNotRunning) {
			t.Fatalf("итерация %d: Start после Shutdown = %v, ожидалось ErrNotRunning", i, err)
		}
		ch, cancel := m.Subscribe()
		select {
		case _, open := <-ch:
			if open {
				t.Fatalf("итерация %d: подписка после Shutdown должна быть закрыта", i)
			}
		case <-time.After(time.Second):
			t.Fatalf("итерация %d: канал подписки после Shutdown не закрыт", i)
		}
		cancel()
	}
}
