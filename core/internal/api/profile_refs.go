// SPDX-License-Identifier: GPL-3.0-only

package api

// profile_refs.go — кто на кого ссылается и что сейчас работает: общие
// проверки очистки недоступных и дубликатов (хвосты A).

import (
	"github.com/anatolijlaptev1991-ctrl/lavray-android/core/internal/model"
	"github.com/anatolijlaptev1991-ctrl/lavray-android/core/internal/store"
)

// referencedIDs — id профилей, на которые кто-то ссылается: звенья цепочек
// (любой группы) и front proxy групп.
func referencedIDs(all []*model.Profile, groups []*model.Group) map[int64]bool {
	refs := map[int64]bool{}
	for _, p := range all {
		if d, ok := p.Detail.(*model.ChainDetail); ok && p.Type == model.TypeChain {
			for _, id := range d.ProfileIDs {
				refs[id] = true
			}
		}
	}
	for _, g := range groups {
		if g.FrontProxyID != nil {
			refs[*g.FrontProxyID] = true
		}
	}
	return refs
}

// referencedProfileIDs читает профили и группы и строит referencedIDs.
func (s *Server) referencedProfileIDs() (map[int64]bool, error) {
	all, err := s.st.ListProfiles(-1)
	if err != nil {
		return nil, err
	}
	groups, err := s.st.ListGroups()
	if err != nil {
		return nil, err
	}
	return referencedIDs(all, groups), nil
}

// activeProfileID — id профиля работающего соединения (0 — нет соединения
// или оно запущено из inline-профиля).
func (s *Server) activeProfileID() int64 {
	if run := s.inst.Status().Run; run != nil {
		return run.ProfileID
	}
	return 0
}

// deleteProfiles удаляет профили одной транзакцией: либо все, либо ни один.
func (s *Server) deleteProfiles(ids []int64) error {
	return s.st.InTx(func(tx *store.Store) error {
		for _, id := range ids {
			if err := tx.DeleteProfile(id); err != nil {
				return err
			}
		}
		return nil
	})
}
