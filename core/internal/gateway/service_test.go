// SPDX-License-Identifier: GPL-3.0-only

package gateway

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

type testClock struct{ t time.Time }

func (c *testClock) Now() time.Time          { return c.t }
func (c *testClock) Advance(d time.Duration) { c.t = c.t.Add(d) }

// svcFixture — сервис с фейковым nft и ручным временем.
type svcFixture struct {
	svc    *Service
	runner *fakeRunner
	kv     *memKV
	clk    *testClock
}

func newSvc(t *testing.T) *svcFixture {
	t.Helper()
	f := &svcFixture{runner: &fakeRunner{}, kv: newMemKV(), clk: &testClock{t: time.Date(2026, 10, 31, 12, 0, 0, 0, time.UTC)}}
	svc, err := NewService(baseCfg(), NewPolicyRepo(f.kv), &NftApplier{Runner: f.runner}, f.clk.Now)
	if err != nil {
		t.Fatal(err)
	}
	f.svc = svc
	return f
}

func (f *svcFixture) lastScript() string { return f.runner.scripts[len(f.runner.scripts)-1] }

// TestApplierRollbackOnWatchdogTrip — нет связи дольше порога: правила
// снимаются (интернет напрямую), вернулась связь — правила возвращаются (R).
func TestApplierRollbackOnWatchdogTrip(t *testing.T) {
	f := newSvc(t)
	var events []bool
	f.svc.SetNotify(func(failover bool) { events = append(events, failover) })

	if err := f.svc.Activate(); err != nil {
		t.Fatal(err)
	}
	if st := f.svc.Status(); !st.Active || st.WatchdogTripped || !strings.Contains(f.lastScript(), "tproxy ip to :7893") {
		t.Fatalf("после Activate: %+v, скрипт %q", st, f.lastScript())
	}
	f.clk.Advance(4 * time.Minute)
	f.svc.Tick()
	if !f.svc.Status().Active {
		t.Fatal("правила сняты раньше порога в 5 минут")
	}
	f.clk.Advance(time.Minute)
	f.svc.Tick()
	st := f.svc.Status()
	if st.Active || !st.WatchdogTripped || f.lastScript() != removeScript || len(events) != 1 || !events[0] {
		t.Fatalf("порог не сработал: %+v events=%v", st, events)
	}
	n := len(f.runner.scripts)
	f.svc.Tick()
	if len(f.runner.scripts) != n {
		t.Fatal("повторный Tick снова вызвал nft")
	}
	f.svc.Beat() // связь вернулась
	if st := f.svc.Status(); !st.Active || st.WatchdogTripped || len(events) != 2 || events[1] {
		t.Fatalf("правила не вернулись: %+v events=%v", st, events)
	}
}

func TestTripOnInstanceCrashAndRecovery(t *testing.T) {
	f := newSvc(t)
	_ = f.svc.Activate()
	if err := f.svc.Trip(); err != nil {
		t.Fatal(err)
	}
	if st := f.svc.Status(); st.Active || !st.WatchdogTripped {
		t.Fatalf("после Trip: %+v", st)
	}
	f.runner.err = errors.New("nft: Operation not permitted")
	f.svc.Beat() // правила вернуть не удалось
	if st := f.svc.Status(); st.Active || !st.WatchdogTripped {
		t.Fatalf("сбой Apply при возврате: %+v", st)
	}
	f.runner.err = nil
	f.svc.Beat()
	if !f.svc.Status().Active {
		t.Fatal("повтор Beat не вернул правила")
	}
}

func TestDeactivateStopsBeats(t *testing.T) {
	f := newSvc(t)
	_ = f.svc.Activate()
	if err := f.svc.Deactivate(); err != nil {
		t.Fatal(err)
	}
	n := len(f.runner.scripts)
	f.svc.Beat()
	f.svc.Tick()
	if len(f.runner.scripts) != n || f.svc.Status().Active || f.svc.Status().WatchdogTripped {
		t.Fatal("выключенный шлюз вернул правила")
	}
}

func TestActivateFailureKeepsWatchdogArmed(t *testing.T) {
	f := newSvc(t)
	f.runner.err = errors.New("nft: syntax error\nline 2")
	if err := f.svc.Activate(); err == nil {
		t.Fatal("ошибка применения проглочена")
	}
	st := f.svc.Status()
	if st.Active || strings.ContainsAny(st.LastError, "\n") || !strings.Contains(st.LastError, "syntax error") {
		t.Fatalf("статус при ошибке: %+v", st)
	}
	f.runner.err = nil
	f.svc.Beat()
	if st := f.svc.Status(); !st.Active || st.LastError != "" {
		t.Fatalf("Beat не повторил применение: %+v", st)
	}
}

func TestClearOrphans(t *testing.T) {
	f := newSvc(t)
	if err := f.svc.ClearOrphans(); err != nil || f.lastScript() != removeScript {
		t.Fatalf("ClearOrphans: %v / %q", err, f.runner.scripts)
	}
	f.runner.err = errors.New("boom")
	if err := f.svc.ClearOrphans(); err == nil {
		t.Fatal("ошибка снятия не возвращена")
	}
}

// TestGatewayPolicies — политики сохраняются, попадают в правила и переживают
// перезапуск сервиса.
func TestGatewayPolicies(t *testing.T) {
	f := newSvc(t)
	if _, err := f.svc.SetPolicy(DevicePolicy{MAC: "AA:BB:CC:DD:EE:10", Policy: PolicyDirect}); err != nil {
		t.Fatal(err)
	}
	if len(f.runner.scripts) != 0 {
		t.Fatal("правила применены при выключенном шлюзе")
	}
	_ = f.svc.Activate()
	if !strings.Contains(f.lastScript(), "ether saddr aa:bb:cc:dd:ee:10 return") {
		t.Fatalf("политика не попала в правила:\n%s", f.lastScript())
	}
	got, err := f.svc.SetPolicy(DevicePolicy{MAC: "aa-bb-cc-dd-ee-20", Policy: PolicyPreset, PresetID: "ru-direct"})
	if err != nil || got.MAC != "aa:bb:cc:dd:ee:20" {
		t.Fatalf("SetPolicy: %+v %v", got, err)
	}
	if !strings.Contains(f.lastScript(), "aa:bb:cc:dd:ee:20") {
		t.Fatal("новая политика не применена к работающему шлюзу")
	}
	again, err := NewService(baseCfg(), NewPolicyRepo(f.kv), &NftApplier{Runner: f.runner}, f.clk.Now)
	if err != nil || len(again.Policies()) != 2 {
		t.Fatalf("после перезапуска: %v %v", again.Policies(), err)
	}
	if err := f.svc.ClearPolicy("aa:bb:cc:dd:ee:10"); err != nil || len(f.svc.Policies()) != 1 {
		t.Fatalf("ClearPolicy: %v %v", err, f.svc.Policies())
	}
	if strings.Contains(f.lastScript(), "ee:10") {
		t.Fatal("снятая политика осталась в правилах")
	}
	if err := f.svc.ClearPolicy("aa:bb:cc:dd:ee:77"); err != nil {
		t.Fatalf("снятие несуществующей политики: %v", err)
	}
}

func TestSetPolicyValidation(t *testing.T) {
	f := newSvc(t)
	bad := []DevicePolicy{
		{MAC: "nope", Policy: PolicyProxy},
		{MAC: "aa:bb:cc:dd:ee:10", Policy: "nuke"},
		{MAC: "aa:bb:cc:dd:ee:10", Policy: ""},
		{MAC: "aa:bb:cc:dd:ee:10", Policy: PolicyPreset},
		{MAC: "aa:bb:cc:dd:ee:10", Policy: PolicyPreset, PresetID: "bad id\n"},
	}
	for _, p := range bad {
		if _, err := f.svc.SetPolicy(p); err == nil {
			t.Errorf("политика %+v принята", p)
		}
	}
	if len(f.svc.Policies()) != 0 || len(f.kv.m) != 0 {
		t.Fatal("недопустимая политика сохранена")
	}
}

// TestSetPolicyRollsBack — сбой nft не оставляет политику в настройках, сбой
// записи возвращает прежние правила (R).
func TestSetPolicyRollsBack(t *testing.T) {
	f := newSvc(t)
	_, _ = f.svc.SetPolicy(DevicePolicy{MAC: "aa:bb:cc:dd:ee:10", Policy: PolicyDirect})
	_ = f.svc.Activate()

	f.runner.err = errors.New("nft: busy")
	if _, err := f.svc.SetPolicy(DevicePolicy{MAC: "aa:bb:cc:dd:ee:20", Policy: PolicyDirect}); err == nil {
		t.Fatal("сбой nft не возвращён")
	}
	f.runner.err = nil
	if got := f.svc.Policies(); len(got) != 1 || got[0].MAC != "aa:bb:cc:dd:ee:10" {
		t.Fatalf("политика сохранена несмотря на сбой nft: %+v", got)
	}

	f.kv.failSet = true
	before := len(f.runner.scripts)
	if _, err := f.svc.SetPolicy(DevicePolicy{MAC: "aa:bb:cc:dd:ee:30", Policy: PolicyDirect}); err == nil {
		t.Fatal("сбой записи не возвращён")
	}
	if len(f.runner.scripts) != before+2 || strings.Contains(f.lastScript(), "ee:30") {
		t.Fatalf("прежние правила не восстановлены: вызовов %d→%d\n%s", before, len(f.runner.scripts), f.lastScript())
	}
}

func TestSetPolicyLimit(t *testing.T) {
	f := newSvc(t)
	list := make([]DevicePolicy, MaxPolicies)
	for i := range list {
		list[i] = DevicePolicy{MAC: macN(i), Policy: PolicyProxy}
	}
	if err := f.svc.repo.Save(list); err != nil {
		t.Fatal(err)
	}
	svc, _ := NewService(baseCfg(), f.svc.repo, &NftApplier{Runner: f.runner}, f.clk.Now)
	if _, err := svc.SetPolicy(DevicePolicy{MAC: macN(MaxPolicies + 5), Policy: PolicyProxy}); !errors.Is(err, ErrTooManyPolicies) {
		t.Fatalf("сверх лимита: %v", err)
	}
	if _, err := svc.SetPolicy(DevicePolicy{MAC: macN(3), Policy: PolicyDirect}); err != nil {
		t.Fatalf("замена на лимите должна проходить: %v", err)
	}
}

func macN(i int) string {
	const hex = "0123456789abcdef"
	return "02:00:00:00:" + string(hex[(i>>12)&15]) + string(hex[(i>>8)&15]) + ":" + string(hex[(i>>4)&15]) + string(hex[i&15])
}

// TestRunLoopBeatsWhileProbeOk — цикл возвращает правила, когда probe снова истинен.
func TestRunLoopBeatsWhileProbeOk(t *testing.T) {
	f := newSvc(t)
	_ = f.svc.Activate()
	_ = f.svc.Trip()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { f.svc.Run(ctx, time.Millisecond, func() bool { return true }); close(done) }()
	deadline := time.Now().Add(2 * time.Second)
	for !f.svc.Status().Active && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	cancel()
	<-done
	if !f.svc.Status().Active {
		t.Fatal("цикл не вернул правила")
	}
}

// TestPolicyChangeVsWatchdog — смена политик параллельно с Beat/Tick/Trip не
// теряет политику и не возвращает правила, снятые watchdog'ом (C, R).
func TestPolicyChangeVsWatchdog(t *testing.T) {
	f := newSvc(t)
	_ = f.svc.Activate()
	stop := make(chan struct{})
	done := make(chan struct{})
	go func() {
		defer close(done)
		for {
			select {
			case <-stop:
				return
			default:
				_ = f.svc.Trip()
				f.svc.Beat()
				f.svc.Tick()
			}
		}
	}()
	for i := 0; i < 30; i++ {
		if _, err := f.svc.SetPolicy(DevicePolicy{MAC: macN(i + 1), Policy: PolicyDirect}); err != nil {
			t.Fatal(err)
		}
	}
	close(stop)
	<-done
	f.svc.Beat() // связь есть: правила на месте
	if !f.svc.Status().Active {
		t.Fatal("правила не вернулись после Beat")
	}
	last := f.lastScript()
	for i := 0; i < 30; i++ {
		if !strings.Contains(last, "ether saddr "+macN(i+1)+" return") {
			t.Fatalf("в итоговых правилах нет политики %d", i+1)
		}
	}
	if len(f.svc.Policies()) != 30 {
		t.Fatalf("политик %d, ждали 30", len(f.svc.Policies()))
	}
}
