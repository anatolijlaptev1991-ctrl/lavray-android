// SPDX-License-Identifier: GPL-3.0-only

package api

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"connectrpc.com/connect"
	lavrayv1 "github.com/anatolijlaptev1991-ctrl/lavray-android/core/internal/gen/lavrayv1"
	"github.com/anatolijlaptev1991-ctrl/lavray-android/core/internal/inst"
	"github.com/anatolijlaptev1991-ctrl/lavray-android/core/internal/store"
)

// sinkRunner — раннер, отдающий тесту приёмник журнала, который api.NewServer
// подключает к SingBoxRunner (T-33).
type sinkRunner struct {
	inst.FakeRunner
	sink inst.LogSink
}

func (r *sinkRunner) SetLogSink(sink inst.LogSink) { r.sink = sink }

// D-07: MAC шлюза из строки sing-box не доходит до журнала ядра (GetTail, из
// которого UI строит экспорт журнала), остальной текст строки сохраняется.
func TestSingBoxLogMasksGatewayMAC(t *testing.T) {
	// Не t.TempDir(): гонка RemoveAll с -wal/-shm на Windows (см. newE2E).
	dir, err := os.MkdirTemp("", "lavray-sbmac-*")
	if err != nil {
		t.Fatal(err)
	}
	st, err := store.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = st.Close()
		for i := 0; i < 50; i++ {
			if os.RemoveAll(dir) == nil {
				return
			}
			time.Sleep(10 * time.Millisecond)
		}
	})
	runner := &sinkRunner{}
	s := NewServer(Deps{Store: st, Runner: runner})
	if runner.sink == nil {
		t.Fatal("api.NewServer не подключил приёмник журнала к раннеру")
	}

	runner.sink("info", "network: updated network environment: gateway 192.168.31.1, gateway_mac 50:88:11:22:33:44")
	runner.sink("warn", "outbound/vless[proxy]: connection to example.com:443 failed")

	res, err := s.GetTail(context.Background(), connect.NewRequest(&lavrayv1.GetTailRequest{Lines: 100}))
	if err != nil {
		t.Fatal(err)
	}
	var env, other string
	for _, l := range res.Msg.Lines {
		switch {
		case strings.Contains(l.Message, "updated network environment"):
			env = l.Message
		case strings.Contains(l.Message, "example.com:443"):
			other = l.Message
		}
		if strings.Contains(l.Message, "11:22:33:44") {
			t.Errorf("MAC остался в журнале: %q", l.Message)
		}
	}
	if want := "network: updated network environment: gateway 192.168.31.1, gateway_mac 50:88:**:**:**:**"; env != want {
		t.Errorf("строка о шлюзе:\n  итог  %q\n  ждали %q", env, want)
	}
	if want := "outbound/vless[proxy]: connection to example.com:443 failed"; other != want {
		t.Errorf("чужая строка изменена: %q", other)
	}
}
