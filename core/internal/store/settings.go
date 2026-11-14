// SPDX-License-Identifier: GPL-3.0-only

package store

import (
	"database/sql"
	"errors"
)

// Settings — key/value хранилище настроек (карта — model.SettingDescriptors;
// значения хранятся как JSON, тип валидирует слой API по дескриптору).

// GetSetting возвращает JSON-значение ключа (false — ключ не задан).
func (s *Store) GetSetting(key string) (string, bool, error) {
	var v string
	err := s.db.QueryRow(`SELECT value FROM settings WHERE key = ?`, key).Scan(&v)
	if errors.Is(err, sql.ErrNoRows) {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	return v, true, nil
}

// SetSetting Upsert-ит значение ключа. Идёт через withTx (см. SetMeta): внутри
// InTx запись входит в транзакцию серии.
func (s *Store) SetSetting(key, value string) error {
	if key == "" {
		return ErrConstraint
	}
	return s.withTx(func(tx *sql.Tx) error {
		_, err := tx.Exec(`INSERT INTO settings (key, value, updated_at_ms) VALUES (?, ?, ?)
			ON CONFLICT(key) DO UPDATE SET value = excluded.value, updated_at_ms = excluded.updated_at_ms`,
			key, value, nowMs())
		return err
	})
}

// Settings возвращает все заданные настройки.
func (s *Store) Settings() (map[string]string, error) {
	rows, err := s.db.Query(`SELECT key, value FROM settings ORDER BY key`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]string{}
	for rows.Next() {
		var k, v string
		if err := rows.Scan(&k, &v); err != nil {
			return nil, err
		}
		out[k] = v
	}
	return out, rows.Err()
}

// DeleteSettings удаляет ключи одной транзакцией (сброс к умолчанию) и
// возвращает те, у которых была запись, в порядке keys. Отсутствующие ключи
// ошибкой не считаются.
func (s *Store) DeleteSettings(keys []string) ([]string, error) {
	var deleted []string
	err := s.withTx(func(tx *sql.Tx) error {
		deleted = nil
		for _, key := range keys {
			res, err := tx.Exec(`DELETE FROM settings WHERE key = ?`, key)
			if err != nil {
				return err
			}
			if n, _ := res.RowsAffected(); n > 0 {
				deleted = append(deleted, key)
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return deleted, nil
}
