// SPDX-License-Identifier: GPL-3.0-only

// gateway_test.go — переходы watchdog. Платформо-независимо: Applier-мок.
// Рендер nft — nft_render_test.go.
package gateway

import (
	"errors"
	"testing"
	"time"
)

type memApplier struct {
	applied  []string
	removed  int
	failNext bool
}

func (m *memApplier) Apply(s string) error {
	if m.failNext {
		m.failNext = false
		return errors.New("boom")
	}
	m.applied = append(m.applied, s)
	return nil
}
func (m *memApplier) Remove() error {
	if m.failNext {
		m.failNext = false
		return errors.New("boom")
	}
	m.removed++
	return nil
}

func cfg() Config { return Config{TProxyPort: 7893, Enabled: true, DNSHijack: true} }

func TestWatchdogFailoverAndRecovery(t *testing.T) {
	ap := &memApplier{}
	w := &Watchdog{
		Threshold: 5 * time.Minute,
		Applier:   ap,
		Config:    cfg(),
		Policies:  func() []DevicePolicy { return nil },
	}
	t0 := time.Now()
	if err := w.StartArmed(t0); err != nil {
		t.Fatal(err)
	}
	if len(ap.applied) != 1 {
		t.Fatal("правила не применены при старте")
	}

	// Тики без Beat: 4 минуты — ещё держим, 5+ — сняли.
	for i := 1; i <= 4; i++ {
		w.Tick(t0.Add(time.Duration(i) * time.Minute))
	}
	if ap.removed != 0 {
		t.Fatalf("снял раньше порога: %d", ap.removed)
	}
	failoverNotified := false
	w.OnFailover = func(active bool) {
		if active {
			failoverNotified = true
		}
	}
	w.Tick(t0.Add(5 * time.Minute))
	if ap.removed != 1 || !failoverNotified {
		t.Fatalf("порог не сработал: removed=%d notified=%v", ap.removed, failoverNotified)
	}

	// Восстановление: Beat возвращает правила.
	recovered := false
	w.OnFailover = func(active bool) {
		if !active {
			recovered = true
		}
	}
	w.Beat(t0.Add(6 * time.Minute))
	if len(ap.applied) != 2 || !recovered {
		t.Fatalf("восстановление: applied=%d recovered=%v", len(ap.applied), recovered)
	}

	// Ошибка применения при восстановлении не паникует.
	ap.failNext = true
	w.Tick(t0.Add(30 * time.Minute)) // снова порог → Remove ок
	ap.failNext = true
	w.Beat(t0.Add(31 * time.Minute)) // Apply упал → молча ждём следующего Beat
	if len(ap.applied) != 2 {        // не изменилось
		t.Fatalf("после сбоя Apply: applied=%d", len(ap.applied))
	}
}

func TestWatchdogBeatPreventsFailover(t *testing.T) {
	ap := &memApplier{}
	w := &Watchdog{Threshold: 5 * time.Minute, Applier: ap, Config: cfg(), Policies: func() []DevicePolicy { return nil }}
	t0 := time.Now()
	_ = w.StartArmed(t0)
	// Beat каждые 4 минуты — никогда не сработает.
	for i := 1; i <= 10; i++ {
		w.Beat(t0.Add(time.Duration(i*4) * time.Minute))
		w.Tick(t0.Add(time.Duration(i*4) * time.Minute).Add(time.Second))
	}
	if ap.removed != 0 {
		t.Fatal("watchdog снял правила при живой связи")
	}
}
