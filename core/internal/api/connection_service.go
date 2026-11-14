// SPDX-License-Identifier: GPL-3.0-only

// connection_service.go — ConnectionService (T-79): список и закрытие
// соединений запущенного инстанса (поток — conn_watch.go). Источник данных
// берётся при каждом вызове у менеджера (s.inst.Traffic()): он живёт столько
// же, сколько инстанс. Нет инстанса — пустой список, а не ошибка: экран
// соединений открыт и до подключения. Адреса назначения — приватные данные
// пользователя: агентам закрыто (agent_table.go), в журнал они не пишутся.
package api

import (
	"context"
	"errors"
	"regexp"

	"connectrpc.com/connect"

	lavrayv1 "github.com/anatolijlaptev1991-ctrl/lavray-android/core/internal/gen/lavrayv1"
	"github.com/anatolijlaptev1991-ctrl/lavray-android/core/internal/traffic"
)

// blockTag — тег исходящего «block» в конфиге ядра (cfgbuild): соединение на
// нём считается заблокированным.
const blockTag = "block"

// connToProto переносит соединение в сообщение контракта.
func connToProto(c traffic.ConnInfo) *lavrayv1.Connection {
	out := &lavrayv1.Connection{
		Id:                  c.ID,
		StartedAtMs:         c.StartMs,
		Status:              connStatus(c),
		Outbound:            c.Outbound,
		Destination:         c.Destination,
		ResolvedDestination: strpIf(c.Domain),
		Inbound:             strpIf(c.Inbound),
		Rule:                strpIf(c.Rule),
		Process:             strpIf(c.Process),
		UploadBytes:         c.Up,
		DownloadBytes:       c.Down,
		Network:             c.Network,
		Chain:               c.Chain,
	}
	if c.EndMs != 0 {
		out.EndedAtMs = int64p(c.EndMs)
	}
	return out
}

func connStatus(c traffic.ConnInfo) lavrayv1.Connection_Status {
	switch {
	case c.Outbound == blockTag:
		return lavrayv1.Connection_STATUS_BLOCKED
	case c.EndMs != 0:
		return lavrayv1.Connection_STATUS_CLOSED
	}
	return lavrayv1.Connection_STATUS_ACTIVE
}

func (s *Server) ListConnections(ctx context.Context, req *connect.Request[lavrayv1.ListConnectionsRequest]) (*connect.Response[lavrayv1.ListConnectionsResponse], error) {
	msg := req.Msg
	offset, limit, err := connPage(msg.Page)
	if err != nil {
		return nil, err
	}
	resp := &lavrayv1.ListConnectionsResponse{}
	src := s.inst.Traffic()
	if src == nil {
		return connect.NewResponse(resp), nil
	}
	filter := newConnFilter(msg.OutboundFilter, msg.DestinationFilter)
	for _, c := range pageOfConns(listConns(src, filter), offset, limit) {
		resp.Connections = append(resp.Connections, connToProto(c))
	}
	return connect.NewResponse(resp), nil
}

// uuidRe — идентификатор соединения: UUID из sing-box (8-4-4-4-12, hex).
var uuidRe = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)

func (s *Server) CloseConnection(ctx context.Context, req *connect.Request[lavrayv1.CloseConnectionRequest]) (*connect.Response[lavrayv1.Empty], error) {
	id := req.Msg.Id
	if id == "" {
		return nil, invalidArg("id соединения не задан")
	}
	if !uuidRe.MatchString(id) {
		return nil, invalidArg("id соединения должен быть UUID")
	}
	src := s.inst.Traffic()
	if src == nil {
		return nil, notFound("соединение не найдено: подключение не активно")
	}
	if err := src.Close(id); err != nil {
		if errors.Is(err, traffic.ErrNotFound) {
			return nil, notFound("соединение не найдено или уже закрыто")
		}
		return nil, err
	}
	return connect.NewResponse(&lavrayv1.Empty{}), nil
}
