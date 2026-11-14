// SPDX-License-Identifier: GPL-3.0-only

//go:build singbox_run

package inst

import (
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/sagernet/sing-box/adapter"
	"github.com/sagernet/sing-box/common/trafficcontrol"
	M "github.com/sagernet/sing/common/metadata"
)

func TestConnInfoFromMeta(t *testing.T) {
	up, down := new(atomic.Int64), new(atomic.Int64)
	up.Store(11)
	down.Store(22)
	created := time.UnixMilli(1_700_000_000_123)
	m := &trafficcontrol.TrackerMetadata{
		CreatedAt: created,
		Upload:    up,
		Download:  down,
		Chain:     []string{"proxy", "auto", "select"}, // форк: от конечного к выбранному
		Outbound:  "proxy",
		Metadata: adapter.InboundContext{
			Inbound:     "mixed-in",
			Network:     "tcp",
			Destination: M.ParseSocksaddr("example.org:443"),
			Domain:      "cdn.example.org",
			ProcessInfo: &adapter.ConnectionOwner{ProcessPath: `C:\Users\anatoly\AppData\Local\Chrome\chrome.exe`},
		},
	}
	c := connInfoFromMeta(m, false)
	if c.StartMs != 1_700_000_000_123 || c.Up != 11 || c.Down != 22 || c.EndMs != 0 {
		t.Fatalf("время и счётчики: %+v", c)
	}
	if c.Outbound != "proxy" || c.Inbound != "mixed-in" || c.Network != "tcp" ||
		c.Destination != "example.org:443" || c.Domain != "cdn.example.org" {
		t.Fatalf("поля: %+v", c)
	}
	if strings.Join(c.Chain, ">") != "select>auto>proxy" {
		t.Fatalf("цепочка должна идти по ходу трафика, получено %v", c.Chain)
	}
	if c.Process != "chrome.exe" {
		t.Fatalf("процесс %q: путь на диске наружу не уходит, только имя файла", c.Process)
	}
	if strings.Contains(c.Process, `\`) || strings.Contains(c.Process, "anatoly") {
		t.Fatal("в имени процесса остались части пути")
	}

	// У закрытого соединения есть время завершения; у активного ClosedAt не читается.
	m.ClosedAt = created.Add(3 * time.Second)
	if got := connInfoFromMeta(m, true); got.EndMs != 1_700_000_003_123 {
		t.Fatalf("EndMs закрытого = %d", got.EndMs)
	}
	if got := connInfoFromMeta(m, false); got.EndMs != 0 {
		t.Fatalf("EndMs активного = %d", got.EndMs)
	}
}

// Домен, совпадающий с адресом назначения, не дублируется; пустые счётчики и
// метаданные не ломают разбор.
func TestConnInfoFromMetaSparse(t *testing.T) {
	m := &trafficcontrol.TrackerMetadata{
		Metadata: adapter.InboundContext{
			Destination: M.ParseSocksaddr("example.org:80"),
			Domain:      "example.org",
		},
	}
	c := connInfoFromMeta(m, false)
	if c.Domain != "" || c.Up != 0 || c.Down != 0 || c.Chain != nil || c.Rule != "" || c.Process != "" {
		t.Fatalf("разреженные метаданные: %+v", c)
	}
}
