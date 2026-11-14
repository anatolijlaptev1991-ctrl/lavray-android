// SPDX-License-Identifier: GPL-3.0-only

//go:build singbox_run

package inst

import (
	"context"
	"fmt"
	"net"
	"sync"
	"testing"
	"time"
)

// T-14 (A1-05, A1-06) на реальном sing-box.

func minimalMixedConfig(port int) string {
	return fmt.Sprintf(`{
  "log": {"level": "error"},
  "inbounds": [{"type": "mixed", "tag": "mixed-in", "listen": "127.0.0.1", "listen_port": %d}],
  "outbounds": [{"type": "direct", "tag": "direct"}],
  "route": {"final": "direct"}
}`, port)
}

func portIsFree(port int) bool {
	ln, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", port))
	if err != nil {
		return false
	}
	_ = ln.Close()
	return true
}

// Несколько Stop подряд и одновременно на раннере без менеджера: ни паники
// («close of closed channel»), ни ошибки «file already closed»; порт
// освобождён; раннер пригоден для нового Start.
func TestSingBoxRunnerStopIsSafeToRepeat(t *testing.T) {
	port := freeTCPPort(t)
	runner := NewSingBoxRunner()
	if err := runner.Start(context.Background(), []byte(minimalMixedConfig(port))); err != nil {
		t.Fatalf("Start: %v", err)
	}

	const callers = 8
	errs := make(chan error, callers)
	var wg sync.WaitGroup
	for i := 0; i < callers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			defer func() {
				if r := recover(); r != nil {
					errs <- fmt.Errorf("panic: %v", r)
				}
			}()
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			errs <- runner.Stop(ctx)
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Errorf("Stop: %v", err)
		}
	}
	if err := runner.Stop(context.Background()); err != nil {
		t.Errorf("повторный Stop после остановки: %v", err)
	}
	if !portIsFree(port) {
		t.Fatal("порт mixed-входа не освобождён после Stop")
	}

	// Раннер переиспользуется.
	if err := runner.Start(context.Background(), []byte(minimalMixedConfig(port))); err != nil {
		t.Fatalf("Start после Stop: %v", err)
	}
	if err := runner.Stop(context.Background()); err != nil {
		t.Fatalf("Stop после второго Start: %v", err)
	}
}

// Двойной «Отключить» (A1-05): два Stop менеджера одновременно, как два
// быстрых клика. До исправления: «PANIC: close of closed channel» и итоговое
// состояние Crashed при закрытом инстансе.
func TestManagerDoubleStopRealSingBox(t *testing.T) {
	for i := 0; i < 5; i++ {
		port := freeTCPPort(t)
		m := NewManager(NewSingBoxRunner())
		if err := m.Start(context.Background(), RunInfo{ConfigJSON: []byte(minimalMixedConfig(port))}); err != nil {
			t.Fatalf("итерация %d: Start: %v", i, err)
		}
		errs := make(chan error, 2)
		for k := 0; k < 2; k++ {
			go func() {
				defer func() {
					if r := recover(); r != nil { // как recover-интерсептор API
						errs <- fmt.Errorf("PANIC: %v", r)
					}
				}()
				ctx, cancel := context.WithTimeout(context.Background(), StopTimeout)
				defer cancel()
				errs <- m.Stop(ctx)
			}()
		}
		for k := 0; k < 2; k++ {
			if err := <-errs; err != nil {
				t.Fatalf("итерация %d: Stop: %v", i, err)
			}
		}
		if st := m.Status(); st.State != StateIdle || st.LastError != "" {
			t.Fatalf("итерация %d: state=%v lastError=%q, ожидалось idle", i, st.State, st.LastError)
		}
	}
}

// Disconnect во время подключения (A1-06) на реальном раннере: контекст старта
// уже отменён — раннер не открывает порты и возвращает отмену, а не «успех»,
// после которого инстанс оказывается поднятым.
func TestSingBoxRunnerStartHonoursCancelledContext(t *testing.T) {
	port := freeTCPPort(t)
	runner := NewSingBoxRunner()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err := runner.Start(ctx, []byte(minimalMixedConfig(port)))
	if err == nil {
		_ = runner.Stop(context.Background())
		t.Fatal("Start с отменённым контекстом вернул успех")
	}
	if ctx.Err() == nil || !portIsFree(port) {
		t.Fatalf("после отменённого Start порт занят (err=%v)", err)
	}
	if err := runner.Stop(context.Background()); err != nil {
		t.Fatalf("Stop после отменённого Start: %v", err)
	}
}
