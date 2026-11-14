// SPDX-License-Identifier: GPL-3.0-only

package api

// traffic_session.go — завершение и сброс идущей сессии учёта трафика (T-79):
// кто и когда пишет итог. Сам учёт — traffic_accountant.go.

import (
	"errors"

	"github.com/anatolijlaptev1991-ctrl/lavray-android/core/internal/model"
	"github.com/anatolijlaptev1991-ctrl/lavray-android/core/internal/store"
)

// currentSession — идущая сессия учёта; nil — подключения нет.
func (s *Server) currentSession() *acctSession {
	a := &s.acct
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.cur
}

// endSession завершает именно sess, если она ещё идёт: итоговый сброс и время
// конца. Остановка, захватившая сессию до Stop, не завершит сессию нового
// подключения, успевшего начаться за это время. nil — ничего не делает.
func (s *Server) endSession(sess *acctSession) {
	if sess != nil {
		s.finishSession(sess)
	}
}

// endAccounting завершает идущую сессию. Повторный вызов и вызов без сессии
// ничего не делают.
func (s *Server) endAccounting() {
	s.endSession(s.currentSession())
}

// flushAccounting сбрасывает счётчики идущей сессии сейчас: чтение итогов не
// должно отставать от живого трафика на период таймера.
func (s *Server) flushAccounting() {
	if sess := s.currentSession(); sess != nil {
		s.flushSession(sess, false)
	}
}

// withFlushedAccounting выполняет fn, когда всё набранное идущей сессией уже
// в счётчиках профиля, и не даёт сбросу вклиниться до конца fn. Нужен обнулению
// счётчиков: иначе следующий сброс вернул бы байты, набранные до обнуления.
func (s *Server) withFlushedAccounting(fn func() error) error {
	a := &s.acct
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.cur != nil {
		s.flushLocked(a.cur, false)
	}
	return fn()
}

// resetProfileTraffic обнуляет счётчики трафика профилей ids (FR-UI-211).
// D-04: UpdateProfile колонки трафика не пишет — обнуляет отдельный UPDATE (как
// T-22 для результатов тестов). Несуществующий id пропускается.
func (s *Server) resetProfileTraffic(ids []int64) error {
	return s.withFlushedAccounting(func() error {
		for _, id := range ids {
			if err := s.st.SaveTraffic(id, model.TrafficCounters{}); err != nil && !errors.Is(err, store.ErrNotFound) {
				return mapErr(err)
			}
		}
		return nil
	})
}

// finishSession — итоговый сброс; сессия перестаёт быть текущей. Вызывается из
// нескольких мест (Disconnect, событие остановки, перезапуск): выигрывает первый.
func (s *Server) finishSession(sess *acctSession) {
	a := &s.acct
	a.mu.Lock()
	if a.cur != sess {
		a.mu.Unlock()
		return
	}
	a.cur = nil
	a.mu.Unlock()
	s.flushSession(sess, true)
	close(sess.stop)
}
