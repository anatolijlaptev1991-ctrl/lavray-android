// SPDX-License-Identifier: GPL-3.0-only

package logs

import "time"

// FloodGate — антифлуд подписчика (max_lines_per_second): не более max строк в
// СКОЛЬЗЯЩУЮ секунду, как FloodGate в shell/coreproc/logs.rs. Время — значения
// time.Time с монотонными часами (time.Now), поэтому смена системных часов
// лимит не ломает. Отброшенные строки считаются; счётчик отдаёт TakeDropped.
// Не потокобезопасен: один подписчик — одна горутина.
type FloodGate struct {
	max      int
	win      []time.Time // метки пропущенных строк в окне, по возрастанию
	dropped  int
	lastDrop time.Time
}

// NewFloodGate — max <= 0 означает «без лимита».
func NewFloodGate(max int) *FloodGate { return &FloodGate{max: max} }

// Allow сообщает, пропускать ли строку, пришедшую в now; отброшенную считает.
func (g *FloodGate) Allow(now time.Time) bool {
	if g.max <= 0 {
		return true
	}
	k := 0
	for k < len(g.win) && now.Sub(g.win[k]) >= time.Second {
		k++
	}
	g.win = g.win[k:]
	if len(g.win) < g.max {
		g.win = append(g.win, now)
		return true
	}
	g.dropped++
	g.lastDrop = now
	return false
}

// TakeDropped возвращает число отброшенных с прошлого вызова и сбрасывает его.
// Без passed возвращает 0, пока всплеск не затих (с последнего отброса не
// прошла секунда): сводка идёт после всплеска, а не посреди него. passed=true —
// строка только что прошла, значит окно уже освободилось.
func (g *FloodGate) TakeDropped(now time.Time, passed bool) int {
	if g.dropped == 0 || (!passed && now.Sub(g.lastDrop) < time.Second) {
		return 0
	}
	n := g.dropped
	g.dropped = 0
	return n
}
