// SPDX-License-Identifier: GPL-3.0-only

package api

import (
	"context"
	"errors"
	"strconv"
	"testing"
	"time"

	"connectrpc.com/connect"

	lavrayv1 "github.com/anatolijlaptev1991-ctrl/lavray-android/core/internal/gen/lavrayv1"
	"github.com/anatolijlaptev1991-ctrl/lavray-android/core/internal/inst"
)

// rememberSetup — remember.* в базе: включено ли, какой профиль и режим.
func (f *svcFixture) rememberSetup(t *testing.T, enable bool, id int64, modes string) {
	t.Helper()
	err := f.srv.writeSettings([]settingWrite{
		{"remember.enable", strconv.FormatBool(enable)},
		{settingRememberID, strconv.FormatInt(id, 10)},
		{settingRememberModes, modes},
	})
	if err != nil {
		t.Fatal(err)
	}
}

// noSleep — окружение без реальных пауз; pauses копит запрошенные паузы.
func (f *svcFixture) onceEnv(pauses *[]time.Duration) onceEnv {
	return f.srv.onceEnvironment(func(_ context.Context, d time.Duration) error {
		if pauses != nil {
			*pauses = append(*pauses, d)
		}
		return nil
	})
}

func TestConnectLastOnceDisabledIsNoop(t *testing.T) {
	f := newSvcFixture(t, nil)
	p := f.trojan(t)
	f.rememberSetup(t, false, p.ID, `["tun"]`)
	if err := runConnectLastOnce(context.Background(), f.onceEnv(nil)); err != nil {
		t.Fatalf("ожидался тихий no-op, получено: %v", err)
	}
	st := f.srv.inst.Status()
	if f.runner.StartedCounter != 0 || st.State != inst.StateIdle || st.LastError != "" {
		t.Fatalf("при выключенном remember.enable подключения и ошибки быть не должно: starts=%d %v %q",
			f.runner.StartedCounter, st.State, st.LastError)
	}
}

func TestConnectLastOnceNoProfileIsNoop(t *testing.T) {
	f := newSvcFixture(t, nil)
	f.rememberSetup(t, true, 0, `[]`)
	if err := runConnectLastOnce(context.Background(), f.onceEnv(nil)); err != nil {
		t.Fatal(err)
	}
	if st := f.srv.inst.Status(); st.State != inst.StateIdle || st.LastError != "" {
		t.Fatal("без запомненного профиля состояние должно остаться чистым idle")
	}
}

func TestConnectLastOnceConnectsRememberedOnce(t *testing.T) {
	f := newSvcFixture(t, nil)
	p := f.trojan(t)
	f.rememberSetup(t, true, p.ID, `["tun"]`)
	if err := runConnectLastOnce(context.Background(), f.onceEnv(nil)); err != nil {
		t.Fatal(err)
	}
	st := f.srv.inst.Status()
	if f.runner.StartedCounter != 1 || st.State != inst.StateReady || st.Run == nil ||
		st.Run.ProfileID != p.ID || st.Run.Mode != "tun" {
		t.Fatalf("ожидалось одно подключение профиля %d в tun: starts=%d state=%v run=%+v",
			p.ID, f.runner.StartedCounter, st.State, st.Run)
	}
}

func TestConnectLastOnceDeletedProfileNoRetryAndVisible(t *testing.T) {
	f := newSvcFixture(t, nil)
	p := f.trojan(t)
	f.rememberSetup(t, true, p.ID, `["system_proxy"]`)
	if err := f.srv.st.DeleteProfile(p.ID); err != nil {
		t.Fatal(err)
	}
	var pauses []time.Duration
	if err := runConnectLastOnce(context.Background(), f.onceEnv(&pauses)); err != nil {
		t.Fatal(err)
	}
	st := f.srv.inst.Status()
	if len(pauses) != 0 || f.runner.StartedCounter != 0 {
		t.Fatalf("повторов быть не должно: pauses=%v starts=%d", pauses, f.runner.StartedCounter)
	}
	if st.State != inst.StateCrashed || st.LastError == "" {
		t.Fatalf("сбой должен быть виден в статусе: %v %q", st.State, st.LastError)
	}
}

func TestConnectLastOnceTunWithoutRightsNoRetryAndVisible(t *testing.T) {
	f := newSvcFixture(t, nil)
	p := f.trojan(t)
	f.rememberSetup(t, true, p.ID, `["tun"]`)
	f.srv.tunCheck = func() error { return errors.New("TUN требует прав администратора") }
	var pauses []time.Duration
	if err := runConnectLastOnce(context.Background(), f.onceEnv(&pauses)); err != nil {
		t.Fatal(err)
	}
	st := f.srv.inst.Status()
	if len(pauses) != 0 || st.State != inst.StateCrashed || st.LastError != "TUN требует прав администратора" {
		t.Fatalf("pauses=%v state=%v err=%q", pauses, st.State, st.LastError)
	}
}

// Пользователь подключился/отключился сам между проверками — ядро не трогаем.
func TestConnectLastOnceUserActionStops(t *testing.T) {
	f := newSvcFixture(t, nil)
	p := f.trojan(t)
	f.rememberSetup(t, true, p.ID, `["tun"]`)
	env := f.onceEnv(nil)
	real := env.userOps
	calls := 0
	env.userOps = func() int64 { calls++; return real() + int64(calls-1) } // «кто-то нажал» после снимка
	if err := runConnectLastOnce(context.Background(), env); err != nil {
		t.Fatal(err)
	}
	if f.runner.StartedCounter != 0 {
		t.Fatalf("после действий пользователя подключения быть не должно: %d", f.runner.StartedCounter)
	}
}

// Ручной Connect/Disconnect реально увеличивает счётчик действий пользователя.
func TestUserOpsCountedByRPC(t *testing.T) {
	f := newSvcFixture(t, nil)
	p := f.trojan(t)
	before := f.srv.auto.userOps.Load()
	if err := f.connect(t, &lavrayv1.ConnectRequest{ProfileId: &p.ID}); err != nil {
		t.Fatal(err)
	}
	f.disconnect(t)
	if got := f.srv.auto.userOps.Load(); got != before+2 {
		t.Fatalf("Connect+Disconnect должны дать +2, получено %d", got-before)
	}
}

func TestConnectLastOnceNotIdleUntouched(t *testing.T) {
	f := newSvcFixture(t, nil)
	p := f.trojan(t)
	f.rememberSetup(t, true, p.ID, `["tun"]`)
	if err := f.connect(t, &lavrayv1.ConnectRequest{ProfileId: &p.ID, Mode: lavrayv1.ConnectMode_CONNECT_MODE_SYSTEM_PROXY}); err != nil {
		t.Fatal(err)
	}
	if err := runConnectLastOnce(context.Background(), f.onceEnv(nil)); err != nil {
		t.Fatal(err)
	}
	if f.runner.StartedCounter != 1 || f.srv.inst.Status().Run.Mode != "system_proxy" {
		t.Fatal("уже подключённое ядро не трогаем")
	}
}

func TestConnectLastOnceTransientRetriesBounded(t *testing.T) {
	f := newSvcFixture(t, nil)
	p := f.trojan(t)
	f.rememberSetup(t, true, p.ID, `["system_proxy"]`)
	f.runner.StartErr = errors.New("network is unreachable")
	var pauses []time.Duration
	if err := runConnectLastOnce(context.Background(), f.onceEnv(&pauses)); err != nil {
		t.Fatal(err)
	}
	if f.runner.StartedCounter != 3 || len(pauses) != 2 {
		t.Fatalf("ожидалось ровно 3 попытки и 2 паузы: starts=%d pauses=%v", f.runner.StartedCounter, pauses)
	}
	if pauses[1] <= pauses[0] || pauses[0]+pauses[1] > 20*time.Second {
		t.Fatalf("паузы должны расти и укладываться в бюджет: %v", pauses)
	}
	if st := f.srv.inst.Status(); st.State != inst.StateCrashed || st.LastError == "" {
		t.Fatalf("итоговый сбой должен быть виден: %v %q", st.State, st.LastError)
	}
}

func TestConnectLastOnceTransientThenSuccess(t *testing.T) {
	f := newSvcFixture(t, nil)
	p := f.trojan(t)
	f.rememberSetup(t, true, p.ID, `["system_proxy"]`)
	f.runner.StartErr = errors.New("temporary")
	env := f.srv.onceEnvironment(func(context.Context, time.Duration) error {
		f.runner.StartErr = nil // сеть появилась
		return nil
	})
	if err := runConnectLastOnce(context.Background(), env); err != nil {
		t.Fatal(err)
	}
	if st := f.srv.inst.Status(); f.runner.StartedCounter != 2 || st.State != inst.StateReady {
		t.Fatalf("starts=%d state=%v", f.runner.StartedCounter, st.State)
	}
}

func TestConnectLastOnceCancelledByContext(t *testing.T) {
	f := newSvcFixture(t, nil)
	p := f.trojan(t)
	f.rememberSetup(t, true, p.ID, `["system_proxy"]`)
	f.runner.StartErr = errors.New("down")
	ctx, cancel := context.WithCancel(context.Background())
	env := f.srv.onceEnvironment(func(c context.Context, _ time.Duration) error { cancel(); return c.Err() })
	if err := runConnectLastOnce(ctx, env); err != nil {
		t.Fatalf("отмена — штатный выход: %v", err)
	}
	if f.runner.StartedCounter != 1 {
		t.Fatalf("после отмены повторов быть не должно: %d", f.runner.StartedCounter)
	}
}

func TestIsPermanentConnectErr(t *testing.T) {
	for name, tc := range map[string]struct {
		err  error
		want bool
	}{
		"нет профиля":    {errNoRememberedProfile, true},
		"профиль удалён": {errRememberedGone, true},
		"нужны права":    {failedPrecondition("tun"), true},
		"инвалид":        {invalidArg("x"), true},
		"сеть":           {errors.New("dial tcp: unreachable"), false},
		"таймаут":        {connect.NewError(connect.CodeDeadlineExceeded, errors.New("t")), false},
	} {
		if got := isPermanentConnectErr(tc.err); got != tc.want {
			t.Errorf("%s: got %v want %v", name, got, tc.want)
		}
	}
}
