// SPDX-License-Identifier: GPL-3.0-only

package web

import (
	"strconv"
	"testing"
	"time"
)

func newTestLimiter(clk *fakeClock) *Limiter {
	return NewLimiter(clk.Now, LimiterConfig{})
}

// fail фиксирует неудачу, предварительно проверив, что попытка допущена.
func fail(t *testing.T, l *Limiter, account, ip string) {
	t.Helper()
	if d := l.Allow(account, ip); d != 0 {
		t.Fatalf("попытка не допущена (ждать %v)", d)
	}
	l.Failure(account, ip)
}

// TestLockoutAfterFiveFailures — после 5 неудач вход закрывается на
// экспоненциально растущую паузу, после 10 — на 15 минут (C).
func TestLockoutAfterFiveFailures(t *testing.T) {
	clk := newFakeClock()
	l := newTestLimiter(clk)
	for i := 0; i < 4; i++ {
		fail(t, l, "admin", "10.0.0.1")
	}
	if d := l.Allow("admin", "10.0.0.1"); d != 0 {
		t.Fatalf("после 4 неудач вход закрыт на %v", d)
	}
	l.Failure("admin", "10.0.0.1") // 5-я
	if d := l.Allow("admin", "10.0.0.1"); d != time.Second {
		t.Fatalf("пауза после 5-й неудачи = %v, ждали 1s", d)
	}
	clk.Advance(time.Second)
	for i, want := range []time.Duration{2, 4, 8, 16} { // неудачи 6..9
		fail(t, l, "admin", "10.0.0.1")
		if d := l.Allow("admin", "10.0.0.1"); d != want*time.Second {
			t.Fatalf("пауза после неудачи %d = %v, ждали %v", 6+i, d, want*time.Second)
		}
		clk.Advance(want * time.Second)
	}
	fail(t, l, "admin", "10.0.0.1") // 10-я
	if d := l.Allow("admin", "10.0.0.1"); d != 15*time.Minute {
		t.Fatalf("блок после 10 неудач = %v, ждали 15m", d)
	}
	clk.Advance(15*time.Minute - time.Second)
	if d := l.Allow("admin", "10.0.0.1"); d != time.Second {
		t.Fatalf("за секунду до конца блока осталось %v", d)
	}
	clk.Advance(time.Second)
	if d := l.Allow("admin", "10.0.0.1"); d != 0 {
		t.Fatalf("блок не снялся: %v", d)
	}
	// После блока счётчик не обнуляется: следующая неудача снова даёт паузу.
	l.Failure("admin", "10.0.0.1")
	if d := l.Allow("admin", "10.0.0.1"); d == 0 {
		t.Fatal("после блока счётчик обнулился целиком")
	}
}

// TestLockoutPerAccount — блокировка одной учётной записи не задевает другую,
// даже с того же адреса, и обход блока сменой адреса не работает (C, S).
func TestLockoutPerAccount(t *testing.T) {
	clk := newFakeClock()
	l := newTestLimiter(clk)
	for i := 0; i < 5; i++ {
		fail(t, l, "admin", "10.0.0.1")
	}
	if d := l.Allow("other", "10.0.0.1"); d != 0 {
		t.Fatalf("чужая запись заблокирована: %v", d)
	}
	if d := l.Allow("admin", "10.0.0.99"); d == 0 {
		t.Fatal("смена адреса обошла блокировку записи")
	}
	if d := l.Allow("  ADMIN ", "10.0.0.2"); d == 0 {
		t.Fatal("регистр и пробелы в имени обошли блокировку")
	}
}

func TestSuccessResetsAccount(t *testing.T) {
	clk := newFakeClock()
	l := newTestLimiter(clk)
	for i := 0; i < 4; i++ {
		fail(t, l, "admin", "10.0.0.1")
	}
	l.Success("admin", "10.0.0.1")
	for i := 0; i < 4; i++ {
		fail(t, l, "admin", "10.0.0.1")
	}
	if d := l.Allow("admin", "10.0.0.1"); d != 0 {
		t.Fatalf("успех не сбросил счётчик: %v", d)
	}
}

func TestIPLimit(t *testing.T) {
	clk := newFakeClock()
	l := NewLimiter(clk.Now, LimiterConfig{IPFailures: 6, IPWindow: time.Minute, IPBlock: 2 * time.Minute})
	for i := 0; i < 6; i++ {
		fail(t, l, "user"+strconv.Itoa(i), "10.0.0.7")
	}
	if d := l.Allow("fresh", "10.0.0.7"); d != 2*time.Minute {
		t.Fatalf("блок адреса = %v, ждали 2m", d)
	}
	if d := l.Allow("fresh", "10.0.0.8"); d != 0 {
		t.Fatalf("блок адреса задел другой адрес: %v", d)
	}
	clk.Advance(2 * time.Minute)
	if d := l.Allow("fresh", "10.0.0.7"); d != 0 {
		t.Fatalf("блок адреса не снялся: %v", d)
	}
}

// TestGlobalRateLimit — общий потолок попыток в минуту (C).
func TestGlobalRateLimit(t *testing.T) {
	clk := newFakeClock()
	l := NewLimiter(clk.Now, LimiterConfig{GlobalPerMinute: 10})
	for i := 0; i < 10; i++ {
		if d := l.Allow("u"+strconv.Itoa(i), "10.0.1."+strconv.Itoa(i)); d != 0 {
			t.Fatalf("попытка %d отклонена: %v", i, d)
		}
	}
	d := l.Allow("another", "10.0.2.2")
	if d <= 0 || d > time.Minute {
		t.Fatalf("11-я попытка за минуту: пауза %v", d)
	}
	clk.Advance(time.Minute)
	if d := l.Allow("another", "10.0.2.2"); d != 0 {
		t.Fatalf("окно не сдвинулось: %v", d)
	}
}

// TestLimiterMapsAreBounded — тысячи разных имён и адресов не раздувают
// карты сверх предела (B, S).
func TestLimiterMapsAreBounded(t *testing.T) {
	clk := newFakeClock()
	l := NewLimiter(clk.Now, LimiterConfig{MaxEntries: 50, GlobalPerMinute: 1 << 30})
	for i := 0; i < 2000; i++ {
		acc, ip := "acc"+strconv.Itoa(i), "10.1."+strconv.Itoa(i/250)+"."+strconv.Itoa(i%250)
		l.Allow(acc, ip)
		l.Failure(acc, ip)
		clk.Advance(time.Millisecond)
	}
	accounts, ips := l.Sizes()
	if accounts > 50 || ips > 50 {
		t.Fatalf("карты выросли: accounts=%d ips=%d (предел 50)", accounts, ips)
	}
}

// TestLimiterKeepsLockedEntries — при переполнении вытесняются неблокированные
// записи, а не та, что держит блок.
func TestLimiterKeepsLockedEntries(t *testing.T) {
	clk := newFakeClock()
	l := NewLimiter(clk.Now, LimiterConfig{MaxEntries: 20, GlobalPerMinute: 1 << 30})
	for i := 0; i < 10; i++ {
		l.Failure("victim", "10.0.0.1")
	}
	for i := 0; i < 200; i++ {
		l.Failure("noise"+strconv.Itoa(i), "10.2.0."+strconv.Itoa(i%250))
		clk.Advance(time.Millisecond)
	}
	if d := l.Allow("victim", "10.0.0.9"); d == 0 {
		t.Fatal("блокировка записи вытеснена потоком чужих неудач")
	}
}

func TestLimiterLongAccountNameIsBounded(t *testing.T) {
	l := newTestLimiter(newFakeClock())
	long := make([]byte, 100000)
	for i := range long {
		long[i] = 'a'
	}
	l.Failure(string(long), "10.0.0.1")
	if key := normalizeAccount(string(long)); len(key) > 128 {
		t.Fatalf("ключ записи не ограничен: %d байт", len(key))
	}
}
