// SPDX-License-Identifier: GPL-3.0-only

package web

import (
	"strconv"
	"sync"
	"testing"
	"time"
)

// fakeClock — управляемое время для TTL и лимитера.
type fakeClock struct {
	mu sync.Mutex
	t  time.Time
}

func newFakeClock() *fakeClock { return &fakeClock{t: time.Date(2026, 10, 31, 12, 0, 0, 0, time.UTC)} }

func (c *fakeClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.t
}

func (c *fakeClock) Advance(d time.Duration) {
	c.mu.Lock()
	c.t = c.t.Add(d)
	c.mu.Unlock()
}

var admin = UserRecord{ID: 1, Username: "admin"}

func TestSessionCreateAndValidate(t *testing.T) {
	clk := newFakeClock()
	s := NewSessions(clk.Now)
	tok, sess, err := s.Create(admin, "Firefox", "10.0.0.5")
	if err != nil {
		t.Fatal(err)
	}
	if tok == "" || sess.ID == "" || sess.CSRF == "" || sess.ID == tok {
		t.Fatalf("сессия: tok=%q %+v", tok, sess)
	}
	got, ok := s.Validate(tok)
	if !ok || got.Username != "admin" || got.CSRF != sess.CSRF || got.RemoteAddr != "10.0.0.5" {
		t.Fatalf("Validate: ok=%v %+v", ok, got)
	}
	for _, bad := range []string{"", "nope", tok + "x", tok[:len(tok)-1]} {
		if _, ok := s.Validate(bad); ok {
			t.Errorf("токен %q принят", bad)
		}
	}
}

// TestSessionsAreEvicted — не более 64 сессий на пользователя: старейшая по
// активности вытесняется (B).
func TestSessionsAreEvicted(t *testing.T) {
	clk := newFakeClock()
	s := NewSessions(clk.Now)
	tokens := make([]string, 0, MaxSessionsPerUser+10)
	for i := 0; i < MaxSessionsPerUser; i++ {
		tok, _, err := s.Create(admin, "c"+strconv.Itoa(i), "10.0.0.1")
		if err != nil {
			t.Fatal(err)
		}
		tokens = append(tokens, tok)
		clk.Advance(time.Second)
	}
	// Первая сессия активна: вытеснена должна быть вторая, а не она.
	if _, ok := s.Validate(tokens[0]); !ok {
		t.Fatal("первая сессия пропала до лимита")
	}
	clk.Advance(time.Second)
	for i := 0; i < 9; i++ {
		tok, _, _ := s.Create(admin, "extra", "10.0.0.1")
		tokens = append(tokens, tok)
		clk.Advance(time.Second)
		if n := len(s.List(admin.ID)); n > MaxSessionsPerUser {
			t.Fatalf("после %d лишних сессий в списке %d", i+1, n)
		}
	}
	if n := s.Count(); n != MaxSessionsPerUser {
		t.Fatalf("всего сессий %d, ждали %d", n, MaxSessionsPerUser)
	}
	if _, ok := s.Validate(tokens[1]); ok {
		t.Fatal("давно не активная сессия не вытеснена")
	}
	if _, ok := s.Validate(tokens[len(tokens)-1]); !ok {
		t.Fatal("новая сессия вытеснена")
	}
}

func TestSessionsLimitIsPerUser(t *testing.T) {
	s := NewSessions(newFakeClock().Now)
	other := UserRecord{ID: 2, Username: "second"}
	tok, _, _ := s.Create(other, "", "")
	for i := 0; i < MaxSessionsPerUser+5; i++ {
		_, _, _ = s.Create(admin, "", "")
	}
	if _, ok := s.Validate(tok); !ok {
		t.Fatal("сессия другого пользователя вытеснена")
	}
}

// TestSessionIdleAndAbsoluteTTL — простой 12 ч и предел 7 суток (X).
func TestSessionIdleAndAbsoluteTTL(t *testing.T) {
	clk := newFakeClock()
	s := NewSessions(clk.Now)
	tok, _, _ := s.Create(admin, "", "")

	clk.Advance(IdleTTL - time.Minute)
	if _, ok := s.Validate(tok); !ok {
		t.Fatal("сессия погасла раньше простоя в 12 ч")
	}
	clk.Advance(IdleTTL) // 12 ч без запросов после последнего
	if _, ok := s.Validate(tok); ok {
		t.Fatal("сессия пережила простой в 12 ч")
	}

	tok2, _, _ := s.Create(admin, "", "")
	for i := 0; i < 27; i++ { // 27 × 6 ч = 6 сут 18 ч: сессия жива благодаря запросам
		clk.Advance(6 * time.Hour)
		if _, ok := s.Validate(tok2); !ok {
			t.Fatalf("активная сессия погасла на шаге %d", i)
		}
	}
	clk.Advance(6 * time.Hour) // 7 суток от создания
	if _, ok := s.Validate(tok2); ok {
		t.Fatal("сессия пережила абсолютный предел в 7 суток")
	}
	if s.Count() != 0 {
		t.Fatalf("истёкшие сессии остались в памяти: %d", s.Count())
	}
}

func TestSessionExpiresAt(t *testing.T) {
	clk := newFakeClock()
	s := NewSessions(clk.Now)
	_, sess, _ := s.Create(admin, "", "")
	if got := s.ExpiresAt(sess); !got.Equal(sess.CreatedAt.Add(IdleTTL)) {
		t.Fatalf("ExpiresAt = %v", got)
	}
	old := sess
	old.LastSeen = old.CreatedAt.Add(AbsoluteTTL - time.Hour)
	if got := s.ExpiresAt(old); !got.Equal(old.CreatedAt.Add(AbsoluteTTL)) {
		t.Fatalf("предел 7 суток не учтён: %v", got)
	}
}

func TestSessionRevoke(t *testing.T) {
	s := NewSessions(newFakeClock().Now)
	tokA, a, _ := s.Create(admin, "a", "")
	tokB, b, _ := s.Create(admin, "b", "")
	tokC, _, _ := s.Create(admin, "c", "")
	if !s.Revoke(a.ID) {
		t.Fatal("Revoke по id не нашёл сессию")
	}
	if s.Revoke(a.ID) {
		t.Fatal("повторный Revoke вернул true")
	}
	if _, ok := s.Validate(tokA); ok {
		t.Fatal("отозванная сессия действует")
	}
	if n := s.RevokeOthers(admin.ID, b.ID); n != 1 {
		t.Fatalf("RevokeOthers отозвал %d, ждали 1", n)
	}
	if _, ok := s.Validate(tokB); !ok {
		t.Fatal("текущая сессия отозвана")
	}
	if _, ok := s.Validate(tokC); ok {
		t.Fatal("чужая сессия пережила RevokeOthers")
	}
	s.RevokeToken(tokB)
	if s.Count() != 0 {
		t.Fatal("RevokeToken не удалил сессию")
	}
}

func TestSessionListFilter(t *testing.T) {
	s := NewSessions(newFakeClock().Now)
	other := UserRecord{ID: 2, Username: "second"}
	_, _, _ = s.Create(admin, "", "")
	_, _, _ = s.Create(admin, "", "")
	_, _, _ = s.Create(other, "", "")
	if n := len(s.List(admin.ID)); n != 2 {
		t.Fatalf("сессий admin: %d", n)
	}
	if n := len(s.List(0)); n != 3 {
		t.Fatalf("всех сессий: %d", n)
	}
}
