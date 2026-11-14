// SPDX-License-Identifier: GPL-3.0-only

package api

// traffic_session_test.go — завершение и сброс сессии учёта (T-79, ревью):
// обнуление счётчиков посреди сессии и остановка ядра при выходе процесса.

import (
	"context"
	"testing"
	"time"

	"connectrpc.com/connect"

	lavrayv1 "github.com/anatolijlaptev1991-ctrl/lavray-android/core/internal/gen/lavrayv1"
)

func TestResetTrafficDuringSessionDoesNotResurrect(t *testing.T) {
	e := newTrafficEnv(t)
	e.connect()
	e.open(idA, "proxy", "a:1", 1)
	e.src.AddBytes(idA, 1000, 2000) // ещё не сброшено таймером
	_, err := e.srv.ResetTraffic(context.Background(), connect.NewRequest(&lavrayv1.ResetTrafficRequest{Ids: []int64{e.profile.ID}}))
	if err != nil {
		t.Fatal(err)
	}
	e.srv.flushAccounting()
	if got := e.profileTraffic(); got.UplinkBytes != 0 || got.DownlinkBytes != 0 {
		t.Fatalf("после обнуления вернулись байты до него: %+v", got)
	}
	e.src.AddBytes(idA, 10, 20)
	e.disconnect(&lavrayv1.DisconnectRequest{})
	if got := e.profileTraffic(); got.UplinkBytes != 10 || got.DownlinkBytes != 20 {
		t.Fatalf("после обнуления считается только новое: %+v", got)
	}
	if s := e.sessions(); len(s) != 1 || s[0].UplinkBytes != 1010 || s[0].DownlinkBytes != 2020 {
		t.Fatalf("история сессии не обнуляется: %+v", s[0])
	}
}

func TestResetTrafficWithoutSessionAndUnknownID(t *testing.T) {
	e := newTrafficEnv(t)
	if err := e.st.AddTraffic(e.profile.ID, 5, 6); err != nil {
		t.Fatal(err)
	}
	_, err := e.srv.ResetTraffic(context.Background(), connect.NewRequest(&lavrayv1.ResetTrafficRequest{Ids: []int64{e.profile.ID, 999999}}))
	if err != nil {
		t.Fatalf("неизвестный id пропускается: %v", err)
	}
	if got := e.profileTraffic(); got.UplinkBytes != 0 || got.DownlinkBytes != 0 {
		t.Fatalf("счётчики: %+v", got)
	}
}

// Выход процесса (EOF stdin, сигнал) идёт через StopInstance. Байты,
// переданные, пока инстанс останавливается, попадают и в профиль, и в
// сессию, а сессия закрыта к моменту возврата — до выхода процесса.
func TestStopInstanceEndsSessionAfterStop(t *testing.T) {
	e := newTrafficEnv(t)
	e.runner.StopDelay = 300 * time.Millisecond
	e.connect()
	e.open(idA, "proxy", "a:1", 1)
	e.src.AddBytes(idA, 100, 200)
	done := make(chan error, 1)
	go func() { done <- e.srv.StopInstance(context.Background()) }()
	time.Sleep(100 * time.Millisecond) // идёт Stop: подписки уже закрыты
	e.src.AddBytes(idA, 1000, 2000)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	s := e.sessions()
	if len(s) != 1 || s[0].EndedAtMs == nil {
		t.Fatalf("сессия не закрыта к возврату StopInstance: %+v", s)
	}
	if s[0].UplinkBytes != 1100 || s[0].DownlinkBytes != 2200 {
		t.Fatalf("сессия без байт периода остановки: %+v", s[0])
	}
	if got := e.profileTraffic(); got.UplinkBytes != 1100 || got.DownlinkBytes != 2200 {
		t.Fatalf("профиль без байт периода остановки: %+v", got)
	}
}
