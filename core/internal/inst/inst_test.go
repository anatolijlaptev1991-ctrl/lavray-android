// SPDX-License-Identifier: GPL-3.0-only

package inst

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestManagerLifecycle(t *testing.T) {
	r := &FakeRunner{}
	m := NewManager(r)
	if st := m.Status(); st.State != StateIdle || st.Run != nil {
		t.Fatalf("начальное состояние: %+v", st)
	}
	ctx := context.Background()
	if err := m.Start(ctx, RunInfo{ProfileID: 7, ProfileName: "тест", Mode: "system_proxy", ConfigJSON: []byte(`{}`)}); err != nil {
		t.Fatalf("Start: %v", err)
	}
	st := m.Status()
	if st.State != StateReady || st.ActiveProfileID() != 7 || st.Run.ProfileName != "тест" {
		t.Fatalf("после Start: %+v", st)
	}
	if !r.Running() {
		t.Fatal("раннер должен быть запущен")
	}
	// Start поверх бегущего → ErrAlreadyRunning (FR-UI-032 / §6.1)
	if err := m.Start(ctx, RunInfo{ProfileID: 8}); !errors.Is(err, ErrAlreadyRunning) {
		t.Fatalf("повторный Start: %v, хочу ErrAlreadyRunning", err)
	}
	if err := m.Stop(ctx); err != nil {
		t.Fatalf("Stop: %v", err)
	}
	if st := m.Status(); st.State != StateIdle || st.Run != nil {
		t.Fatalf("после Stop: %+v", st)
	}
	if r.Running() {
		t.Fatal("раннер должен быть остановлен")
	}
	// LastConfig переживает остановку (GetRuntimeConfig — «текущего/последнего»)
	if len(m.Status().LastConfig) == 0 {
		t.Fatal("LastConfig пуст после остановки")
	}
	// идемпотентность Stop
	if err := m.Stop(ctx); err != nil {
		t.Fatalf("повторный Stop: %v", err)
	}
}

func TestManagerCrashAndRestart(t *testing.T) {
	boom := errors.New("порт занят")
	r := &FakeRunner{StartErr: boom}
	m := NewManager(r)
	ctx := context.Background()
	err := m.Start(ctx, RunInfo{ProfileID: 1, ConfigJSON: []byte(`{}`)})
	if err == nil {
		t.Fatal("Start с ошибкой раннера должен вернуть ошибку")
	}
	st := m.Status()
	if st.State != StateCrashed || st.LastError == "" {
		t.Fatalf("после сбоя: %+v", st)
	}
	// рестарт из Crashed разрешён
	r.StartErr = nil
	if err := m.Start(ctx, RunInfo{ProfileID: 2}); err != nil {
		t.Fatalf("рестарт из Crashed: %v", err)
	}
	if st := m.Status(); st.State != StateReady || st.ActiveProfileID() != 2 {
		t.Fatalf("после рестарта: %+v", st)
	}
}

func TestManagerStopTimeout(t *testing.T) {
	r := &FakeRunner{StopDelay: 500 * time.Millisecond}
	m := NewManager(r)
	bg := context.Background()
	if err := m.Start(bg, RunInfo{ProfileID: 1}); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(bg, 50*time.Millisecond)
	defer cancel()
	if err := m.Stop(ctx); err == nil {
		t.Fatal("Stop с истёкшим ctx должен вернуть ошибку (graceful-лестница)")
	}
	if st := m.Status(); st.State != StateCrashed {
		t.Fatalf("после таймаута остановки: %v, хочу Crashed", st.State)
	}
}

func TestManagerEvents(t *testing.T) {
	r := &FakeRunner{}
	m := NewManager(r)
	ch, cancel := m.Subscribe()
	defer cancel()
	ctx := context.Background()
	if err := m.Start(ctx, RunInfo{ProfileID: 1}); err != nil {
		t.Fatal(err)
	}
	if err := m.Stop(ctx); err != nil {
		t.Fatal(err)
	}
	// ожидаем Starting → Ready → Stopping → Idle
	want := []State{StateStarting, StateReady, StateStopping, StateIdle}
	timeout := time.After(2 * time.Second)
	for _, w := range want {
		select {
		case ev := <-ch:
			if ev.Status.State != w {
				t.Fatalf("событие %v, хочу %v", ev.Status.State, w)
			}
		case <-timeout:
			t.Fatalf("не дождались события %v", w)
		}
	}
}

func TestNopRunner(t *testing.T) {
	m := NewManager(nil) // nil → NopRunner
	err := m.Start(context.Background(), RunInfo{ProfileID: 1})
	if err == nil || err.Error() == "" {
		t.Fatal("NopRunner.Start должен объяснить отсутствие sing-box")
	}
	if st := m.Status(); st.State != StateCrashed {
		t.Fatalf("NopRunner: состояние %v, хочу Crashed", st.State)
	}
}

func TestShutdown(t *testing.T) {
	r := &FakeRunner{}
	m := NewManager(r)
	ctx := context.Background()
	if err := m.Start(ctx, RunInfo{ProfileID: 1}); err != nil {
		t.Fatal(err)
	}
	ch, cancel := m.Subscribe()
	defer cancel()
	if err := m.Shutdown(ctx); err != nil {
		t.Fatalf("Shutdown: %v", err)
	}
	if r.Running() {
		t.Fatal("Shutdown должен остановить инстанс (AUDIT §7.1.2)")
	}
	// подписки закрыты
	select {
	case _, ok := <-ch:
		if ok {
			t.Fatal("канал подписки должен быть закрыт после Shutdown")
		}
	case <-time.After(time.Second):
		t.Fatal("канал не закрыт после Shutdown")
	}
	// повторный Shutdown — no-op
	if err := m.Shutdown(ctx); err != nil {
		t.Fatalf("повторный Shutdown: %v", err)
	}
}
