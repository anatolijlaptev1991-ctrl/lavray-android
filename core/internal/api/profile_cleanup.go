// SPDX-License-Identifier: GPL-3.0-only

package api

// profile_cleanup.go — RemoveUnavailableProfiles (FR-UI-042, Ctrl+Alt+R):
// удаление профилей группы, проверенных и не отвечающих.

import (
	"context"

	"connectrpc.com/connect"

	lavrayv1 "github.com/anatolijlaptev1991-ctrl/lavray-android/core/internal/gen/lavrayv1"
	"github.com/anatolijlaptev1991-ctrl/lavray-android/core/internal/logs"
	"github.com/anatolijlaptev1991-ctrl/lavray-android/core/internal/model"
)

// isUnavailable: тест был и профиль не ответил — latency_ms < 0 или
// неудачный тест (время есть, задержки нет, есть ошибка; test_instance.go).
// Непроверенный и прерванный тест (без времени) недоступным не считаются.
func isUnavailable(p *model.Profile) bool {
	tr := p.TestResult
	if tr.LatencyMs != nil {
		return *tr.LatencyMs < 0
	}
	return tr.TestedAtMs != nil && tr.LastError != ""
}

// RemoveUnavailableProfiles удаляет недоступные профили группы. Активный
// профиль и профили, на которые ссылается цепочка или front proxy группы, не
// трогаются и возвращаются в skipped_ids. apply=false — пробный прогон: ответ
// тот же, ничего не пишется. Несуществующая группа — NOT_FOUND.
func (s *Server) RemoveUnavailableProfiles(ctx context.Context, req *connect.Request[lavrayv1.RemoveUnavailableProfilesRequest]) (*connect.Response[lavrayv1.RemoveUnavailableResponse], error) {
	groupID := req.Msg.GroupId
	if _, err := s.st.GetGroup(groupID); err != nil {
		return nil, mapErr(err)
	}
	list, err := s.st.ListProfiles(groupID)
	if err != nil {
		return nil, mapErr(err)
	}
	refs, err := s.referencedProfileIDs()
	if err != nil {
		return nil, mapErr(err)
	}
	active := s.activeProfileID()
	resp := &lavrayv1.RemoveUnavailableResponse{}
	for _, p := range list {
		switch {
		case !isUnavailable(p):
		case p.ID == active || refs[p.ID]:
			resp.SkippedIds = append(resp.SkippedIds, p.ID)
		default:
			resp.WouldRemoveIds = append(resp.WouldRemoveIds, p.ID)
		}
	}
	if req.Msg.Apply && len(resp.WouldRemoveIds) > 0 {
		if err := s.deleteProfiles(resp.WouldRemoveIds); err != nil {
			return nil, mapErr(err)
		}
		resp.RemovedIds = resp.WouldRemoveIds
		s.logs.Logf(logs.LevelInfo, logs.SourceCore, "очистка недоступных: группа %d, удалено %d, пропущено %d",
			groupID, len(resp.RemovedIds), len(resp.SkippedIds))
	}
	return connect.NewResponse(resp), nil
}
