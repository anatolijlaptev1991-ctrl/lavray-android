// SPDX-License-Identifier: GPL-3.0-only

// group_service.go — GroupService (аудит №4 A2): список/создание/правка/
// удаление/порядок групп. Без ListGroups UI не мог узнать id встроенной
// группы и слал импорт в несуществующую group_id=1.
package api

import (
	"context"
	"errors"
	"strings"

	"connectrpc.com/connect"
	lavrayv1 "github.com/anatolijlaptev1991-ctrl/lavray-android/core/internal/gen/lavrayv1"
	"github.com/anatolijlaptev1991-ctrl/lavray-android/core/internal/model"
	"github.com/anatolijlaptev1991-ctrl/lavray-android/core/internal/store"
)

// maxUserAgentLen — предел длины User-Agent подписки (байт).
const maxUserAgentLen = 256

func (s *Server) ListGroups(ctx context.Context, req *connect.Request[lavrayv1.ListGroupsRequest]) (*connect.Response[lavrayv1.ListGroupsResponse], error) {
	list, err := s.st.ListGroups()
	if err != nil {
		return nil, mapErr(err)
	}
	out := &lavrayv1.ListGroupsResponse{}
	for _, g := range list {
		out.Groups = append(out.Groups, groupToProto(g))
	}
	return connect.NewResponse(out), nil
}

func (s *Server) CreateGroup(ctx context.Context, req *connect.Request[lavrayv1.CreateGroupRequest]) (*connect.Response[lavrayv1.Group], error) {
	in := req.Msg.Group
	if in == nil || in.Name == "" {
		return nil, invalidArg("group.name обязателен")
	}
	g := &model.Group{Name: in.Name, Kind: model.GroupBasic}
	if err := s.st.CreateGroup(g); err != nil {
		return nil, mapErr(err)
	}
	return connect.NewResponse(groupToProto(g)), nil
}

func (s *Server) UpdateGroup(ctx context.Context, req *connect.Request[lavrayv1.UpdateGroupRequest]) (*connect.Response[lavrayv1.Group], error) {
	in := req.Msg.Group
	if in == nil || in.Id == 0 {
		return nil, invalidArg("group.id обязателен")
	}
	g, err := s.st.GetGroup(in.Id)
	if err != nil {
		return nil, mapErr(err)
	}
	if in.Name != "" {
		g.Name = in.Name
	}
	g.Archive = in.Archive
	g.SkipAutoUpdate = in.SkipAutoUpdate
	// T-21: per-sub настройки подписки (FR-UI-263). optional-поле, которого нет
	// в запросе, значение не меняет (так переключатель «Автообновление» не
	// затирает интервал и User-Agent); 0 и пустая строка сбрасывают настройку
	// к глобальной.
	if in.UpdateIntervalMin != nil {
		switch v := *in.UpdateIntervalMin; {
		case v < 0:
			return nil, invalidArg("update_interval_min не может быть отрицательным")
		case v == 0:
			g.UpdateIntervalMin = nil
		default:
			g.UpdateIntervalMin = int32p(v)
		}
	}
	if in.UserAgent != nil {
		ua := strings.TrimSpace(*in.UserAgent)
		if len(ua) > maxUserAgentLen || strings.ContainsAny(ua, "\r\n\x00") {
			return nil, invalidArg("user_agent: не длиннее %d байт, без переводов строки", maxUserAgentLen)
		}
		g.UserAgent = ua
	}
	if err := s.st.UpdateGroup(g); err != nil {
		return nil, mapErr(err)
	}
	return connect.NewResponse(groupToProto(g)), nil
}

// guardActiveGroup отказывает, если в группе работает профиль активного
// соединения (T-12, B1-19). Удаление группы каскадно убирает её профили
// (profiles.group_id ... ON DELETE CASCADE): без этой охраны активный профиль
// исчезал из-под работающего инстанса — статус оставался CONNECTED, GetProfile
// отвечал NotFound, RestartProfile падал. Для DeleteProfile такая охрана уже
// есть (A9); используют DeleteGroup и DeleteSubscription.
func (s *Server) guardActiveGroup(groupID int64) error {
	run := s.inst.Status().Run // nil = нет активного соединения
	if run == nil || run.ProfileID == 0 {
		return nil
	}
	// Группу берём у профиля сейчас, а не из RunInfo: профиль могли перенести.
	p, err := s.st.GetProfile(run.ProfileID)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return nil // профиль уже удалён — защищать нечего
		}
		return mapErr(err)
	}
	if p.GroupID != groupID {
		return nil
	}
	return failedPrecondition(
		"группа содержит активный профиль «%s» — сначала отключитесь или переключитесь на сервер из другой группы",
		p.Name)
}

func (s *Server) DeleteGroup(ctx context.Context, req *connect.Request[lavrayv1.DeleteGroupRequest]) (*connect.Response[lavrayv1.Empty], error) {
	if err := s.guardActiveGroup(req.Msg.Id); err != nil {
		return nil, err
	}
	if err := s.st.DeleteGroup(req.Msg.Id); err != nil {
		return nil, mapErr(err)
	}
	return connect.NewResponse(&lavrayv1.Empty{}), nil
}

func (s *Server) ReorderGroups(ctx context.Context, req *connect.Request[lavrayv1.ReorderGroupsRequest]) (*connect.Response[lavrayv1.Empty], error) {
	if err := s.st.ReorderGroups(req.Msg.OrderedIds); err != nil {
		return nil, mapErr(err)
	}
	return connect.NewResponse(&lavrayv1.Empty{}), nil
}
