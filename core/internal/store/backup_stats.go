// SPDX-License-Identifier: GPL-3.0-only

package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
)

// Stats — числа для сводки резервной копии (api.BackupSummary).
type Stats struct {
	Groups        int // все группы, включая подписки
	Profiles      int
	Subscriptions int // группы kind=2
	Presets       int // пользовательские наборы маршрутов (meta)
	Settings      int // настройки без исключённых (update.*)
}

// Stats считает содержимое хранилища, которое попадёт в копию.
func (s *Store) Stats(ctx context.Context) (Stats, error) {
	if s.tx != nil {
		return Stats{}, errors.New("store: Stats недоступен внутри InTx")
	}
	var st Stats
	err := s.db.QueryRowContext(ctx, `SELECT
		(SELECT COUNT(*) FROM groups),
		(SELECT COUNT(*) FROM profiles),
		(SELECT COUNT(*) FROM groups WHERE kind = 2),
		(SELECT COUNT(*) FROM settings`+excludedWhere("settings")+`)`).
		Scan(&st.Groups, &st.Profiles, &st.Subscriptions, &st.Settings)
	if err != nil {
		return Stats{}, fmt.Errorf("store: подсчёт содержимого: %w", err)
	}
	var raw string
	err = s.db.QueryRowContext(ctx, `SELECT value FROM meta WHERE key = ?`, MetaCustomPresets).Scan(&raw)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return Stats{}, fmt.Errorf("store: подсчёт наборов: %w", err)
	}
	var list []json.RawMessage
	if json.Unmarshal([]byte(raw), &list) == nil {
		st.Presets = len(list)
	}
	return st, nil
}
