// SPDX-License-Identifier: GPL-3.0-only

package subs

// Атомарность Apply и ручные серверы (T-51, A1-16, B1-27, B1-35).

import (
	"fmt"

	"github.com/anatolijlaptev1991-ctrl/lavray-android/core/internal/model"
	"github.com/anatolijlaptev1991-ctrl/lavray-android/core/internal/store"
)

// txRunner — хранилище, умеющее выполнить функцию в одной транзакции
// (*store.Store.InTx).
type txRunner interface {
	InTx(fn func(tx *store.Store) error) error
}

// applyAtomically выполняет записи Apply.
//
// Если st умеет транзакции (*store.Store), все записи — создание, обновление,
// удаление, порядок, поля группы — идут в ОДНОЙ транзакции: ошибка откатывает
// всё штатным ROLLBACK, а убитый посреди обновления процесс оставляет прежнюю
// группу (раньше каждая запись была своей транзакцией, и группа оставалась
// смешанной «старые + новые»). Иначе (тестовые обёртки StoreAPI) — прежний
// путь: записи по одной и ручной откат из снимка.
func applyAtomically(st StoreAPI, snap *Snapshot, params ApplyParams, next []*model.Profile, diff *Diff) (*UpdateResult, error) {
	if tr, ok := st.(txRunner); ok {
		var res *UpdateResult
		err := tr.InTx(func(tx *store.Store) error {
			var err error
			res, err = applyWrites(tx, snap, params, next, diff)
			return err
		})
		if err != nil {
			return nil, err
		}
		return res, nil
	}
	res, err := applyWrites(st, snap, params, next, diff)
	if err != nil {
		// Откат при ошибке (FR-UI-258): группа возвращается к снапшоту.
		if rbErr := Rollback(st, snap); rbErr != nil {
			return nil, fmt.Errorf("%w; откат тоже не удался: %v", err, rbErr)
		}
		return nil, err
	}
	return res, nil
}

// managedOnly — профили, которыми управляет подписка. Профили с флагом Manual
// (добавлены пользователем вручную во вкладку подписки) обновление не
// сопоставляет с ответом, не удаляет и не учитывает в пороге удаления.
func managedOnly(list []*model.Profile) []*model.Profile {
	out := make([]*model.Profile, 0, len(list))
	for _, p := range list {
		if !p.Manual {
			out = append(out, p)
		}
	}
	return out
}
