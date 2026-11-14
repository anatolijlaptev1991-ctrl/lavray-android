// SPDX-License-Identifier: GPL-3.0-only

package store

// traffic.go — накопление трафика профиля и сводка по сессиям (T-79).

import (
	"errors"
	"fmt"
)

// ErrNegativeTraffic — отрицательная прибавка к счётчикам: счётчики только растут.
var ErrNegativeTraffic = errors.New("store: отрицательная прибавка трафика")

// AddTraffic прибавляет к счётчикам профиля отправленные и полученные байты
// одним атомарным UPDATE (uplink_total += ?): параллельные вызовы не теряют
// прибавок, чтения старого значения нет. Ключ traffic в JSON профиля
// обновляется тем же запросом (SQLite считает все выражения SET по прежней
// строке). Профиль удалён или не существует — не ошибка: учёт идёт в фоне, и
// пользователь мог удалить сервер во время сессии. Остальные поля профиля и
// updated_at_ms не меняются (как у SaveTraffic).
func (s *Store) AddTraffic(id, uplink, downlink int64) error {
	if uplink < 0 || downlink < 0 {
		return fmt.Errorf("%w: ↑%d ↓%d", ErrNegativeTraffic, uplink, downlink)
	}
	if uplink == 0 && downlink == 0 {
		return nil
	}
	_, err := s.db.Exec(`UPDATE profiles SET
		uplink_total = uplink_total + ?,
		downlink_total = downlink_total + ?,
		data = json_set(data, '$.traffic', json_object(
			'uplink_bytes', uplink_total + ?, 'downlink_bytes', downlink_total + ?))
	WHERE id = ?`, uplink, downlink, uplink, downlink, id)
	if err != nil {
		return fmt.Errorf("store: add traffic: %w", err)
	}
	return nil
}

// GroupTraffic — итог сессий одной группы за период.
type GroupTraffic struct {
	GroupID       int64
	UplinkBytes   int64
	DownlinkBytes int64
	SessionCount  int64
	FirstStartMs  int64 // начало самой ранней сессии
	LastStartMs   int64 // начало самой поздней сессии
}

// SumSessions суммирует сессии по группам. Берутся сессии, начавшиеся не
// раньше sinceMs (граница включительно; 0 — за всё время). groupID != nil
// ограничивает одной группой. Сессии без группы (inline-профиль) в сводку не
// входят. Порядок — по id группы.
func (s *Store) SumSessions(groupID *int64, sinceMs int64) ([]GroupTraffic, error) {
	q := `SELECT group_id, COALESCE(SUM(uplink_bytes), 0), COALESCE(SUM(downlink_bytes), 0),
		COUNT(*), MIN(started_at_ms), MAX(started_at_ms)
		FROM sessions WHERE group_id IS NOT NULL AND started_at_ms >= ?`
	args := []any{sinceMs}
	if groupID != nil {
		q += ` AND group_id = ?`
		args = append(args, *groupID)
	}
	rows, err := s.db.Query(q+` GROUP BY group_id ORDER BY group_id`, args...)
	if err != nil {
		return nil, fmt.Errorf("store: sum sessions: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var out []GroupTraffic
	for rows.Next() {
		var g GroupTraffic
		if err := rows.Scan(&g.GroupID, &g.UplinkBytes, &g.DownlinkBytes,
			&g.SessionCount, &g.FirstStartMs, &g.LastStartMs); err != nil {
			return nil, fmt.Errorf("store: sum sessions: %w", err)
		}
		out = append(out, g)
	}
	return out, rows.Err()
}
