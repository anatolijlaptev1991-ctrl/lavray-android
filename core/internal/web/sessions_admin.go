// SPDX-License-Identifier: GPL-3.0-only

package web

// sessions_admin.go — отзыв сессий и их список (панель «Сессии»).

import (
	"sort"
)

// Revoke отзывает сессию по id; false — такой нет.
func (s *Sessions) Revoke(id string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	for k, sess := range s.byToken {
		if sess.ID == id {
			delete(s.byToken, k)
			return true
		}
	}
	return false
}

// RevokeToken отзывает сессию по токену (выход).
func (s *Sessions) RevokeToken(token string) {
	s.mu.Lock()
	delete(s.byToken, tokenKey(token))
	s.mu.Unlock()
}

// RevokeOthers отзывает все сессии пользователя, кроме keepID, и возвращает их число.
func (s *Sessions) RevokeOthers(userID int64, keepID string) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	n := 0
	for k, sess := range s.byToken {
		if sess.UserID == userID && sess.ID != keepID {
			delete(s.byToken, k)
			n++
		}
	}
	return n
}

// List — действующие сессии пользователя (userID == 0 — всех), новые первыми.
func (s *Sessions) List(userID int64) []Session {
	now := s.now()
	s.mu.Lock()
	s.purgeLocked(now)
	out := make([]Session, 0, len(s.byToken))
	for _, sess := range s.byToken {
		if userID == 0 || sess.UserID == userID {
			out = append(out, *sess)
		}
	}
	s.mu.Unlock()
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.After(out[j].CreatedAt) })
	return out
}

// Count — число действующих сессий.
func (s *Sessions) Count() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.purgeLocked(s.now())
	return len(s.byToken)
}
