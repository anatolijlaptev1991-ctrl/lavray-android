// SPDX-License-Identifier: GPL-3.0-only

package traffic

import (
	"slices"
	"sync"
	"time"
)

// FakeSource — детерминированный Source для тестов api, inst и mcp. Состоянием
// управляет тест: Open добавляет соединение, AddBytes наращивает счётчики,
// Finish закрывает. Close (как у настоящего источника) закрывает соединение и
// запоминает вызов.
type FakeSource struct {
	mu     sync.Mutex
	active []ConnInfo
	closed []ConnInfo
	// foldedUp/Down — байты очищенных закрытых соединений: остаются в итогах.
	foldedUp, foldedDown int64
	subs                 map[int]chan Event
	nextID               int
	calls                []string
	now                  func() time.Time
}

// NewFakeSource создаёт пустой источник.
func NewFakeSource() *FakeSource {
	return &FakeSource{subs: map[int]chan Event{}, now: time.Now}
}

// Open добавляет активное соединение и рассылает EventOpened.
func (f *FakeSource) Open(c ConnInfo) {
	f.mu.Lock()
	f.active = append(f.active, c)
	f.emitLocked(Event{Kind: EventOpened, ID: c.ID})
	f.mu.Unlock()
}

// AddBytes прибавляет байты активному соединению; false — такого нет.
func (f *FakeSource) AddBytes(id string, up, down int64) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	i := f.indexLocked(id)
	if i < 0 {
		return false
	}
	f.active[i].Up += up
	f.active[i].Down += down
	return true
}

// Finish закрывает активное соединение с временем endMs (0 — «сейчас») и
// рассылает EventClosed; false — такого нет.
func (f *FakeSource) Finish(id string, endMs int64) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	i := f.indexLocked(id)
	if i < 0 {
		return false
	}
	c := f.active[i]
	f.active = slices.Delete(f.active, i, i+1)
	if endMs == 0 {
		endMs = f.now().UnixMilli()
	}
	c.EndMs = endMs
	f.closed = append(f.closed, c)
	f.emitLocked(Event{Kind: EventClosed, ID: id})
	return true
}

func (f *FakeSource) indexLocked(id string) int {
	return slices.IndexFunc(f.active, func(c ConnInfo) bool { return c.ID == id })
}

// emitLocked рассылает событие без блокировки: переполненный подписчик
// пропускает его, как у настоящего источника.
func (f *FakeSource) emitLocked(ev Event) {
	for _, ch := range f.subs {
		select {
		case ch <- ev:
		default:
		}
	}
}

// Active возвращает копию списка активных соединений.
func (f *FakeSource) Active() []ConnInfo {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.active)
}

// Closed возвращает копию списка закрытых соединений.
func (f *FakeSource) Closed() []ConnInfo {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.closed)
}

// ClearClosed очищает список закрытых соединений, как sing-box при сборке
// мусора: самих соединений уже не видно, а их байты остаются в итогах.
func (f *FakeSource) ClearClosed() {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, c := range f.closed {
		f.foldedUp += c.Up
		f.foldedDown += c.Down
	}
	f.closed = nil
}

// Total суммирует байты всех соединений: активных, закрытых и очищенных.
func (f *FakeSource) Total() (up, down int64) {
	f.mu.Lock()
	defer f.mu.Unlock()
	up, down = f.foldedUp, f.foldedDown
	for _, list := range [][]ConnInfo{f.active, f.closed} {
		for _, c := range list {
			up += c.Up
			down += c.Down
		}
	}
	return up, down
}

// Close закрывает активное соединение; ErrNotFound — такого нет или уже закрыто.
func (f *FakeSource) Close(id string) error {
	f.mu.Lock()
	f.calls = append(f.calls, id)
	f.mu.Unlock()
	if !f.Finish(id, 0) {
		return ErrNotFound
	}
	return nil
}

// CloseCalls — id, которые просили закрыть (в порядке вызовов).
func (f *FakeSource) CloseCalls() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.calls)
}

// Subscribe подписывает на события; cancel отписывает и закрывает канал.
func (f *FakeSource) Subscribe() (<-chan Event, func()) {
	ch := make(chan Event, 64)
	f.mu.Lock()
	id := f.nextID
	f.nextID++
	f.subs[id] = ch
	f.mu.Unlock()
	return ch, func() {
		f.mu.Lock()
		defer f.mu.Unlock()
		if _, ok := f.subs[id]; ok {
			delete(f.subs, id)
			close(ch)
		}
	}
}

// Subscribers — сколько подписок ещё не отменено (проверка утечек в тестах).
func (f *FakeSource) Subscribers() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.subs)
}

var _ Source = (*FakeSource)(nil)
