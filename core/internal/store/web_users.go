// SPDX-License-Identifier: GPL-3.0-only

package store

// web_users.go — пользователи веб-панели (T-76, миграция v5). Хэш пароля
// store не разбирает: формат и проверка — пакет web.

import (
	"database/sql"
	"errors"
	"fmt"
)

// ErrWebAdminExists — первый администратор уже создан (SetupFirstAdmin
// срабатывает один раз).
var ErrWebAdminExists = errors.New("store: web admin already exists")

// WebUser — строка таблицы web_users.
type WebUser struct {
	ID           int64
	Username     string
	PasswordHash string
	CreatedAtMs  int64
	UpdatedAtMs  int64
}

// CreateFirstWebUser создаёт администратора, только если таблица пуста. Проверка
// и вставка — один оператор, поэтому два параллельных вызова не создадут двух
// администраторов.
func (s *Store) CreateFirstWebUser(username, passwordHash string) (*WebUser, error) {
	now := nowMs()
	res, err := s.db.Exec(`INSERT INTO web_users (username, password_hash, created_at_ms, updated_at_ms)
		SELECT ?, ?, ?, ? WHERE NOT EXISTS (SELECT 1 FROM web_users)`,
		username, passwordHash, now, now)
	if err != nil {
		return nil, fmt.Errorf("store: create web user: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return nil, ErrWebAdminExists
	}
	id, err := res.LastInsertId()
	if err != nil {
		return nil, err
	}
	return &WebUser{ID: id, Username: username, PasswordHash: passwordHash, CreatedAtMs: now, UpdatedAtMs: now}, nil
}

// GetWebUser ищет пользователя по имени без учёта регистра (ErrNotFound — нет).
func (s *Store) GetWebUser(username string) (*WebUser, error) {
	var u WebUser
	err := s.db.QueryRow(`SELECT id, username, password_hash, created_at_ms, updated_at_ms
		FROM web_users WHERE username = ?`, username).
		Scan(&u.ID, &u.Username, &u.PasswordHash, &u.CreatedAtMs, &u.UpdatedAtMs)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &u, nil
}

// CountWebUsers — сколько пользователей создано (0 — нужна первичная настройка).
func (s *Store) CountWebUsers() (int, error) {
	var n int
	err := s.db.QueryRow(`SELECT COUNT(*) FROM web_users`).Scan(&n)
	return n, err
}

// SetWebUserPassword заменяет хэш пароля (смена пароля и перехеширование).
func (s *Store) SetWebUserPassword(id int64, passwordHash string) error {
	res, err := s.db.Exec(`UPDATE web_users SET password_hash = ?, updated_at_ms = ? WHERE id = ?`,
		passwordHash, nowMs(), id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}
