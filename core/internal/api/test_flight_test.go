// SPDX-License-Identifier: GPL-3.0-only

package api

// test_flight_test.go — single-flight пакетного теста: профиль, который уже
// проверяется, второй вызов не умножает нагрузкой (побеждает последний), а
// StopTests отменяет все идущие батчи.

import (
	"context"
	"net"
	"sync/atomic"
	"testing"
	"time"

	"connectrpc.com/connect"
	lavrayv1 "github.com/anatolijlaptev1991-ctrl/lavray-android/core/internal/gen/lavrayv1"
)

// hangingDial — dial, висящий до отмены; считает вызовы и сообщает о входе.
func hangingDial(calls *atomic.Int32, entered chan<- struct{}) func(context.Context, string, string) (net.Conn, error) {
	return func(ctx context.Context, _, _ string) (net.Conn, error) {
		calls.Add(1)
		entered <- struct{}{}
		<-ctx.Done()
		return nil, ctx.Err()
	}
}

// startBatch запускает TestLatency в горутине (заголовки ответа приходят только
// с первым кадром) и отдаёт канал с итоговой ошибкой стрима.
func (e *stopEnv) startBatch(t *testing.T, ids ...int64) <-chan error {
	t.Helper()
	out := make(chan error, 1)
	timeout := int32(30000)
	go func() {
		st, err := e.profile.TestLatency(context.Background(), connect.NewRequest(&lavrayv1.TestLatencyRequest{
			ProfileIds: ids, Mode: lavrayv1.TestLatencyRequest_MODE_TCP_PING, TimeoutMs: &timeout,
		}))
		if err != nil {
			out <- err
			return
		}
		for st.Receive() {
		}
		out <- st.Err()
	}()
	return out
}

func waitEntered(t *testing.T, ch <-chan struct{}) {
	t.Helper()
	select {
	case <-ch:
	case <-time.After(5 * time.Second):
		t.Fatal("измерение не началось")
	}
}

func drainErr(t *testing.T, ch <-chan error) error {
	t.Helper()
	select {
	case err := <-ch:
		return err
	case <-time.After(5 * time.Second):
		t.Fatal("стрим не завершился")
		return nil
	}
}

// Новый батч, пересекающийся по профилям, отменяет старый (побеждает последний)
// и завершается без ошибки; непересекающиеся батчи идут параллельно.
func TestOverlappingBatchCancelsPrevious(t *testing.T) {
	e := newStopEnv(t, 1001, 1002, 1003)
	var hung, total atomic.Int32
	entered := make(chan struct{}, 8)
	e.s.dial = func(ctx context.Context, _, addr string) (net.Conn, error) {
		total.Add(1)
		if portOf(addr) == "1001" && hung.Add(1) == 1 {
			entered <- struct{}{}
			<-ctx.Done() // первое измерение 1001 висит, пока батч не отменят
			return nil, ctx.Err()
		}
		c, peer := net.Pipe()
		_ = peer.Close()
		return c, nil
	}

	first := e.startBatch(t, e.ids[1001], e.ids[1002])
	waitEntered(t, entered)

	second := e.startBatch(t, e.ids[1001], e.ids[1003])
	if err := drainErr(t, second); err != nil {
		t.Fatalf("пересекающийся батч должен завершиться без ошибки: %v", err)
	}
	if err := drainErr(t, first); err != nil {
		t.Fatalf("отменённый батч: %v", err)
	}
}

func TestDisjointBatchesRunInParallel(t *testing.T) {
	e := newStopEnv(t, 1001, 1002)
	var calls atomic.Int32
	entered := make(chan struct{}, 8)
	e.s.dial = hangingDial(&calls, entered)

	first := e.startBatch(t, e.ids[1001])
	waitEntered(t, entered)
	second := e.startBatch(t, e.ids[1002])
	waitEntered(t, entered) // второй не отменил первый и не ждёт его
	if calls.Load() != 2 {
		t.Fatalf("dial вызван %d раз, ожидалось 2", calls.Load())
	}

	// StopTests отменяет оба батча, а не только последний.
	if _, err := e.profile.StopTests(context.Background(), connect.NewRequest(&lavrayv1.StopTestsRequest{})); err != nil {
		t.Fatal(err)
	}
	if err := drainErr(t, first); err != nil {
		t.Fatalf("первый батч после StopTests: %v", err)
	}
	if err := drainErr(t, second); err != nil {
		t.Fatalf("второй батч после StopTests: %v", err)
	}
}

func TestBatchAllowedAfterStopAndAfterFinish(t *testing.T) {
	e := newStopEnv(t, 1001)
	var calls atomic.Int32
	entered := make(chan struct{}, 8)
	e.s.dial = hangingDial(&calls, entered)

	first := e.startBatch(t, e.ids[1001])
	waitEntered(t, entered)
	if _, err := e.profile.StopTests(context.Background(), connect.NewRequest(&lavrayv1.StopTestsRequest{})); err != nil {
		t.Fatal(err)
	}
	// Сразу после Stop новый запуск не должен упираться в разматывающийся старый.
	again := e.startBatch(t, e.ids[1001])
	waitEntered(t, entered)
	_ = drainErr(t, first)
	if _, err := e.profile.StopTests(context.Background(), connect.NewRequest(&lavrayv1.StopTestsRequest{})); err != nil {
		t.Fatal(err)
	}
	if err := drainErr(t, again); err != nil {
		t.Fatalf("батч после Stop: %v", err)
	}

	// После завершения повтор разрешён.
	e.s.dial = func(context.Context, string, string) (net.Conn, error) {
		c, peer := net.Pipe()
		_ = peer.Close()
		return c, nil
	}
	if err := drainErr(t, e.startBatch(t, e.ids[1001])); err != nil {
		t.Fatalf("повтор после завершения: %v", err)
	}
	if err := drainErr(t, e.startBatch(t, e.ids[1001])); err != nil {
		t.Fatalf("ещё один повтор: %v", err)
	}
}
