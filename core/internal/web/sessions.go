// SPDX-License-Identifier: GPL-3.0-only

package web

// sessions.go — сессии панели в памяти ядра: не более 64 на пользователя
// (вытесняется давно не активная), простой 12 ч (скользящий), предел жизни
// 7 суток. Перезапуск ядра сессии сбрасывает — вход повторяется. В карте токены
// хранятся хэшем (sha256), id для списка сессий — отдельное случайное значение.

import (
	"crypto/sha256"
	"sync"
	"time"
)

// Лимиты и сроки сессий.
const (
	MaxSessionsPerUser = 64
	maxSessionsTotal   = 1024 // потолок на все учётные записи сразу
	IdleTTL            = 12 * time.Hour
	AbsoluteTTL        = 7 * 24 * time.Hour
)

// Session — снимок сессии (изменения хранилища на копию не влияют).
type Session struct {
	ID         string // для списка и отзыва; не токен
	UserID     int64
	Username   string
	CSRF       string
	Label      string
	RemoteAddr string
	CreatedAt  time.Time
	LastSeen   time.Time
}

// Sessions — хранилище сессий; безопасно для параллельного использования.
type Sessions struct {
	now func() time.Time

	mu      sync.Mutex
	byToken map[[32]byte]*Session
}

// NewSessions создаёт хранилище; now == nil — time.Now.
func NewSessions(now func() time.Time) *Sessions {
	if now == nil {
		now = time.Now
	}
	return &Sessions{now: now, byToken: map[[32]byte]*Session{}}
}

func tokenKey(token string) [32]byte { return sha256.Sum256([]byte(token)) }

// Create открывает сессию пользователя и возвращает токен для cookie. Просроченные
// сессии при этом удаляются; при переполнении вытесняется давно не активная.
func (s *Sessions) Create(user UserRecord, label, remoteAddr string) (string, Session, error) {
	token, err := newToken()
	if err != nil {
		return "", Session{}, err
	}
	csrf, err := newToken()
	if err != nil {
		return "", Session{}, err
	}
	id, err := newToken()
	if err != nil {
		return "", Session{}, err
	}
	now := s.now()
	sess := &Session{
		ID: id[:22], UserID: user.ID, Username: user.Username, CSRF: csrf,
		Label: label, RemoteAddr: remoteAddr, CreatedAt: now, LastSeen: now,
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.purgeLocked(now)
	for s.countLocked(user.ID) >= MaxSessionsPerUser {
		s.evictOldestLocked(user.ID)
	}
	for len(s.byToken) >= maxSessionsTotal {
		s.evictOldestLocked(0)
	}
	s.byToken[tokenKey(token)] = sess
	return token, *sess, nil
}

// Validate возвращает сессию по токену и продлевает простой. Просроченная
// сессия удаляется.
func (s *Sessions) Validate(token string) (Session, bool) {
	if token == "" {
		return Session{}, false
	}
	now := s.now()
	key := tokenKey(token)
	s.mu.Lock()
	defer s.mu.Unlock()
	sess, ok := s.byToken[key]
	if !ok {
		return Session{}, false
	}
	if expired(sess, now) {
		delete(s.byToken, key)
		return Session{}, false
	}
	sess.LastSeen = now
	return *sess, true
}

// ExpiresAt — когда сессия погаснет, если запросов больше не будет: простой от
// последней активности, но не позже предела в 7 суток.
func (s *Sessions) ExpiresAt(sess Session) time.Time {
	idle := sess.LastSeen.Add(IdleTTL)
	abs := sess.CreatedAt.Add(AbsoluteTTL)
	if abs.Before(idle) {
		return abs
	}
	return idle
}

func expired(sess *Session, now time.Time) bool {
	return now.Sub(sess.LastSeen) >= IdleTTL || now.Sub(sess.CreatedAt) >= AbsoluteTTL
}

func (s *Sessions) purgeLocked(now time.Time) {
	for k, sess := range s.byToken {
		if expired(sess, now) {
			delete(s.byToken, k)
		}
	}
}

func (s *Sessions) countLocked(userID int64) int {
	n := 0
	for _, sess := range s.byToken {
		if sess.UserID == userID {
			n++
		}
	}
	return n
}

// evictOldestLocked удаляет сессию пользователя (0 — любую) с самой давней активностью.
func (s *Sessions) evictOldestLocked(userID int64) {
	var oldestKey [32]byte
	var oldest *Session
	for k, sess := range s.byToken {
		if userID != 0 && sess.UserID != userID {
			continue
		}
		if oldest == nil || sess.LastSeen.Before(oldest.LastSeen) {
			oldestKey, oldest = k, sess
		}
	}
	if oldest != nil {
		delete(s.byToken, oldestKey)
	}
}
