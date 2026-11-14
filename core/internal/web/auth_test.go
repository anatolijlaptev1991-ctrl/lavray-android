// SPDX-License-Identifier: GPL-3.0-only

package web

import (
	"errors"
	"strings"
	"sync"
	"testing"
	"time"
)

func newTestAuth(t *testing.T) (*Auth, *fakeClock) {
	t.Helper()
	clk := newFakeClock()
	users, _ := newUsers(t)
	return NewAuth(users, NewSessions(clk.Now), NewLimiter(clk.Now, LimiterConfig{})), clk
}

var lan = ClientInfo{IP: "192.168.1.50", Label: "Firefox"}

func TestAuthSetupAndLogin(t *testing.T) {
	a, _ := newTestAuth(t)
	if _, err := a.Login("admin", goodPass, lan); !errors.Is(err, ErrNoAdmin) {
		t.Fatalf("вход до настройки: %v", err)
	}
	setup, err := a.SetupFirstAdmin("admin", goodPass, "", lan)
	if err != nil || setup.Token == "" || setup.Session.Username != "admin" {
		t.Fatalf("настройка: %+v err=%v", setup, err)
	}
	if _, err := a.SetupFirstAdmin("admin2", goodPass, "", lan); !errors.Is(err, ErrAdminExists) {
		t.Fatalf("повторная настройка: %v", err)
	}
	res, err := a.Login("admin", goodPass, lan)
	if err != nil || res.Token == setup.Token {
		t.Fatalf("вход: %+v err=%v", res, err)
	}
	if _, ok := a.Sessions.Validate(res.Token); !ok {
		t.Fatal("сессия после входа не действует")
	}
	if _, err := a.SetupFirstAdmin("x", goodPass, "", lan); !errors.Is(err, ErrAdminExists) {
		t.Fatalf("настройка при существующем администраторе: %v", err)
	}
}

func TestSetupRejectsWeakPassword(t *testing.T) {
	a, _ := newTestAuth(t)
	if _, err := a.SetupFirstAdmin("admin", "short", "", lan); !errors.Is(err, ErrWeakPassword) {
		t.Fatalf("слабый пароль: %v", err)
	}
	if need, _ := a.Users.NeedsSetup(); !need {
		t.Fatal("администратор создан со слабым паролем")
	}
}

// TestAuthLockout — неудачи копятся по записи и закрывают вход даже верному
// паролю, пока не пройдёт пауза.
func TestAuthLockout(t *testing.T) {
	a, clk := newTestAuth(t)
	if _, err := a.SetupFirstAdmin("admin", goodPass, "", lan); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 5; i++ {
		if _, err := a.Login("admin", "wrong-password-1", lan); !errors.Is(err, ErrBadCredentials) {
			t.Fatalf("неудача %d: %v", i+1, err)
		}
	}
	_, err := a.Login("admin", goodPass, lan)
	var rl *RateLimitError
	if !errors.As(err, &rl) || rl.RetrySeconds() != 1 {
		t.Fatalf("после 5 неудач верный пароль: err=%v", err)
	}
	clk.Advance(time.Second)
	if _, err := a.Login("admin", goodPass, lan); err != nil {
		t.Fatalf("после паузы: %v", err)
	}
}

func TestAuthUnknownUserCountsAsFailure(t *testing.T) {
	a, _ := newTestAuth(t)
	_, _ = a.SetupFirstAdmin("admin", goodPass, "", lan)
	for i := 0; i < 5; i++ {
		_, _ = a.Login("ghost", "whatever-pass-1", lan)
	}
	var rl *RateLimitError
	if _, err := a.Login("ghost", goodPass, lan); !errors.As(err, &rl) {
		t.Fatalf("перебор несуществующего имени не ограничен: %v", err)
	}
}

// TestChangePasswordRevokesOthers — смена пароля гасит все сессии, кроме текущей.
func TestChangePasswordRevokesOthers(t *testing.T) {
	a, _ := newTestAuth(t)
	cur, _ := a.SetupFirstAdmin("admin", goodPass, "", lan)
	other1, _ := a.Login("admin", goodPass, lan)
	other2, _ := a.Login("admin", goodPass, lan)

	if _, err := a.ChangePassword(cur.Session, "wrong-password-1", "brand-new-pass-1", lan.IP); !errors.Is(err, ErrBadCredentials) {
		t.Fatalf("неверный прежний пароль: %v", err)
	}
	n, err := a.ChangePassword(cur.Session, goodPass, "brand-new-pass-1", lan.IP)
	if err != nil || n != 2 {
		t.Fatalf("revoked=%d err=%v", n, err)
	}
	if _, ok := a.Sessions.Validate(cur.Token); !ok {
		t.Fatal("текущая сессия отозвана")
	}
	for _, tok := range []string{other1.Token, other2.Token} {
		if _, ok := a.Sessions.Validate(tok); ok {
			t.Fatal("чужая сессия пережила смену пароля")
		}
	}
	if _, err := a.Login("admin", "brand-new-pass-1", lan); err != nil {
		t.Fatalf("новый пароль: %v", err)
	}
}

// countingRepo считает обращения к хранилищу пользователей: сколько попыток
// дошло до проверки пароля.
type countingRepo struct {
	*memRepo
	mu   sync.Mutex
	gets int
}

func (c *countingRepo) Get(name string) (UserRecord, error) {
	c.mu.Lock()
	c.gets++
	c.mu.Unlock()
	return c.memRepo.Get(name)
}

// TestParallelLoginsRespectLockout — пачка параллельных входов не обходит
// блокировку: до проверки пароля доходят только первые 5 попыток (C, S).
func TestParallelLoginsRespectLockout(t *testing.T) {
	clk := newFakeClock()
	repo := &countingRepo{memRepo: &memRepo{}}
	a := NewAuth(NewUsers(repo), NewSessions(clk.Now), NewLimiter(clk.Now, LimiterConfig{}))
	if _, err := a.SetupFirstAdmin("admin", goodPass, "", lan); err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	for i := 0; i < 40; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _ = a.Login("admin", "wrong-password-1", lan)
		}()
	}
	wg.Wait()
	repo.mu.Lock()
	defer repo.mu.Unlock()
	if repo.gets > 5 {
		t.Fatalf("до проверки пароля дошло %d попыток из 40, ждали не больше 5", repo.gets)
	}
}

// TestLoginRejectsOverlongInput — гигантские имя и пароль отклоняются до расчёта
// хэша и считаются неудачей (S, B).
func TestLoginRejectsOverlongInput(t *testing.T) {
	a, _ := newTestAuth(t)
	_, _ = a.SetupFirstAdmin("admin", goodPass, "", lan)
	for name, in := range map[string][2]string{
		"имя":    {strings.Repeat("a", 5_000_000), goodPass},
		"пароль": {"admin", strings.Repeat("p", 1000)},
	} {
		start := time.Now()
		if _, err := a.Login(in[0], in[1], lan); !errors.Is(err, ErrBadCredentials) {
			t.Errorf("%s: err=%v, ждали ErrBadCredentials", name, err)
		}
		if time.Since(start) > 2*time.Second {
			t.Errorf("%s: отказ занял %v", name, time.Since(start))
		}
	}
	// Границы допустимого проходят до проверки пароля (имя не существует).
	if _, err := a.Login(strings.Repeat("a", 64), goodPass, lan); !errors.Is(err, ErrBadCredentials) {
		t.Fatalf("имя в 64 символа: %v", err)
	}
}

// TestSetupCodeGate — при включённом коде настройка без верного кода закрыта;
// после успеха код потребляется (S, D).
func TestSetupCodeGate(t *testing.T) {
	a, _ := newTestAuth(t)
	sc, err := NewSetupCode(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	a.SetupCode = sc
	for _, bad := range []string{"", "WRONGCODE1", strings.Repeat("A", 100000)} {
		if _, err := a.SetupFirstAdmin("admin", goodPass, bad, lan); !errors.Is(err, ErrBadSetupCode) {
			t.Fatalf("код %.12q: err=%v", bad, err)
		}
	}
	if need, _ := a.Users.NeedsSetup(); !need {
		t.Fatal("администратор создан без верного кода")
	}
	res, err := a.SetupFirstAdmin("admin", goodPass, strings.ToLower(sc.Code()), lan)
	if err != nil || res.Token == "" {
		t.Fatalf("верный код: %v", err)
	}
	if sc.Code() != "" {
		t.Fatal("код не потреблён после создания администратора")
	}
}

// TestSetupCodeBruteForceIsLimited — перебор кода упирается в лимитер (C, S).
func TestSetupCodeBruteForceIsLimited(t *testing.T) {
	a, _ := newTestAuth(t)
	sc, _ := NewSetupCode(t.TempDir())
	a.SetupCode = sc
	for i := 0; i < 5; i++ {
		_, _ = a.SetupFirstAdmin("admin", goodPass, "WRONGCODE1", lan)
	}
	var rl *RateLimitError
	if _, err := a.SetupFirstAdmin("admin", goodPass, sc.Code(), lan); !errors.As(err, &rl) {
		t.Fatalf("после 5 неверных кодов даже верный не пускает сразу: %v", err)
	}
}

// TestSetupAfterAdminIsCheap — когда администратор есть, SetupFirstAdmin
// отказывает сразу: не тратит общий лимит попыток и не считает хэш (S).
func TestSetupAfterAdminIsCheap(t *testing.T) {
	clk := newFakeClock()
	users, _ := newUsers(t)
	a := NewAuth(users, NewSessions(clk.Now), NewLimiter(clk.Now, LimiterConfig{GlobalPerMinute: 3}))
	if _, err := a.SetupFirstAdmin("admin", goodPass, "", lan); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 50; i++ {
		if _, err := a.SetupFirstAdmin("x", goodPass, "", lan); !errors.Is(err, ErrAdminExists) {
			t.Fatalf("попытка %d: %v", i, err)
		}
	}
	if _, err := a.Login("admin", goodPass, lan); err != nil {
		t.Fatalf("вход закрыт чужими попытками настройки: %v", err)
	}
}
