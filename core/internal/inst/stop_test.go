// SPDX-License-Identifier: GPL-3.0-only

package inst

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

// T-14 (A1-05, A1-06): повторный Stop и Stop во время Start.

// closeOnceRunner ведёт себя как реальный sing-box: второй Close отдаёт
// ошибку «file already closed», а не молча успех.
type closeOnceRunner struct {
	FakeRunner
	closeMu sync.Mutex
	closed  bool
}

func (c *closeOnceRunner) Stop(ctx context.Context) error {
	c.closeMu.Lock()
	again := c.closed
	c.closed = true
	c.closeMu.Unlock()
	if again {
		return errors.New("file already closed")
	}
	return c.FakeRunner.Stop(ctx)
}

// stubbornRunner не реагирует на ctx: Start всегда занимает delay целиком —
// как instance.Start() у sing-box, куда ctx не передаётся.
type stubbornRunner struct {
	FakeRunner
	delay time.Duration
}

func (s *stubbornRunner) Start(ctx context.Context, configJSON []byte) error {
	time.Sleep(s.delay)
	return s.FakeRunner.Start(context.WithoutCancel(ctx), configJSON)
}

func waitForState(t *testing.T, m *Manager, want State) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if m.Status().State == want {
			return
		}
		time.Sleep(2 * time.Millisecond)
	}
	t.Fatalf("не дождались состояния %v, сейчас %v", want, m.Status().State)
}

// Два (и больше) одновременных Stop: runner.Stop зовётся один раз, все
// вызывающие получают успех, итог — Idle. До исправления второй Stop звал
// runner.Stop повторно (ошибка «file already closed») и менеджер уходил в
// Crashed при полностью остановленном инстансе.
func TestManagerConcurrentStopStopsRunnerOnce(t *testing.T) {
	r := &closeOnceRunner{FakeRunner: FakeRunner{StopDelay: 100 * time.Millisecond}}
	m := NewManager(r)
	if err := m.Start(context.Background(), RunInfo{ProfileID: 1, ConfigJSON: []byte(`{}`)}); err != nil {
		t.Fatal(err)
	}
	const callers = 4
	errs := make(chan error, callers)
	for i := 0; i < callers; i++ {
		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			errs <- m.Stop(ctx)
		}()
	}
	for i := 0; i < callers; i++ {
		if err := <-errs; err != nil {
			t.Errorf("Stop #%d: %v (ожидался nil)", i, err)
		}
	}
	if st := m.Status(); st.State != StateIdle || st.LastError != "" {
		t.Fatalf("после параллельных Stop: state=%v lastError=%q, ожидалось idle без ошибки", st.State, st.LastError)
	}
	if r.StopCalls != 1 {
		t.Fatalf("runner.Stop вызван %d раз, ожидался 1", r.StopCalls)
	}
	// Повторный Stop уже остановленного — no-op.
	if err := m.Stop(context.Background()); err != nil || r.StopCalls != 1 {
		t.Fatalf("Stop в Idle: err=%v, StopCalls=%d", err, r.StopCalls)
	}
}

// Ошибка первой остановки достаётся и ждавшим её (а не «успех»).
func TestManagerConcurrentStopSharesError(t *testing.T) {
	boom := errors.New("не закрылось")
	r := &FakeRunner{StopDelay: 80 * time.Millisecond, StopErr: boom}
	m := NewManager(r)
	if err := m.Start(context.Background(), RunInfo{ProfileID: 1}); err != nil {
		t.Fatal(err)
	}
	errs := make(chan error, 2)
	for i := 0; i < 2; i++ {
		go func() { errs <- m.Stop(context.Background()) }()
	}
	for i := 0; i < 2; i++ {
		if err := <-errs; !errors.Is(err, boom) {
			t.Errorf("Stop #%d: %v, ожидалась ошибка первой остановки", i, err)
		}
	}
	if r.StopCalls != 1 {
		t.Fatalf("runner.Stop вызван %d раз, ожидался 1", r.StopCalls)
	}
}

// Disconnect на 3-й секунде подключения. Раннер не реагирует на отмену (как
// SingBoxRunner в версии до T-14) либо реагирует; в обоих случаях после Stop:
// инстанс не работает, состояние Idle без ошибки, Start вернул отмену, а
// отложенного Ready нет.
func TestManagerStopDuringStartLeavesNothingRunning(t *testing.T) {
	cases := []struct {
		name string
		fr   interface {
			Runner
			Running() bool
		}
		want []State // события после Starting
	}{
		{"раннер игнорирует ctx", &stubbornRunner{delay: 250 * time.Millisecond}, []State{StateStopping, StateIdle}},
		{"раннер уважает ctx", &FakeRunner{StartDelay: 300 * time.Millisecond, FailStartCtx: true}, []State{StateIdle}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m := NewManager(tc.fr)
			events, unsubscribe := m.Subscribe()
			defer unsubscribe()

			startErr := make(chan error, 1)
			go func() {
				startErr <- m.Start(context.Background(), RunInfo{ProfileID: 7, ConfigJSON: []byte(`{}`)})
			}()
			waitForState(t, m, StateStarting)

			stopCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			if err := m.Stop(stopCtx); err != nil {
				t.Fatalf("Stop: %v", err)
			}
			// Сразу после Stop — окончательное состояние, ждать не нужно.
			if st := m.Status(); st.State != StateIdle || st.LastError != "" || st.Run != nil {
				t.Fatalf("сразу после Stop: state=%v lastError=%q run=%v, ожидалось idle без ошибки", st.State, st.LastError, st.Run)
			}
			if tc.fr.Running() {
				t.Fatal("инстанс работает после Disconnect во время подключения")
			}
			if err := <-startErr; !errors.Is(err, context.Canceled) {
				t.Fatalf("Start вернул %v, ожидалась отмена (context.Canceled)", err)
			}
			time.Sleep(60 * time.Millisecond) // отложенного Ready быть не должно
			if st := m.Status(); st.State != StateIdle || tc.fr.Running() {
				t.Fatalf("позже: state=%v running=%v", st.State, tc.fr.Running())
			}

			// События: Starting, затем только перечисленные; Ready нет.
			got := []State{}
		drain:
			for {
				select {
				case ev := <-events:
					got = append(got, ev.Status.State)
				default:
					break drain
				}
			}
			want := append([]State{StateStarting}, tc.want...)
			if len(got) != len(want) {
				t.Fatalf("события %v, ожидались %v", got, want)
			}
			for i := range want {
				if got[i] != want[i] {
					t.Fatalf("события %v, ожидались %v", got, want)
				}
			}

			// После отменённого старта соединение снова поднимается.
			if fr, ok := tc.fr.(*FakeRunner); ok {
				fr.StartDelay = 0
			}
			if err := m.Start(context.Background(), RunInfo{ProfileID: 8, ConfigJSON: []byte(`{}`)}); err != nil {
				t.Fatalf("Start после отменённого: %v", err)
			}
			if st := m.Status(); st.State != StateReady {
				t.Fatalf("после нового Start: %v", st.State)
			}
		})
	}
}

// Вызывающий Stop не дождался (истёк его ctx), но Start сам доводит дело до
// конца: после отмены пользователем инстанс не остаётся работать.
func TestManagerStopDuringStartCallerGivesUp(t *testing.T) {
	fr := &stubbornRunner{delay: 250 * time.Millisecond}
	m := NewManager(fr)
	startErr := make(chan error, 1)
	go func() { startErr <- m.Start(context.Background(), RunInfo{ProfileID: 1, ConfigJSON: []byte(`{}`)}) }()
	waitForState(t, m, StateStarting)

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	if err := m.Stop(ctx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Stop с коротким ctx: %v, ожидался DeadlineExceeded", err)
	}
	if err := <-startErr; !errors.Is(err, context.Canceled) {
		t.Fatalf("Start вернул %v, ожидалась отмена", err)
	}
	if st := m.Status(); st.State != StateIdle || fr.Running() {
		t.Fatalf("после отменённого старта: state=%v running=%v", st.State, fr.Running())
	}
}
