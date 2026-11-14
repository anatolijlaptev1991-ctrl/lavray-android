// SPDX-License-Identifier: GPL-3.0-only

package nekoimport

import (
	"strconv"

	"github.com/anatolijlaptev1991-ctrl/lavray-android/core/internal/store"
)

// state.go — State: всё, что перенос должен знать о базе ДО записи. Читается
// заранее и целиком, потому что внутри InTx читать нельзя (store/intx.go).

// State — снимок базы для Remap.
type State struct {
	GroupIDs     map[int64]bool // id существующих групп
	ProfileIDs   map[int64]bool // id существующих профилей
	GroupFloor   int64          // наибольший выданный id групп
	ProfileFloor int64          // наибольший выданный id профилей
	rows         map[string]map[string]string
}

// LoadState читает из базы id групп и профилей, отметки выданных id и import_map.
func LoadState(s *store.Store) (State, error) {
	var st State
	var err error
	if st.GroupIDs, err = s.EntityIDs("groups"); err != nil {
		return State{}, err
	}
	if st.ProfileIDs, err = s.EntityIDs("profiles"); err != nil {
		return State{}, err
	}
	if st.GroupFloor, err = s.IDFloor("groups"); err != nil {
		return State{}, err
	}
	if st.ProfileFloor, err = s.IDFloor("profiles"); err != nil {
		return State{}, err
	}
	rows, err := s.ImportMapRows(SourceNekoBox)
	if err != nil {
		return State{}, err
	}
	st.rows = map[string]map[string]string{}
	for _, r := range rows {
		if st.rows[r.Kind] == nil {
			st.rows[r.Kind] = map[string]string{}
		}
		st.rows[r.Kind][r.SourceKey] = r.LocalID
	}
	return st, nil
}

// Rows — ключ идемпотентности → локальный id для вида записей (kind).
func (st State) Rows(kind string) map[string]string { return st.rows[kind] }

// localNumeric — локальный числовой id по ключу; false — записи нет.
func (st State) localNumeric(kind, key string) (int64, bool) {
	raw, ok := st.rows[kind][key]
	if !ok {
		return 0, false
	}
	id, err := strconv.ParseInt(raw, 10, 64)
	return id, err == nil
}
