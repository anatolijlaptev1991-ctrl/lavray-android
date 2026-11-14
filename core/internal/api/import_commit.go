// SPDX-License-Identifier: GPL-3.0-only

package api

// Сохранение импортированных профилей и «ручные» серверы подписки (T-51,
// A1-16, B1-27, B1-35). «Тот же сервер» определяет model.DedupKey — тот же
// отпечаток, что у очистки дубликатов.

import (
	"github.com/anatolijlaptev1991-ctrl/lavray-android/core/internal/model"
	"github.com/anatolijlaptev1991-ctrl/lavray-android/core/internal/store"
)

// isSubscriptionGroup — группа создана как подписка. Профили, которые
// пользователь кладёт в такую группу сам, помечаются model.Profile.Manual:
// обновление подписки их не удаляет.
func (s *Server) isSubscriptionGroup(id int64) bool {
	g, err := s.st.GetGroup(id)
	return err == nil && g.Kind == model.GroupSubscription
}

// commitImport сохраняет разобранные профили в группу ОДНОЙ транзакцией и
// пропускает уже имеющиеся (в группе или раньше в этом же списке). Раньше
// каждый профиль создавался отдельной транзакцией: повторный импорт той же
// ссылки давал дубль, а сбой посреди списка оставлял половину.
// Возвращает id созданных профилей и число пропущенных дублей.
func (s *Server) commitImport(groupID int64, list []*model.Profile) (added []int64, skipped int, err error) {
	existing, err := s.st.ListProfiles(groupID)
	if err != nil {
		return nil, 0, err
	}
	seen := make(map[string]bool, len(existing)+len(list))
	for _, p := range existing {
		if k := model.DedupKey(p); k != "" {
			seen[k] = true
		}
	}
	manual := s.isSubscriptionGroup(groupID)
	var fresh []*model.Profile
	for _, p := range list {
		if k := model.DedupKey(p); k != "" {
			if seen[k] {
				skipped++
				continue
			}
			seen[k] = true
		}
		p.ID = 0
		p.GroupID = groupID
		p.Manual = manual
		fresh = append(fresh, p)
	}
	err = s.st.InTx(func(tx *store.Store) error {
		for _, p := range fresh {
			if err := tx.CreateProfile(p); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return nil, 0, err
	}
	for _, p := range fresh {
		added = append(added, p.ID)
	}
	return added, skipped, nil
}
