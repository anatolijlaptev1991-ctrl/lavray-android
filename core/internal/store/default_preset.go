// SPDX-License-Identifier: GPL-3.0-only

package store

import (
	"database/sql"
	"fmt"
)

// defaultPresetSettledKey — отметка в meta: для этой базы вопрос о наборе по
// умолчанию уже решён (0.9.3).
const defaultPresetSettledKey = "routing.default_preset_settled"

// settleDefaultPreset один раз на базу защищает от молчаливой смены маршрутов.
//
// С 0.9.3 набор по умолчанию у новой установки — lav-ru, а строка настройки
// появляется только после явного выбора. База с серверами, где выбора не было,
// работала на global и без этой записи перешла бы на lav-ru, поэтому ей global
// записывается явно. В свежей базе серверов ещё нет: строка не создаётся,
// действует значение по умолчанию, а отметка не даёт закрепить global позже,
// когда серверы появятся.
//
// Намеренно не миграция схемы: с новым номером схемы 0.9.2 отказалась бы
// открывать базу (SchemaTooNewError), а пока обновления не настроены, установщик
// прошлой версии — единственный способ откатиться.
//
// Принятая граница: если новую установку без серверов откатили на 0.9.2, там
// добавили серверы и снова обновили, отметка уже стоит, и база получит lav-ru.
func (s *Store) settleDefaultPreset() error {
	return s.withTx(func(tx *sql.Tx) error {
		res, err := tx.Exec(`INSERT OR IGNORE INTO meta (key, value) VALUES (?, '1')`, defaultPresetSettledKey)
		if err != nil {
			return fmt.Errorf("default preset mark: %w", err)
		}
		marked, err := res.RowsAffected()
		if err != nil {
			return fmt.Errorf("default preset mark: %w", err)
		}
		if marked == 0 {
			return nil
		}
		_, err = tx.Exec(`INSERT OR IGNORE INTO settings (key, value, updated_at_ms)
			SELECT 'routing.active_preset', '"global"', ?
			 WHERE EXISTS (SELECT 1 FROM profiles)`, nowMs())
		if err != nil {
			return fmt.Errorf("default preset pin: %w", err)
		}
		return nil
	})
}
