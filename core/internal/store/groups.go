// SPDX-License-Identifier: GPL-3.0-only

package store

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/anatolijlaptev1991-ctrl/lavray-android/core/internal/model"
)

// ---- Группы (FR-UI-102–113) ----

// CreateGroup создаёт группу. Если g.ID == 0 — id и tab_order назначаются
// хранилищем (используется UI).
func (s *Store) CreateGroup(g *model.Group) error {
	return s.createGroup(g, true)
}

// ImportGroup создаёт группу с СОХРАНЁННЫМ id (импортёр NekoBox, §5.3):
// id каталога NekoBox начинаются с 0 и служат ссылками (front_proxy,
// chain-списки, gid профилей), поэтому переназначать их нельзя.
// TabOrder при 0 назначается как обычно.
func (s *Store) ImportGroup(g *model.Group) error {
	return s.createGroup(g, false)
}

func (s *Store) createGroup(g *model.Group, autoID bool) error {
	if g.Name == "" {
		return fmt.Errorf("store: group name is required")
	}
	if g.Kind != model.GroupSubscription {
		g.Kind = model.GroupBasic // KIND_UNSPECIFIED → basic
	}
	return s.withTx(func(tx *sql.Tx) error {
		now := nowMs()
		if autoID && g.ID == 0 {
			id, err := nextID(tx, "groups")
			if err != nil {
				return err
			}
			g.ID = id
		}
		if g.TabOrder == 0 {
			var maxOrder sql.NullInt64
			if err := tx.QueryRow(`SELECT MAX(tab_order) FROM groups`).Scan(&maxOrder); err != nil {
				return err
			}
			g.TabOrder = nullInt64(maxOrder) + 1
		}
		if g.CreatedAtMs == 0 {
			g.CreatedAtMs = now
		}
		g.UpdatedAtMs = now

		headers, err := marshalJSONString(g.HTTPHeaders)
		if err != nil {
			return err
		}
		var subInfo any
		if g.SubscriptionInfo != nil {
			b, err := json.Marshal(g.SubscriptionInfo)
			if err != nil {
				return err
			}
			subInfo = string(b)
		}
		_, err = tx.Exec(`INSERT INTO groups (
			id, kind, name, subscription_url, archive, skip_auto_update,
			front_proxy_id, subscription_info, update_interval_min, user_agent,
			http_headers, http_basic_user, last_working_url, last_update_ms,
			tab_order, ui_state, created_at_ms, updated_at_ms
		) VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
			g.ID, int(g.Kind), g.Name, nullableString(g.SubscriptionURL), boolInt(g.Archive), boolInt(g.SkipAutoUpdate),
			nullableInt64(g.FrontProxyID), subInfo, nullableInt32(g.UpdateIntervalMin), nullableString(g.UserAgent),
			headers, nullableString(g.HTTPBasicUser), nullableString(g.LastWorkingURL), nullableMs(g.LastUpdateMs),
			g.TabOrder, nullableString(g.UIState), g.CreatedAtMs, g.UpdatedAtMs)
		if err != nil {
			return fmt.Errorf("store: insert group: %w", err)
		}
		g.ProfileOrder = nil
		return nil
	})
}

// UpdateGroup обновляет изменяемые поля (kind после создания не меняется,
// FR-UI-109; id/create-время тоже).
func (s *Store) UpdateGroup(g *model.Group) error {
	if g.ID == 0 {
		return fmt.Errorf("store: group id is required")
	}
	return s.withTx(func(tx *sql.Tx) error {
		res, err := tx.Exec(`UPDATE groups SET
			name = ?, subscription_url = ?, archive = ?, skip_auto_update = ?,
			front_proxy_id = ?, subscription_info = ?, update_interval_min = ?, user_agent = ?,
			http_headers = ?, http_basic_user = ?, last_working_url = ?, last_update_ms = ?,
			ui_state = ?, updated_at_ms = ?
		WHERE id = ?`,
			g.Name, nullableString(g.SubscriptionURL), boolInt(g.Archive), boolInt(g.SkipAutoUpdate),
			nullableInt64(g.FrontProxyID), nullableSubInfo(g.SubscriptionInfo), nullableInt32(g.UpdateIntervalMin), nullableString(g.UserAgent),
			mustJSON(g.HTTPHeaders), nullableString(g.HTTPBasicUser), nullableString(g.LastWorkingURL), nullableMs(g.LastUpdateMs),
			nullableString(g.UIState), nowMs(), g.ID)
		if err != nil {
			return err
		}
		if n, _ := res.RowsAffected(); n == 0 {
			return ErrNotFound
		}
		return nil
	})
}

// GetGroup возвращает группу с вычисленным ProfileOrder (порядок — sort_key).
func (s *Store) GetGroup(id int64) (*model.Group, error) {
	row := s.db.QueryRow(`SELECT
		id, kind, name, subscription_url, archive, skip_auto_update,
		front_proxy_id, subscription_info, update_interval_min, user_agent,
		http_headers, http_basic_user, last_working_url, last_update_ms,
		tab_order, ui_state, created_at_ms, updated_at_ms
	FROM groups WHERE id = ?`, id)
	g, err := scanGroup(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	if err := s.loadProfileOrder(g); err != nil {
		return nil, err
	}
	return g, nil
}

// ListGroups — все группы в порядке вкладок (FR-UI-113).
func (s *Store) ListGroups() ([]*model.Group, error) {
	rows, err := s.db.Query(`SELECT
		id, kind, name, subscription_url, archive, skip_auto_update,
		front_proxy_id, subscription_info, update_interval_min, user_agent,
		http_headers, http_basic_user, last_working_url, last_update_ms,
		tab_order, ui_state, created_at_ms, updated_at_ms
	FROM groups ORDER BY tab_order, id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*model.Group
	for rows.Next() {
		g, err := scanGroup(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, g)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	for _, g := range out {
		if err := s.loadProfileOrder(g); err != nil {
			return nil, err
		}
	}
	return out, nil
}

// DeleteGroup удаляет группу вместе с профилями (каскад). Последнюю группу
// удалить нельзя (FR-UI-107).
func (s *Store) DeleteGroup(id int64) error {
	return s.withTx(func(tx *sql.Tx) error {
		var count int
		if err := tx.QueryRow(`SELECT COUNT(*) FROM groups`).Scan(&count); err != nil {
			return err
		}
		if count <= 1 {
			return ErrLastGroup
		}
		res, err := tx.Exec(`DELETE FROM groups WHERE id = ?`, id)
		if err != nil {
			return err
		}
		if n, _ := res.RowsAffected(); n == 0 {
			return ErrNotFound
		}
		return nil
	})
}

// ReorderGroups задаёт порядок вкладок (FR-UI-113).
func (s *Store) ReorderGroups(orderedIDs []int64) error {
	return s.withTx(func(tx *sql.Tx) error {
		for i, id := range orderedIDs {
			if _, err := tx.Exec(`UPDATE groups SET tab_order = ? WHERE id = ?`, i+1, id); err != nil {
				return err
			}
		}
		return nil
	})
}

// ---- вспомогательное ----

type rowScanner interface{ Scan(dest ...any) error }

func scanGroup(row rowScanner) (*model.Group, error) {
	var (
		g              model.Group
		kind           int
		archive        int
		skipAuto       int
		subURL         sql.NullString
		frontProxyID   sql.NullInt64
		subInfo        sql.NullString
		updateInterval sql.NullInt32
		userAgent      sql.NullString
		httpHeaders    sql.NullString
		basicUser      sql.NullString
		lastWorking    sql.NullString
		lastUpdate     sql.NullInt64
		uiState        sql.NullString
	)
	err := row.Scan(&g.ID, &kind, &g.Name, &subURL, &archive, &skipAuto,
		&frontProxyID, &subInfo, &updateInterval, &userAgent,
		&httpHeaders, &basicUser, &lastWorking, &lastUpdate,
		&g.TabOrder, &uiState, &g.CreatedAtMs, &g.UpdatedAtMs)
	if err != nil {
		return nil, err
	}
	g.Kind = model.GroupKind(kind)
	g.Archive = archive != 0
	g.SkipAutoUpdate = skipAuto != 0
	g.SubscriptionURL = subURL.String
	if frontProxyID.Valid {
		v := frontProxyID.Int64
		g.FrontProxyID = &v
	}
	if subInfo.Valid && subInfo.String != "" {
		var info model.SubscriptionInfo
		if err := json.Unmarshal([]byte(subInfo.String), &info); err == nil {
			g.SubscriptionInfo = &info
		}
	}
	if updateInterval.Valid {
		v := updateInterval.Int32
		g.UpdateIntervalMin = &v
	}
	g.UserAgent = userAgent.String
	if httpHeaders.Valid && httpHeaders.String != "" {
		_ = json.Unmarshal([]byte(httpHeaders.String), &g.HTTPHeaders)
	}
	g.HTTPBasicUser = basicUser.String
	g.LastWorkingURL = lastWorking.String
	g.LastUpdateMs = nullInt64(lastUpdate)
	g.UIState = uiState.String
	return &g, nil
}

func (s *Store) loadProfileOrder(g *model.Group) error {
	rows, err := s.db.Query(`SELECT id FROM profiles WHERE group_id = ? ORDER BY sort_key, id`, g.ID)
	if err != nil {
		return err
	}
	defer rows.Close()
	g.ProfileOrder = nil
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return err
		}
		g.ProfileOrder = append(g.ProfileOrder, id)
	}
	return rows.Err()
}

// nextID — следующий id для таблицы. L18 аудита №1: MAX(id)+1 переиспользует
// id только что удалённой записи, из-за чего ChainDetail.ProfileIDs,
// front_proxy_id и снапшоты начинали указывать на ДРУГОЙ профиль. Поэтому
// высоководная отметка хранится в meta и только растёт (id удалённых записей
// не возвращаются в оборот).
func nextID(tx *sql.Tx, table string) (int64, error) {
	key := "store.last_id." + table
	var maxID sql.NullInt64
	if err := tx.QueryRow(`SELECT MAX(id) FROM ` + table).Scan(&maxID); err != nil {
		return 0, err
	}
	next := nullInt64(maxID) + 1

	var savedRaw string
	err := tx.QueryRow(`SELECT value FROM meta WHERE key = ?`, key).Scan(&savedRaw)
	switch {
	case err == nil:
		var saved int64
		if _, perr := fmt.Sscanf(savedRaw, "%d", &saved); perr == nil && saved >= next {
			next = saved + 1
		}
	case errors.Is(err, sql.ErrNoRows):
		// отметки ещё нет
	default:
		return 0, err
	}
	if _, err := tx.Exec(`INSERT INTO meta (key, value) VALUES (?, ?)
		ON CONFLICT(key) DO UPDATE SET value = excluded.value`, key, fmt.Sprintf("%d", next)); err != nil {
		return 0, err
	}
	return next, nil
}

func boolInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

func nullInt64(v sql.NullInt64) int64 {
	if v.Valid {
		return v.Int64
	}
	return 0
}

func nullableString(s string) any {
	if s == "" {
		return nil
	}
	return s
}

func nullableInt64(p *int64) any {
	if p == nil {
		return nil
	}
	return *p
}

func nullableInt32(p *int32) any {
	if p == nil {
		return nil
	}
	return *p
}

// nullableMs хранит 0 как NULL («никогда»).
func nullableMs(ms int64) any {
	if ms == 0 {
		return nil
	}
	return ms
}

func nullableSubInfo(info *model.SubscriptionInfo) any {
	if info == nil {
		return nil
	}
	return mustJSON(info)
}

func marshalJSONString(m map[string]string) (any, error) {
	if len(m) == 0 {
		return nil, nil
	}
	b, err := json.Marshal(m)
	if err != nil {
		return nil, err
	}
	return string(b), nil
}

func mustJSON(v any) string {
	if v == nil {
		return ""
	}
	b, err := json.Marshal(v)
	if err != nil {
		return ""
	}
	return string(b)
}
