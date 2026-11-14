// SPDX-License-Identifier: GPL-3.0-only

package logs

import (
	"fmt"
	"sync"
	"testing"
)

func logN(m *Mux, from, to int) {
	for i := from; i < to; i++ {
		m.Logf(LevelInfo, SourceCore, "m%d", i)
	}
}

func TestTailSmallNReturnsLastOnly(t *testing.T) {
	m := NewMux(50)
	logN(m, 0, 30)
	got := m.Tail(3)
	if len(got) != 3 || got[0].Message != "m27" || got[2].Message != "m29" {
		t.Fatalf("Tail(3) = %v", got)
	}
	got[0].Message = "испорчено"
	if m.Tail(1)[0].Message != "m29" || m.Tail(3)[0].Message != "m27" {
		t.Fatal("Tail отдал память буфера")
	}
	if len(m.Tail(0)) != 30 || len(m.Tail(100)) != 30 {
		t.Fatal("n<=0 и n больше буфера — весь буфер")
	}
}

func TestOpenHistoryFilteredAndLimited(t *testing.T) {
	m := NewMux(100)
	logN(m, 0, 20)
	even := func(l Line) bool { var n int; fmt.Sscanf(l.Message, "m%d", &n); return n%2 == 0 }
	sub := m.Open(8, 3, even)
	defer sub.Close()
	if len(sub.History) != 3 || sub.History[0].Message != "m14" || sub.History[2].Message != "m18" {
		t.Fatalf("история %v", sub.History)
	}
	none := m.Open(8, 0, nil)
	defer none.Close()
	if len(none.History) != 0 {
		t.Fatalf("tail 0 = без истории, получено %d", len(none.History))
	}
}

// История и живой поток не дублируют и не теряют строки на стыке, даже когда
// пишут параллельно с подпиской.
func TestOpenNoDuplicatesNoGaps(t *testing.T) {
	for round := 0; round < 50; round++ {
		m := NewMux(1000)
		logN(m, 0, 10)
		var wg sync.WaitGroup
		wg.Add(1)
		go func() { defer wg.Done(); logN(m, 10, 200) }()
		sub := m.Open(512, 1000, nil)
		wg.Wait()
		seen := map[string]int{}
		for _, l := range sub.History {
			seen[l.Message]++
		}
		for len(sub.C) > 0 {
			seen[(<-sub.C).Message]++
		}
		sub.Close()
		for i := 0; i < 200; i++ {
			if c := seen[fmt.Sprintf("m%d", i)]; c != 1 {
				t.Fatalf("раунд %d: строка m%d встретилась %d раз", round, i, c)
			}
		}
	}
}

func TestOpenCountsOverflowDrops(t *testing.T) {
	m := NewMux(100)
	sub := m.Open(2, 0, nil)
	defer sub.Close()
	logN(m, 0, 10)
	if sub.Dropped() != 8 {
		t.Fatalf("потеряно %d, нужно 8", sub.Dropped())
	}
}
