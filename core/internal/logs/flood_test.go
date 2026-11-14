// SPDX-License-Identifier: GPL-3.0-only

package logs

import (
	"testing"
	"time"
)

func TestFloodGateSlidingWindow(t *testing.T) {
	base := time.Now()
	g := NewFloodGate(3)
	var allowed int
	for i := 0; i < 10; i++ { // всплеск в одну миллисекунду
		if g.Allow(base.Add(time.Duration(i) * time.Millisecond)) {
			allowed++
		}
	}
	if allowed != 3 {
		t.Fatalf("пропущено %d, лимит 3", allowed)
	}
	// Окно скользящее: на стыке секунд двойной лимит не проходит.
	if g.Allow(base.Add(999 * time.Millisecond)) {
		t.Fatal("строка в пределах секунды от первых трёх пропущена")
	}
	if !g.Allow(base.Add(time.Second)) {
		t.Fatal("после выхода первой метки из окна строка должна пройти")
	}
}

func TestFloodGateDroppedReport(t *testing.T) {
	base := time.Now()
	g := NewFloodGate(1)
	g.Allow(base)
	for i := 0; i < 4; i++ {
		g.Allow(base.Add(time.Duration(i+1) * time.Millisecond))
	}
	if n := g.TakeDropped(base.Add(10*time.Millisecond), false); n != 0 {
		t.Fatalf("всплеск не закончился, сообщать рано: %d", n)
	}
	if n := g.TakeDropped(base.Add(2*time.Second), false); n != 4 {
		t.Fatalf("после затишья сообщено %d, нужно 4", n)
	}
	if n := g.TakeDropped(base.Add(3*time.Second), true); n != 0 {
		t.Fatalf("счётчик не сброшен: %d", n)
	}
}

func TestFloodGateUnlimited(t *testing.T) {
	base := time.Now()
	for _, max := range []int{0, -5} {
		g := NewFloodGate(max)
		for i := 0; i < 1000; i++ {
			if !g.Allow(base) {
				t.Fatalf("лимит %d: строка отброшена", max)
			}
		}
	}
}
