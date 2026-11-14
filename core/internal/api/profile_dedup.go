// SPDX-License-Identifier: GPL-3.0-only

package api

// profile_dedup.go — DeduplicateProfiles (FR-UI-042, Ctrl+Alt+D): удаление
// копий серверов в группе. Что считается копией — model.DedupKey, как решает
// план — profile_dedup_plan.go.

import (
	"context"

	"connectrpc.com/connect"

	lavrayv1 "github.com/anatolijlaptev1991-ctrl/lavray-android/core/internal/gen/lavrayv1"
	"github.com/anatolijlaptev1991-ctrl/lavray-android/core/internal/logs"
	"github.com/anatolijlaptev1991-ctrl/lavray-android/core/internal/store"
)

// DeduplicateProfiles удаляет копии серверов группы: остаётся первый по
// порядку (активный профиль не удаляется никогда), ссылки цепочек и front
// proxy групп переписываются на оставшийся. apply=false — пробный прогон:
// duplicates те же, removed_ids пуст, ничего не пишется. Применение — одна
// транзакция. Несуществующая группа — NOT_FOUND.
func (s *Server) DeduplicateProfiles(ctx context.Context, req *connect.Request[lavrayv1.DeduplicateProfilesRequest]) (*connect.Response[lavrayv1.DeduplicateResponse], error) {
	groupID := req.Msg.GroupId
	if _, err := s.st.GetGroup(groupID); err != nil {
		return nil, mapErr(err)
	}
	scope, err := s.st.ListProfiles(groupID)
	if err != nil {
		return nil, mapErr(err)
	}
	all, err := s.st.ListProfiles(-1)
	if err != nil {
		return nil, mapErr(err)
	}
	groups, err := s.st.ListGroups()
	if err != nil {
		return nil, mapErr(err)
	}
	plan := planDedup(scope, all, groups, s.activeProfileID())
	resp := &lavrayv1.DeduplicateResponse{}
	for _, p := range plan.removed {
		resp.Duplicates = append(resp.Duplicates, profileToProto(p))
	}
	if !req.Msg.Apply || len(plan.removed) == 0 {
		return connect.NewResponse(resp), nil
	}
	if err := s.applyDedup(plan); err != nil {
		return nil, mapErr(err)
	}
	for _, p := range plan.removed {
		resp.RemovedIds = append(resp.RemovedIds, p.ID)
	}
	s.logs.Logf(logs.LevelInfo, logs.SourceCore, "удаление дубликатов: группа %d, удалено %d", groupID, len(resp.RemovedIds))
	return connect.NewResponse(resp), nil
}

// applyDedup пишет план одной транзакцией: сначала новые ссылки, потом удаление.
func (s *Server) applyDedup(plan dedupPlan) error {
	return s.st.InTx(func(tx *store.Store) error {
		for _, g := range plan.groups {
			if err := tx.UpdateGroup(g); err != nil {
				return err
			}
		}
		for _, p := range plan.chains {
			if err := tx.UpdateProfile(p); err != nil {
				return err
			}
		}
		for _, p := range plan.removed {
			if err := tx.DeleteProfile(p.ID); err != nil {
				return err
			}
		}
		return nil
	})
}
