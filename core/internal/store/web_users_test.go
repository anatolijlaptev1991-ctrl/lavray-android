// SPDX-License-Identifier: GPL-3.0-only

package store

// web_users_test.go — миграция v5 и таблица web_users (T-76).

import (
	"errors"
	"sync"
	"testing"
)

func TestMigrationV5CreatesWebUsers(t *testing.T) {
	s := openTemp(t)
	if v, err := s.SchemaVersion(); err != nil || v < 5 {
		t.Fatalf("версия схемы %d (err=%v), ожидалась не меньше 5", v, err)
	}
	n, err := s.CountWebUsers()
	if err != nil || n != 0 {
		t.Fatalf("свежая база: users=%d err=%v", n, err)
	}
}

func TestCreateFirstWebUserOnce(t *testing.T) {
	s := openTemp(t)
	u, err := s.CreateFirstWebUser("admin", "hash-1")
	if err != nil || u.ID == 0 || u.Username != "admin" {
		t.Fatalf("первый админ: %+v err=%v", u, err)
	}
	if _, err := s.CreateFirstWebUser("other", "hash-2"); !errors.Is(err, ErrWebAdminExists) {
		t.Fatalf("второй админ: err=%v, ждали ErrWebAdminExists", err)
	}
}

func TestCreateFirstWebUserRace(t *testing.T) {
	s := openTemp(t)
	var wg sync.WaitGroup
	var mu sync.Mutex
	ok := 0
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			if _, err := s.CreateFirstWebUser("admin"+string(rune('a'+i)), "h"); err == nil {
				mu.Lock()
				ok++
				mu.Unlock()
			}
		}(i)
	}
	wg.Wait()
	if ok != 1 {
		t.Fatalf("создано администраторов: %d, ждали ровно 1", ok)
	}
}

func TestWebUserLookupIsCaseInsensitive(t *testing.T) {
	s := openTemp(t)
	if _, err := s.CreateFirstWebUser("Admin", "h"); err != nil {
		t.Fatal(err)
	}
	u, err := s.GetWebUser("aDMIN")
	if err != nil || u.Username != "Admin" {
		t.Fatalf("поиск без учёта регистра: %+v err=%v", u, err)
	}
	if _, err := s.GetWebUser("nobody"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("нет пользователя: err=%v", err)
	}
}

func TestSetWebUserPassword(t *testing.T) {
	s := openTemp(t)
	u, _ := s.CreateFirstWebUser("admin", "old")
	if err := s.SetWebUserPassword(u.ID, "new"); err != nil {
		t.Fatal(err)
	}
	got, _ := s.GetWebUser("admin")
	if got.PasswordHash != "new" {
		t.Fatalf("хэш не обновлён: %q", got.PasswordHash)
	}
	if err := s.SetWebUserPassword(999, "x"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("несуществующий пользователь: err=%v", err)
	}
}
