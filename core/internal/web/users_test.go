// SPDX-License-Identifier: GPL-3.0-only

package web

import (
	"errors"
	"strings"
	"sync"
	"testing"
)

// memRepo — хранилище пользователей в памяти с той же атомарностью, что у
// store.CreateFirstWebUser.
type memRepo struct {
	mu    sync.Mutex
	users []UserRecord
	next  int64
}

func (m *memRepo) CreateFirst(username, hash string) (UserRecord, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if len(m.users) > 0 {
		return UserRecord{}, ErrAdminExists
	}
	m.next++
	rec := UserRecord{ID: m.next, Username: username, PasswordHash: hash}
	m.users = append(m.users, rec)
	return rec, nil
}

func (m *memRepo) Get(username string) (UserRecord, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, u := range m.users {
		if strings.EqualFold(u.Username, username) {
			return u, nil
		}
	}
	return UserRecord{}, ErrUserNotFound
}

func (m *memRepo) Count() (int, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.users), nil
}

func (m *memRepo) SetPassword(id int64, hash string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for i := range m.users {
		if m.users[i].ID == id {
			m.users[i].PasswordHash = hash
			return nil
		}
	}
	return ErrUserNotFound
}

const goodPass = "correct-horse-1"

func newUsers(t *testing.T) (*Users, *memRepo) {
	t.Helper()
	repo := &memRepo{}
	return NewUsers(repo), repo
}

// TestFirstAdminOnlyOnce — второй администратор не создаётся, в том числе
// при гонке (D).
func TestFirstAdminOnlyOnce(t *testing.T) {
	u, _ := newUsers(t)
	if _, err := u.CreateFirstAdmin("admin", goodPass); err != nil {
		t.Fatal(err)
	}
	if _, err := u.CreateFirstAdmin("second", goodPass); !errors.Is(err, ErrAdminExists) {
		t.Fatalf("второй администратор: err=%v", err)
	}
	if need, _ := u.NeedsSetup(); need {
		t.Fatal("после создания NeedsSetup=true")
	}

	u2, _ := newUsers(t)
	var wg sync.WaitGroup
	var mu sync.Mutex
	created := 0
	for i := 0; i < 6; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := u2.CreateFirstAdmin("admin", goodPass); err == nil {
				mu.Lock()
				created++
				mu.Unlock()
			}
		}()
	}
	wg.Wait()
	if created != 1 {
		t.Fatalf("параллельно создано %d администраторов, ждали 1", created)
	}
}

func TestPasswordPolicy(t *testing.T) {
	cases := []struct {
		pw string
		ok bool
	}{
		{"", false},
		{"123456789", false},              // 9 символов
		{"1234567890", true},              // 10 — граница
		{"пароль-пароль", true},           // кириллица считается символами
		{"паролькос", false},              // 9 рун, но 18 байт: считаем руны
		{"          ", false},             // только пробелы
		{strings.Repeat("a", 129), false}, // длиннее предела
		{strings.Repeat("a", 128), true},
	}
	for _, c := range cases {
		if err := ValidatePassword(c.pw); (err == nil) != c.ok {
			t.Errorf("пароль %q: err=%v, ждали ok=%v", c.pw, err, c.ok)
		}
	}
}

func TestUsernameValidation(t *testing.T) {
	good := []string{"admin", "Админ", "a", strings.Repeat("x", 64), "user.name@lan"}
	bad := []string{"", " admin", "admin ", "ad\nmin", "ad\x00min", strings.Repeat("x", 65), "a\tb"}
	for _, n := range good {
		if _, err := ValidateUsername(n); err != nil {
			t.Errorf("имя %q отклонено: %v", n, err)
		}
	}
	for _, n := range bad {
		if _, err := ValidateUsername(n); !errors.Is(err, ErrBadUsername) {
			t.Errorf("имя %q принято или иная ошибка: %v", n, err)
		}
	}
}

func TestAuthenticate(t *testing.T) {
	u, _ := newUsers(t)
	if _, err := u.Authenticate("admin", goodPass); !errors.Is(err, ErrNoAdmin) {
		t.Fatalf("до настройки: err=%v", err)
	}
	if _, err := u.CreateFirstAdmin("admin", goodPass); err != nil {
		t.Fatal(err)
	}
	if rec, err := u.Authenticate("ADMIN", goodPass); err != nil || rec.Username != "admin" {
		t.Fatalf("вход без учёта регистра: %+v err=%v", rec, err)
	}
	for _, c := range [][2]string{{"admin", "wrong-password-1"}, {"ghost", goodPass}, {"admin", ""}} {
		if _, err := u.Authenticate(c[0], c[1]); !errors.Is(err, ErrBadCredentials) {
			t.Errorf("вход %q: err=%v, ждали ErrBadCredentials", c[0], err)
		}
	}
}

// TestLegacyRehashedOnLogin — вход по pbkdf2-хэшу переписывает его в Argon2id,
// повторный вход хэш не трогает (D).
func TestLegacyRehashedOnLogin(t *testing.T) {
	u, repo := newUsers(t)
	rec, _ := repo.CreateFirst("admin", legacyHash(goodPass, 100000))
	if _, err := u.Authenticate("admin", goodPass); err != nil {
		t.Fatalf("вход по старому хэшу: %v", err)
	}
	after, _ := repo.Get("admin")
	if !strings.HasPrefix(after.PasswordHash, "argon2id$") {
		t.Fatalf("хэш не перехеширован: %q", after.PasswordHash)
	}
	if _, err := u.Authenticate("admin", goodPass); err != nil {
		t.Fatalf("вход после перехеширования: %v", err)
	}
	again, _ := repo.Get("admin")
	if again.PasswordHash != after.PasswordHash || rec.ID != again.ID {
		t.Fatal("хэш перезаписан повторно")
	}
}

func TestChangePassword(t *testing.T) {
	u, _ := newUsers(t)
	rec, _ := u.CreateFirstAdmin("admin", goodPass)
	if err := u.ChangePassword(rec.Username, "wrong-password-1", "brand-new-pass-1"); !errors.Is(err, ErrBadCredentials) {
		t.Fatalf("неверный прежний пароль: err=%v", err)
	}
	if err := u.ChangePassword(rec.Username, goodPass, "short"); !errors.Is(err, ErrWeakPassword) {
		t.Fatalf("слабый новый пароль: err=%v", err)
	}
	if err := u.ChangePassword(rec.Username, goodPass, "brand-new-pass-1"); err != nil {
		t.Fatal(err)
	}
	if _, err := u.Authenticate("admin", goodPass); !errors.Is(err, ErrBadCredentials) {
		t.Fatal("старый пароль всё ещё действует")
	}
	if _, err := u.Authenticate("admin", "brand-new-pass-1"); err != nil {
		t.Fatalf("новый пароль не работает: %v", err)
	}
}
