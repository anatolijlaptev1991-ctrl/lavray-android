// SPDX-License-Identifier: GPL-3.0-only

package store

import (
	"database/sql"
	"fmt"
)

// importmap.go — доступ к import_map и к «отметке выданных id» для переноса из
// внешних источников (T-78). Запись идёт через withTx и входит в InTx; чтение —
// только вне InTx (как у остальных читающих методов).

// ImportMapRow — строка import_map: запись источника и её локальная копия.
type ImportMapRow struct {
	Source       string // источник, например "nekobox"
	Kind         string // "group" | "profile" | "preset"
	SourceKey    string // ключ идемпотентности (hex sha256)
	LocalID      string // id локальной копии (число или id набора)
	ImportedAtMs int64
}

// PutImportMap записывает строку; существующий ключ получает новый local_id
// (перенос после удаления локальной копии). Время ставится хранилищем.
func (s *Store) PutImportMap(r ImportMapRow) error {
	if r.Source == "" || r.SourceKey == "" || r.LocalID == "" {
		return fmt.Errorf("%w: import_map: source, source_key и local_id обязательны", ErrConstraint)
	}
	return s.withTx(func(tx *sql.Tx) error {
		_, err := tx.Exec(`INSERT INTO import_map (source, kind, source_key, local_id, imported_at_ms)
			VALUES (?, ?, ?, ?, ?)
			ON CONFLICT(source, kind, source_key) DO UPDATE SET
			  local_id = excluded.local_id, imported_at_ms = excluded.imported_at_ms`,
			r.Source, r.Kind, r.SourceKey, r.LocalID, nowMs())
		if err != nil {
			return fmt.Errorf("%w: import_map: %v", ErrConstraint, err)
		}
		return nil
	})
}

// ImportMapRows возвращает все строки источника.
func (s *Store) ImportMapRows(source string) ([]ImportMapRow, error) {
	rows, err := s.db.Query(`SELECT source, kind, source_key, local_id, imported_at_ms
		FROM import_map WHERE source = ? ORDER BY kind, source_key`, source)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []ImportMapRow
	for rows.Next() {
		var r ImportMapRow
		if err := rows.Scan(&r.Source, &r.Kind, &r.SourceKey, &r.LocalID, &r.ImportedAtMs); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// idTables — таблицы, чьими id управляет перенос.
var idTables = map[string]bool{"groups": true, "profiles": true}

// EntityIDs — множество всех id таблицы ("groups" или "profiles").
func (s *Store) EntityIDs(table string) (map[int64]bool, error) {
	if !idTables[table] {
		return nil, fmt.Errorf("store: EntityIDs: таблица %q не поддерживается", table)
	}
	rows, err := s.db.Query(`SELECT id FROM ` + table)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	out := map[int64]bool{}
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out[id] = true
	}
	return out, rows.Err()
}

func idFloorKey(table string) string { return "store.last_id." + table }

// IDFloor — наибольший id, выданный таблице за всё время: максимум из MAX(id) и
// отметки nextID (L18: id удалённых записей в оборот не возвращаются). Новые id
// переноса начинаются выше.
func (s *Store) IDFloor(table string) (int64, error) {
	if !idTables[table] {
		return 0, fmt.Errorf("store: IDFloor: таблица %q не поддерживается", table)
	}
	var maxID sql.NullInt64
	if err := s.db.QueryRow(`SELECT MAX(id) FROM ` + table).Scan(&maxID); err != nil {
		return 0, err
	}
	floor := nullInt64(maxID)
	raw, ok, err := s.GetMeta(idFloorKey(table))
	if err != nil {
		return 0, err
	}
	var saved int64
	if _, perr := fmt.Sscanf(raw, "%d", &saved); ok && perr == nil && saved > floor {
		floor = saved
	}
	return floor, nil
}

// RaiseIDFloor поднимает отметку выданных id таблицы до id (не опускает).
// Вызывается в транзакции переноса после записи профилей и групп с назначенными
// заранее id: следующий CreateProfile продолжит выше.
func (s *Store) RaiseIDFloor(table string, id int64) error {
	if !idTables[table] {
		return fmt.Errorf("store: RaiseIDFloor: таблица %q не поддерживается", table)
	}
	return s.withTx(func(tx *sql.Tx) error {
		var raw string
		err := tx.QueryRow(`SELECT value FROM meta WHERE key = ?`, idFloorKey(table)).Scan(&raw)
		if err != nil && err != sql.ErrNoRows {
			return err
		}
		var saved int64
		if err == nil {
			if _, perr := fmt.Sscanf(raw, "%d", &saved); perr != nil {
				saved = 0
			}
		}
		if err == nil && saved >= id {
			return nil
		}
		_, err = tx.Exec(`INSERT INTO meta (key, value) VALUES (?, ?)
			ON CONFLICT(key) DO UPDATE SET value = excluded.value`, idFloorKey(table), fmt.Sprintf("%d", id))
		return err
	})
}
