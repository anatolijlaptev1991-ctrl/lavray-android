// SPDX-License-Identifier: GPL-3.0-only

package store

import (
	"database/sql"
	"fmt"
)

// SetGroupFrontProxy задаёт (nil — снимает) front_proxy_id группы одним UPDATE.
// В отличие от UpdateGroup принимает группу с id 0: id каталога NekoBox
// начинаются с 0, и перенос ставит front_proxy уже после вставки профилей
// (FK на profiles). Идёт через withTx, поэтому входит в транзакцию InTx.
// Ошибки: ErrNotFound — нет группы; ErrConstraint — нет такого профиля.
func (s *Store) SetGroupFrontProxy(groupID int64, profileID *int64) error {
	return s.withTx(func(tx *sql.Tx) error {
		res, err := tx.Exec(`UPDATE groups SET front_proxy_id = ?, updated_at_ms = ? WHERE id = ?`,
			nullableInt64(profileID), nowMs(), groupID)
		if err != nil {
			return fmt.Errorf("%w: front_proxy_id группы %d: %v", ErrConstraint, groupID, err)
		}
		if n, _ := res.RowsAffected(); n == 0 {
			return ErrNotFound
		}
		return nil
	})
}
