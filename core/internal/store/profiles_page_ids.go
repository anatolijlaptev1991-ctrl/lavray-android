// SPDX-License-Identifier: GPL-3.0-only

package store

// profiles_page_ids.go — чтение полных профилей страницы по списку id (R0-D).

import (
	"database/sql"
	"strings"

	"github.com/anatolijlaptev1991-ctrl/lavray-android/core/internal/model"
)

const pageIDChunk = 500

// profilesByIDs читает полные профили страницы, сохраняя порядок ids.
func (s *Store) profilesByIDs(db queryer, ids []int64) ([]*model.Profile, error) {
	byID := make(map[int64]*model.Profile, len(ids))
	for start := 0; start < len(ids); start += pageIDChunk {
		chunk := ids[start:min(len(ids), start+pageIDChunk)]
		args := make([]any, len(chunk))
		for i, id := range chunk {
			args[i] = id
		}
		rows, err := db.Query(`SELECT `+profileCols+` FROM profiles WHERE id IN (?`+strings.Repeat(`,?`, len(chunk)-1)+`)`, args...)
		if err != nil {
			return nil, err
		}
		ps, err := scanProfiles(rows)
		rows.Close()
		if err != nil {
			return nil, err
		}
		for _, p := range ps {
			byID[p.ID] = p
		}
	}
	out := make([]*model.Profile, 0, len(ids))
	for _, id := range ids {
		if p := byID[id]; p != nil {
			out = append(out, p)
		} else {
			s.warnf("store: профиль %d отобран, но не прочитан (id в data не совпал со строкой?)", id)
		}
	}
	return out, nil
}

func scanProfiles(rows *sql.Rows) ([]*model.Profile, error) {
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
