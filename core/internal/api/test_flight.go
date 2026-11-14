// SPDX-License-Identifier: GPL-3.0-only

package api

// test_flight.go — учёт идущих пакетных тестов (single-flight по профилю).
// Новый TestLatency по профилю, который уже проверяется, отменяет старый
// батч («побеждает последний»), нагрузка не умножается. Батчи с
// непересекающимися профилями идут независимо, а StopTests отменяет все (раньше отмена хранилась одна на процесс, и второй
// батч затирал её у первого).

import (
	"context"

	"github.com/anatolijlaptev1991-ctrl/lavray-android/core/internal/model"
)

// testItem — профиль батча, прочитанный до начала измерений.
type testItem struct {
	p *model.Profile
}

// testRun — идущие батчи: отмены и занятые профили (профиль → номер батча).
type testRun struct {
	next    int
	cancels map[int]context.CancelFunc
	busy    map[int64]int
}

// beginTestBatch регистрирует батч по правилу «побеждает последний»: идущие
// батчи, пересекающиеся с новым по профилям, отменяются, их профили
// освобождаются сразу (как в StopTests), новый стартует без ошибки. Так UI,
// который отменяет запрос и тут же шлёт новый, не упирается в разматывающийся
// старый, а нагрузка не умножается: старые измерения прерваны. finish снимает
// только свои записи.
func (s *Server) beginTestBatch(items []testItem, cancel context.CancelFunc) func() {
	s.testsMu.Lock()
	defer s.testsMu.Unlock()
	if s.testsRunning == nil {
		s.testsRunning = &testRun{}
	}
	r := s.testsRunning
	if r.cancels == nil {
		r.cancels, r.busy = map[int]context.CancelFunc{}, map[int64]int{}
	}
	for _, it := range items {
		if old, taken := r.busy[it.p.ID]; taken {
			r.dropLocked(old)
		}
	}
	r.next++
	id := r.next
	r.cancels[id] = cancel
	for _, it := range items {
		r.busy[it.p.ID] = id
	}
	return func() {
		s.testsMu.Lock()
		defer s.testsMu.Unlock()
		delete(r.cancels, id)
		for _, it := range items {
			if r.busy[it.p.ID] == id {
				delete(r.busy, it.p.ID)
			}
		}
	}
}

// dropLocked отменяет батч и освобождает его профили.
func (r *testRun) dropLocked(id int) {
	if c := r.cancels[id]; c != nil {
		c()
	}
	delete(r.cancels, id)
	for pid, owner := range r.busy {
		if owner == id {
			delete(r.busy, pid)
		}
	}
}

// cancelTestBatches — StopTests: отменяет все батчи и сразу освобождает
// профили, чтобы новый запуск не упирался в ещё разматывающийся старый.
func (s *Server) cancelTestBatches() {
	s.testsMu.Lock()
	defer s.testsMu.Unlock()
	r := s.testsRunning
	if r == nil {
		return
	}
	for _, c := range r.cancels {
		c()
	}
	r.busy = map[int64]int{}
}
