// SPDX-License-Identifier: GPL-3.0-only

package store

// sessions.go — жизненный цикл сессии подключения (T-79): старт, обновление
// счётчиков, завершение, закрытие «повисших» после аварии, ограничение
// размера таблицы.

import (
	"database/sql"
	"errors"
	"fmt"
)

// MaxSessions — сколько последних сессий хранит PruneSessions по умолчанию.
const MaxSessions = 10000

// Session — строка таблицы sessions.
type Session struct {
	ID            int64
	ProfileID     *int64 // nil — профиль удалён или не сохранялся (inline)
	GroupID       *int64 // nil — подключение без сохранённого профиля
	Mode          string
	StartedAtMs   int64
	EndedAtMs     *int64 // nil — сессия идёт
	UplinkBytes   int64
	DownlinkBytes int64
	UpdatedAtMs   int64
}

// StartSession создаёт сессию и возвращает её id. Счётчики — 0, updated_at_ms
// равен началу: так «повисшая» сессия без единого сброса получит нулевую длину.
func (s *Store) StartSession(profileID, groupID *int64, mode string, startedAtMs int64) (int64, error) {
	res, err := s.db.Exec(`INSERT INTO sessions
		(profile_id, group_id, mode, started_at_ms, updated_at_ms) VALUES (?, ?, ?, ?, ?)`,
		profileID, groupID, mode, startedAtMs, startedAtMs)
	if err != nil {
		return 0, fmt.Errorf("store: start session: %w", err)
	}
	return res.LastInsertId()
}

// UpdateSession записывает накопленные за сессию байты (абсолютные значения,
// не прибавки: повторная запись тех же чисел ничего не портит).
func (s *Store) UpdateSession(id, uplink, downlink, nowMs int64) error {
	return s.touchSession(id, uplink, downlink, nowMs, false)
}

// EndSession записывает итоговые байты и время завершения. Повторный вызов
// перезаписывает значения тем же способом и не считается ошибкой.
func (s *Store) EndSession(id, uplink, downlink, endedAtMs int64) error {
	return s.touchSession(id, uplink, downlink, endedAtMs, true)
}

func (s *Store) touchSession(id, uplink, downlink, nowMs int64, end bool) error {
	q := `UPDATE sessions SET uplink_bytes = ?, downlink_bytes = ?, updated_at_ms = ?`
	args := []any{uplink, downlink, nowMs}
	if end {
		q += `, ended_at_ms = ?`
		args = append(args, nowMs)
	}
	res, err := s.db.Exec(q+` WHERE id = ?`, append(args, id)...)
	if err != nil {
		return fmt.Errorf("store: update session: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// GetSession возвращает сессию по id.
func (s *Store) GetSession(id int64) (*Session, error) {
	var (
		ss                        Session
		profileID, groupID, ended sql.NullInt64
		mode                      sql.NullString
	)
	err := s.db.QueryRow(`SELECT id, profile_id, group_id, mode, started_at_ms, ended_at_ms,
		uplink_bytes, downlink_bytes, updated_at_ms FROM sessions WHERE id = ?`, id).
		Scan(&ss.ID, &profileID, &groupID, &mode, &ss.StartedAtMs, &ended,
			&ss.UplinkBytes, &ss.DownlinkBytes, &ss.UpdatedAtMs)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("store: get session: %w", err)
	}
	ss.Mode = mode.String
	ss.ProfileID, ss.GroupID, ss.EndedAtMs = nullPtr(profileID), nullPtr(groupID), nullPtr(ended)
	return &ss, nil
}

func nullPtr(v sql.NullInt64) *int64 {
	if !v.Valid {
		return nil
	}
	return &v.Int64
}

// sessionsSchemaVersion — версия схемы, с которой существует таблица sessions.
const sessionsSchemaVersion = 4

// closeDanglingOnOpen — вызов CloseDanglingSessions из Open. Базу, открытую по
// схеме старше v4 (тест старой сборки), не трогает: таблицы sessions в ней нет.
func (s *Store) closeDanglingOnOpen() error {
	v, err := s.SchemaVersion()
	if err != nil || v < sessionsSchemaVersion {
		return err
	}
	_, err = s.CloseDanglingSessions()
	return err
}

// CloseDanglingSessions закрывает сессии, у которых нет времени завершения:
// процесс убили или он упал, не успев вызвать EndSession. Концом считается
// последнее обновление (updated_at_ms) — известное, а не придуманное время.
// Возвращает число закрытых сессий. Вызывается при каждом Open.
func (s *Store) CloseDanglingSessions() (int64, error) {
	res, err := s.db.Exec(`UPDATE sessions SET ended_at_ms = updated_at_ms WHERE ended_at_ms IS NULL`)
	if err != nil {
		return 0, fmt.Errorf("store: close dangling sessions: %w", err)
	}
	return res.RowsAffected()
}

// PruneSessions оставляет keep самых новых сессий (по началу, затем по id) и
// удаляет остальные. Возвращает число удалённых.
func (s *Store) PruneSessions(keep int) (int64, error) {
	if keep <= 0 {
		return 0, fmt.Errorf("store: prune sessions: keep = %d, нужно > 0", keep)
	}
	res, err := s.db.Exec(`DELETE FROM sessions WHERE id NOT IN (
		SELECT id FROM sessions ORDER BY started_at_ms DESC, id DESC LIMIT ?)`, keep)
	if err != nil {
		return 0, fmt.Errorf("store: prune sessions: %w", err)
	}
	return res.RowsAffected()
}
