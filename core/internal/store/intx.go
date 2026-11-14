// SPDX-License-Identifier: GPL-3.0-only

package store

import "fmt"

// InTx выполняет fn над транзакционным представлением хранилища (T-51,
// A1-16): все мутации внутри fn — CreateProfile, ImportProfile, UpdateProfile,
// DeleteProfile, ReorderProfiles, UpdateGroup, CreateGroup, SetMeta, SetSetting — идут в ОДНОЙ
// транзакции SQLite. fn вернула ошибку или запаниковала — откатывается всё;
// процесс убит до COMMIT — после перезапуска ничего из серии не видно.
//
// Ограничения представления tx:
//   - читать через него нельзя (GetProfile, ListProfiles, GetGroup, GetMeta,
//     GetSetting, DB): единственное соединение пула занято транзакцией, поэтому
//     обращение к s.db либо зависло бы навсегда, либо, как здесь, падает
//     паникой сразу (db == nil). Нужное читают ДО InTx;
//   - tx нельзя сохранять и использовать после возврата из fn.
//
// Вложенный InTx на представлении выполняет fn в той же транзакции.
func (s *Store) InTx(fn func(tx *Store) error) (err error) {
	if s.tx != nil {
		return fn(s)
	}
	sqlTx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("store: begin: %w", err)
	}
	committed := false
	defer func() {
		if !committed { // ошибка fn или паника
			_ = sqlTx.Rollback()
		}
	}()
	if err := fn(&Store{dir: s.dir, tx: sqlTx}); err != nil {
		return err
	}
	if err := sqlTx.Commit(); err != nil {
		return fmt.Errorf("store: commit: %w", err)
	}
	committed = true
	return nil
}
