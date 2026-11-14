// SPDX-License-Identifier: GPL-3.0-only

package store

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/anatolijlaptev1991-ctrl/lavray-android/core/internal/model"
)

// Профили. Колонки — поисковые/метаданные-копии; канонический JSON всей
// модели хранится в profiles.data (core-port-plan §5.2, допущение 3).

const sortKeyStep = 1024 // вставка со сдвигом шагом 1024 (§5.2)

// CreateProfile сохраняет новый профиль. ID==0 → назначается хранилищем;
// sort_key — в конец группы.
func (s *Store) CreateProfile(p *model.Profile) error {
	return s.createProfile(withoutCustomHash(p), true) // T-08B: новый не подтверждён
}

// ImportProfile сохраняет профиль с СОХРАНЁННЫМ id (импортёр NekoBox, §5.3):
// id каталога NekoBox начинаются с 0 и служат ссылками (gid, chain-списки,
// front_proxy), переназначать их нельзя.
func (s *Store) ImportProfile(p *model.Profile) error {
	return s.createProfile(p, false)
}

func (s *Store) createProfile(p *model.Profile, autoID bool) error {
	if p.Detail != nil && p.Type == "" {
		return fmt.Errorf("store: profile type is required")
	}
	return s.withTx(func(tx *sql.Tx) error {
		now := nowMs()
		if autoID && p.ID == 0 {
			id, err := nextID(tx, "profiles")
			if err != nil {
				return err
			}
			p.ID = id
		}
		var exists int
		if err := tx.QueryRow(`SELECT COUNT(*) FROM groups WHERE id = ?`, p.GroupID).Scan(&exists); err != nil {
			return err
		}
		if exists == 0 {
			return fmt.Errorf("%w: group %d does not exist", ErrConstraint, p.GroupID)
		}
		sortKey, err := nextSortKey(tx, p.GroupID)
		if err != nil {
			return err
		}
		if p.CreatedAtMs == 0 {
			p.CreatedAtMs = now
		}
		p.UpdatedAtMs = now
		// Канонический JSON пишем с фактическими id/временами.
		data, err := profileData(p)
		if err != nil {
			return err
		}
		if _, err := tx.Exec(`INSERT INTO profiles (
			id, group_id, type, name, address, port, sort_key, favorite, notes,
			latency_ms, full_report, tested_at_ms, uplink_total, downlink_total,
			data, custom_hash, legacy_bean, created_at_ms, updated_at_ms
		) VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
			p.ID, p.GroupID, string(p.Type), p.Name, p.Address, p.Port, sortKey, boolInt(p.Favorite), nullableString(p.Notes),
			nullableLatency(&p.TestResult), nullableString(p.TestResult.FullReport), nullableInt64(p.TestResult.TestedAtMs),
			p.Traffic.UplinkBytes, p.Traffic.DownlinkBytes,
			string(data), nullableString(p.CustomHash), nullableString(p.LegacyBean), p.CreatedAtMs, p.UpdatedAtMs,
		); err != nil {
			return fmt.Errorf("store: insert profile: %w", err)
		}
		return nil
	})
}

// UpdateProfile полностью замещает профиль (кроме sort_key и трафика/
// результатов тестов — они читаются, не перезаписываются клиентом).
func (s *Store) UpdateProfile(p *model.Profile) error {
	if p.ID == 0 {
		return fmt.Errorf("store: profile id is required")
	}
	return s.withTx(func(tx *sql.Tx) error {
		oldGroup, err := loadGroupAndCustomHash(tx, p) // T-08B: хэш — только из БД
		if errors.Is(err, sql.ErrNoRows) {
			return ErrNotFound
		}
		if err != nil {
			return err
		}
		p.UpdatedAtMs = nowMs()
		data, err := profileData(p)
		if err != nil {
			return err
		}
		// При переносе в другую группу sort_key пересчитывается (в конец новой
		// группы). Два явных запроса вместо склейки «sort_key = ?»: плейсхолдеры
		// и аргументы должны совпадать позиционно, а склейка вставляла значение
		// sort_key в group_id → FOREIGN KEY constraint failed.
		if p.GroupID != oldGroup {
			sk, err := nextSortKey(tx, p.GroupID)
			if err != nil {
				return err
			}
			_, err = tx.Exec(`UPDATE profiles SET
				group_id = ?, type = ?, name = ?, address = ?, port = ?, sort_key = ?,
				favorite = ?, notes = ?, data = ?, custom_hash = ?, updated_at_ms = ?
			WHERE id = ?`,
				p.GroupID, string(p.Type), p.Name, p.Address, p.Port, sk,
				boolInt(p.Favorite), nullableString(p.Notes), string(data), nullableString(p.CustomHash),
				p.UpdatedAtMs, p.ID)
			if err != nil {
				return fmt.Errorf("store: update profile: %w", err)
			}
			return nil
		}
		_, err = tx.Exec(`UPDATE profiles SET
			group_id = ?, type = ?, name = ?, address = ?, port = ?, sort_key = sort_key,
			favorite = ?, notes = ?, data = ?, custom_hash = ?, updated_at_ms = ?
		WHERE id = ?`,
			p.GroupID, string(p.Type), p.Name, p.Address, p.Port,
			boolInt(p.Favorite), nullableString(p.Notes), string(data), nullableString(p.CustomHash),
			p.UpdatedAtMs, p.ID)
		if err != nil {
			return fmt.Errorf("store: update profile: %w", err)
		}
		return nil
	})
}

// SaveTestResult записывает результат теста — колонки latency_ms,
// tested_at_ms, full_report и ключ test_result в JSON (там живёт last_error) —
// отдельным UPDATE (T-22). Остальные поля профиля не читаются и не пишутся:
// устаревшая копия профиля у вызывающего не может затереть правки (избранное,
// заметки, имя), а updated_at_ms остаётся прежним — результат теста не правка
// профиля. Пустой результат очищает прежний (ClearTestResults).
func (s *Store) SaveTestResult(id int64, r model.TestResult) error {
	blob, err := json.Marshal(r)
	if err != nil {
		return fmt.Errorf("store: marshal test result: %w", err)
	}
	res, err := s.db.Exec(`UPDATE profiles SET
		latency_ms = ?, tested_at_ms = ?, full_report = ?,
		data = json_set(data, '$.test_result', json(?))
	WHERE id = ?`,
		nullableLatency(&r), nullableInt64(r.TestedAtMs), nullableString(r.FullReport), string(blob), id)
	if err != nil {
		return fmt.Errorf("store: save test result: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// SaveTraffic записывает счётчики трафика профиля — колонки uplink_total,
// downlink_total и ключ traffic в JSON — отдельным UPDATE (D-04): UpdateProfile
// счётчики не пишет, поэтому сброс через него оставлял прежние значения. Как и
// SaveTestResult, не читает и не пишет остальные поля и не меняет updated_at_ms.
func (s *Store) SaveTraffic(id int64, t model.TrafficCounters) error {
	blob, err := json.Marshal(t)
	if err != nil {
		return fmt.Errorf("store: marshal traffic: %w", err)
	}
	res, err := s.db.Exec(`UPDATE profiles SET
		uplink_total = ?, downlink_total = ?,
		data = json_set(data, '$.traffic', json(?))
	WHERE id = ?`,
		t.UplinkBytes, t.DownlinkBytes, string(blob), id)
	if err != nil {
		return fmt.Errorf("store: save traffic: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// profileCols — все декодируемые колонки профиля ОДНИМ запросом.
//
// ВАЖНО: пул соединений ограничен одним (SetMaxOpenConns(1), store.go), поэтому
// при открытых sql.Rows ЗАПРЕЩЕНЫ вложенные запросы к s.db — соединение занято
// итерацией, вложенный запрос вечно ждёт освобождения (deadlock; regression:
// листинги должны выбирать всё одним проходом, как здесь).
const profileCols = `favorite, notes, latency_ms, full_report, tested_at_ms,
	uplink_total, downlink_total, custom_hash, legacy_bean, created_at_ms, updated_at_ms, data`

// GetProfile возвращает профиль по id.
func (s *Store) GetProfile(id int64) (*model.Profile, error) {
	row := s.db.QueryRow(`SELECT `+profileCols+` FROM profiles WHERE id = ?`, id)
	p, err := scanProfile(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return p, nil
}

// ListProfiles — профили группы в каноническом порядке; groupID < 0 — все
// профили. 0 — ВАЛИДНЫЙ id группы (каталог NekoBox: первая группа имеет
// id 0), поэтому «все» выбирается отрицательным значением.
func (s *Store) ListProfiles(groupID int64) ([]*model.Profile, error) {
	q := `SELECT ` + profileCols + ` FROM profiles`
	var args []any
	if groupID >= 0 {
		q += ` WHERE group_id = ?`
		args = append(args, groupID)
	}
	q += ` ORDER BY group_id, sort_key, id`
	rows, err := s.db.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*model.Profile
	for rows.Next() {
		p, err := scanProfile(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// DeleteProfile удаляет профиль (запуск запущенного проверяет слой API, FR-UI-038).
func (s *Store) DeleteProfile(id int64) error {
	// T-51: через withTx, чтобы удаление работало и внутри InTx.
	return s.withTx(func(tx *sql.Tx) error {
		res, err := tx.Exec(`DELETE FROM profiles WHERE id = ?`, id)
		if err != nil {
			return err
		}
		if n, _ := res.RowsAffected(); n == 0 {
			return ErrNotFound
		}
		return nil
	})
}

// ReorderProfiles задаёт порядок профилей в группе (FR-UI-074): перечисленные
// id получают порядок перечисления; не перечисленные (удалённые из списка)
// self-correct — добавляются в конец в текущем порядке.
func (s *Store) ReorderProfiles(groupID int64, orderedIDs []int64) error {
	return s.withTx(func(tx *sql.Tx) error {
		// Только id, реально принадлежащие группе (самокоррекция от чужих).
		inGroup := map[int64]bool{}
		rows, err := tx.Query(`SELECT id FROM profiles WHERE group_id = ?`, groupID)
		if err != nil {
			return err
		}
		for rows.Next() {
			var id int64
			if err := rows.Scan(&id); err != nil {
				rows.Close()
				return err
			}
			inGroup[id] = true
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return err
		}

		next := int64(0)
		listed := make(map[int64]bool, len(orderedIDs))
		for _, id := range orderedIDs {
			if !inGroup[id] || listed[id] {
				continue
			}
			listed[id] = true
			next += sortKeyStep
			if _, err := tx.Exec(`UPDATE profiles SET sort_key = ? WHERE id = ?`, next, id); err != nil {
				return err
			}
		}
		// Неперечисленные — в конец по текущему порядку.
		restRows, err := tx.Query(`SELECT id FROM profiles WHERE group_id = ? ORDER BY sort_key, id`, groupID)
		if err != nil {
			return err
		}
		var rest []int64
		for restRows.Next() {
			var id int64
			if err := restRows.Scan(&id); err != nil {
				restRows.Close()
				return err
			}
			if !listed[id] {
				rest = append(rest, id)
			}
		}
		restRows.Close()
		if err := restRows.Err(); err != nil {
			return err
		}
		for _, id := range rest {
			next += sortKeyStep
			if _, err := tx.Exec(`UPDATE profiles SET sort_key = ? WHERE id = ?`, next, id); err != nil {
				return err
			}
		}
		return nil
	})
}

// ---- вспомогательное ----

func nextSortKey(tx *sql.Tx, groupID int64) (int64, error) {
	var maxKey sql.NullInt64
	if err := tx.QueryRow(`SELECT MAX(sort_key) FROM profiles WHERE group_id = ?`, groupID).Scan(&maxKey); err != nil {
		return 0, err
	}
	return nullInt64(maxKey) + sortKeyStep, nil
}

// nullableLatency: nil = не тестировали (NULL); <0 = unavailable; >0 = мс.
func nullableLatency(r *model.TestResult) any {
	if r == nil || r.LatencyMs == nil {
		return nil
	}
	return *r.LatencyMs
}

// scanProfile декодирует профиль из ОДНОЙ строки выборки: канонический JSON
// (profiles.data) + синхронизация поисковых полей из колонок (источник
// истины для них — SQL). Замена старого decodeProfile с вложенным запросом
// (причина deadlock'а пакета: см. комментарий у profileCols).
func scanProfile(row rowScanner) (*model.Profile, error) {
	var (
		p          model.Profile
		favorite   int
		notes      sql.NullString
		latency    sql.NullInt64
		fullReport sql.NullString
		testedAt   sql.NullInt64
		uplink     int64
		downlink   int64
		customHash sql.NullString
		legacyBean sql.NullString
		createdAt  int64
		updatedAt  int64
		data       string
	)
	if err := row.Scan(&favorite, &notes, &latency, &fullReport, &testedAt,
		&uplink, &downlink, &customHash, &legacyBean, &createdAt, &updatedAt, &data); err != nil {
		return nil, err
	}
	if err := json.Unmarshal([]byte(data), &p); err != nil {
		return nil, fmt.Errorf("store: decode profile: %w", err)
	}
	p.Favorite = favorite != 0
	p.Notes = notes.String
	if latency.Valid {
		v := int32(latency.Int64)
		p.TestResult.LatencyMs = &v
	} else {
		p.TestResult.LatencyMs = nil
	}
	p.TestResult.FullReport = fullReport.String
	if testedAt.Valid {
		v := testedAt.Int64
		p.TestResult.TestedAtMs = &v
	} else {
		p.TestResult.TestedAtMs = nil
	}
	p.Traffic = model.TrafficCounters{UplinkBytes: uplink, DownlinkBytes: downlink}
	applyScannedCustomHash(&p, customHash)
	p.LegacyBean = legacyBean.String
	p.CreatedAtMs = createdAt
	p.UpdatedAtMs = updatedAt
	return &p, nil
}

// SanitizeAddress вырезает пробелы из адреса сервера (FR-UI-083).
func SanitizeAddress(addr string) string { return strings.TrimSpace(addr) }
