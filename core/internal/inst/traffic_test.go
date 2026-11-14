// SPDX-License-Identifier: GPL-3.0-only

package inst

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/anatolijlaptev1991-ctrl/lavray-android/core/internal/traffic"
)

// Источник статистики виден только у инстанса в состоянии Ready: ни при
// запуске, ни при остановке, ни после падения, ни у раннера без статистики.
func TestManagerTrafficOnlyWhenReady(t *testing.T) {
	src := traffic.NewFakeSource()
	r := &FakeRunner{StartDelay: 150 * time.Millisecond}
	r.SetTraffic(src)
	m := NewManager(r)
	ctx := context.Background()

	if m.Traffic() != nil {
		t.Fatal("Idle: источник должен быть nil")
	}
	started := make(chan error, 1)
	go func() { started <- m.Start(ctx, RunInfo{ConfigJSON: []byte(`{}`)}) }()
	waitState(t, m, StateStarting)
	if m.Traffic() != nil {
		t.Fatal("Starting: источник должен быть nil")
	}
	if err := <-started; err != nil {
		t.Fatal(err)
	}
	if m.Traffic() != traffic.Source(src) {
		t.Fatal("Ready: ожидался подставной источник")
	}
	if err := m.Stop(ctx); err != nil {
		t.Fatal(err)
	}
	if m.Traffic() != nil {
		t.Fatal("после Stop источник должен быть nil")
	}

	// Падение при запуске — тоже без источника.
	crash := &FakeRunner{StartErr: errors.New("boom")}
	crash.SetTraffic(src)
	cm := NewManager(crash)
	_ = cm.Start(ctx, RunInfo{ConfigJSON: []byte(`{}`)})
	if cm.Status().State != StateCrashed || cm.Traffic() != nil {
		t.Fatalf("Crashed: состояние %v, источник %v", cm.Status().State, cm.Traffic())
	}

	// Раннер без поддержки статистики (NopRunner и ему подобные) — nil, не паника.
	if NewManager(nil).Traffic() != nil {
		t.Fatal("NopRunner: источник должен быть nil")
	}
	plain := NewManager(&FakeRunner{}) // источник не задан
	if err := plain.Start(ctx, RunInfo{ConfigJSON: []byte(`{}`)}); err != nil {
		t.Fatal(err)
	}
	if plain.Traffic() != nil {
		t.Fatal("раннер без источника: ожидался nil")
	}
}

// Во время остановки источник уже не отдаётся (состояние Stopping).
func TestManagerTrafficNilWhileStopping(t *testing.T) {
	r := &FakeRunner{StopDelay: 150 * time.Millisecond}
	r.SetTraffic(traffic.NewFakeSource())
	m := NewManager(r)
	if err := m.Start(context.Background(), RunInfo{ConfigJSON: []byte(`{}`)}); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- m.Stop(context.Background()) }()
	waitState(t, m, StateStopping)
	if m.Traffic() != nil {
		t.Fatal("Stopping: источник должен быть nil")
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}

func waitState(t *testing.T, m *Manager, want State) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if m.Status().State == want {
			return
		}
		time.Sleep(2 * time.Millisecond)
	}
	t.Fatalf("не дождались состояния %v (сейчас %v)", want, m.Status().State)
}
