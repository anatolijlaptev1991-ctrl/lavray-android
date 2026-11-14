// SPDX-License-Identifier: GPL-3.0-only

package api

// traffic_summary.go — StatsService.GetTrafficSummary (T-79, UX-25): сводка
// трафика по группам и подпискам за период из таблицы sessions.

import (
	"context"

	"connectrpc.com/connect"

	lavrayv1 "github.com/anatolijlaptev1991-ctrl/lavray-android/core/internal/gen/lavrayv1"
	"github.com/anatolijlaptev1991-ctrl/lavray-android/core/internal/store"
)

// GetTrafficSummary: since_ms < 0 — INVALID_ARGUMENT; группы нет — NOT_FOUND.
// Без group_id отдаются все группы, у которых есть сессии за период; с group_id —
// ровно одна запись (нули, если сессий нет). Сессия входит в период, если
// началась не раньше since_ms. Идущая сессия сбрасывается в базу перед чтением,
// чтобы сводка включала трафик «сейчас».
func (s *Server) GetTrafficSummary(ctx context.Context, req *connect.Request[lavrayv1.GetTrafficSummaryRequest]) (*connect.Response[lavrayv1.GetTrafficSummaryResponse], error) {
	since := req.Msg.GetSinceMs()
	if since < 0 {
		return nil, invalidArg("since_ms не может быть отрицательным")
	}
	groupID := req.Msg.GroupId
	if groupID != nil {
		if _, err := s.st.GetGroup(*groupID); err != nil {
			return nil, mapErr(err)
		}
	}
	s.flushAccounting()
	rows, err := s.st.SumSessions(groupID, since)
	if err != nil {
		return nil, mapErr(err)
	}
	resp := &lavrayv1.GetTrafficSummaryResponse{}
	for _, r := range rows {
		resp.Groups = append(resp.Groups, groupSummary(r))
	}
	if groupID != nil && len(resp.Groups) == 0 {
		resp.Groups = []*lavrayv1.GroupTrafficSummary{{GroupId: *groupID}}
	}
	return connect.NewResponse(resp), nil
}

func groupSummary(r store.GroupTraffic) *lavrayv1.GroupTrafficSummary {
	return &lavrayv1.GroupTrafficSummary{
		GroupId:        r.GroupID,
		UplinkBytes:    r.UplinkBytes,
		DownlinkBytes:  r.DownlinkBytes,
		SessionCount:   r.SessionCount,
		FirstSessionMs: int64p(r.FirstStartMs),
		LastSessionMs:  int64p(r.LastStartMs),
	}
}
