// SPDX-License-Identifier: GPL-3.0-only

package logs

// logs_sub.go — чтение ring-буфера и подписки. История и регистрация
// подписчика берутся под одним захватом Mux.mu: строка, опубликованная
// «на стыке», попадает либо в историю, либо в канал — не в оба и не мимо.

// Tail возвращает копию последних n строк (от старых к новым); n<=0 — весь
// буфер. Копируется только запрошенный хвост.
func (m *Mux) Tail(n int) []Line {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.tailLocked(n)
}

func (m *Mux) tailLocked(n int) []Line {
	src := m.ring
	if n > 0 && n < len(src) {
		src = src[len(src)-n:]
	}
	out := make([]Line, len(src))
	copy(out, src)
	return out
}

// matchingLocked — последние n строк, прошедших filter (nil — все), от старых
// к новым; n<=0 — пусто.
func (m *Mux) matchingLocked(n int, filter func(Line) bool) []Line {
	if n <= 0 {
		return nil
	}
	var rev []Line
	for i := len(m.ring) - 1; i >= 0 && len(rev) < n; i-- {
		if filter == nil || filter(m.ring[i]) {
			rev = append(rev, m.ring[i])
		}
	}
	for i, j := 0, len(rev)-1; i < j; i, j = i+1, j-1 {
		rev[i], rev[j] = rev[j], rev[i]
	}
	return rev
}

// Subscription — живая подписка вместе с историей на момент подписки.
type Subscription struct {
	C       <-chan Line // новые строки после History
	History []Line      // последние строки, прошедшие фильтр, до подписки
	m       *Mux
	s       *subscription
}

// Dropped — сколько строк потеряно из-за переполнения канала подписчика.
func (s *Subscription) Dropped() uint64 { return s.s.dropped.Load() }

// Close отписывает; повторный вызов безопасен.
func (s *Subscription) Close() {
	s.m.mu.Lock()
	delete(s.m.subs, s.s)
	s.m.mu.Unlock()
}

// Open атомарно регистрирует подписчика (канал на buf строк, filter nil = все)
// и берёт историю: последние history строк, прошедших filter (<=0 — без
// истории).
func (m *Mux) Open(buf, history int, filter func(Line) bool) *Subscription {
	if buf <= 0 {
		buf = 64
	}
	s := &subscription{ch: make(chan Line, buf), filter: filter}
	m.mu.Lock()
	hist := m.matchingLocked(history, filter)
	m.subs[s] = struct{}{}
	m.mu.Unlock()
	return &Subscription{C: s.ch, History: hist, m: m, s: s}
}

// Subscribe подписывается на новые строки. Возвращает канал (буфер buf),
// функцию отписки и текущий хвост (tail строк, без фильтра; tail<=0 — весь
// буфер), взятый атомарно с регистрацией. filter nil = все строки.
func (m *Mux) Subscribe(buf int, tail int, filter func(Line) bool) (<-chan Line, func(), []Line) {
	if buf <= 0 {
		buf = 64
	}
	s := &subscription{ch: make(chan Line, buf), filter: filter}
	m.mu.Lock()
	hist := m.tailLocked(tail)
	m.subs[s] = struct{}{}
	m.mu.Unlock()
	sub := &Subscription{m: m, s: s}
	return s.ch, sub.Close, hist
}
