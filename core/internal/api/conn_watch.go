// SPDX-License-Identifier: GPL-3.0-only

package api

// conn_watch.go — WatchConnections (T-79): каждый кадр — полный список
// активных соединений (до active_page_limit, сначала новые), закрытые с
// прошлого кадра и признак усечения. Кадры идут не чаще interval_ms.

import (
	"context"
	"sort"
	"time"

	"connectrpc.com/connect"

	lavrayv1 "github.com/anatolijlaptev1991-ctrl/lavray-android/core/internal/gen/lavrayv1"
	"github.com/anatolijlaptev1991-ctrl/lavray-android/core/internal/traffic"
)

const (
	defaultWatchIntervalMs = 1000
	minWatchIntervalMs     = 250
	maxWatchIntervalMs     = 10000
	defaultWatchLimit      = 500
)

// watchInterval: не задан или 0 — 1000 мс; меньше 0 — INVALID_ARGUMENT; иначе
// значение зажимается в 250..10000 мс (слишком частые кадры — нагрузка на роутер,
// слишком редкие — зависший экран).
func watchInterval(ms *int32) (time.Duration, error) {
	v := int64(optInt32(ms))
	if v < 0 {
		return 0, invalidArg("interval_ms не может быть отрицательным")
	}
	if v == 0 {
		v = defaultWatchIntervalMs
	}
	return time.Duration(max(minWatchIntervalMs, min(v, maxWatchIntervalMs))) * time.Millisecond, nil
}

// watchLimit: не задан или 0 — 500; меньше 0 или больше 5000 — INVALID_ARGUMENT.
func watchLimit(v *int32) (int, error) {
	n := int(optInt32(v))
	if n < 0 || n > maxConnPage {
		return 0, invalidArg("active_page_limit должен быть от 0 до %d", maxConnPage)
	}
	if n == 0 {
		n = defaultWatchLimit
	}
	return n, nil
}

// connWatch — состояние одного потока: какие соединения были активны в
// прошлом кадре. Источник меняется при перезапуске инстанса, поток при этом
// живёт: соединения старого инстанса уходят в closed_ids.
type connWatch struct {
	filter connFilter
	limit  int
	prev   map[string]struct{} // все подходящие активные прошлого кадра
}

func newConnWatch(msg *lavrayv1.WatchConnectionsRequest) (*connWatch, time.Duration, error) {
	interval, err := watchInterval(msg.IntervalMs)
	if err != nil {
		return nil, 0, err
	}
	limit, err := watchLimit(msg.ActivePageLimit)
	if err != nil {
		return nil, 0, err
	}
	return &connWatch{
		filter: newConnFilter(msg.OutboundFilter, msg.DestinationFilter),
		limit:  limit,
		prev:   map[string]struct{}{},
	}, interval, nil
}

// frame строит кадр по текущему источнику (nil — инстанса нет: активных нет).
// Закрытые — те, что были активны в прошлом кадре и пропали из списка: каждый
// id попадает ровно в один кадр, и только если клиент мог его видеть.
// Соединение, открывшееся и закрывшееся между кадрами, не называется вовсе.
// События источника для этого не нужны: они асинхронны и приходят позже
// изменения списка, отчего закрытие объявлялось бы дважды.
func (w *connWatch) frame(src traffic.Source) *lavrayv1.ConnectionSnapshot {
	var all []traffic.ConnInfo
	if src != nil {
		all = src.Active()
		if w.filter != (connFilter{}) {
			all = filterConns(all, w.filter)
		}
	}
	cur := make(map[string]struct{}, len(all))
	for _, c := range all {
		cur[c.ID] = struct{}{}
	}
	snap := &lavrayv1.ConnectionSnapshot{ClosedIds: w.closedIDs(cur)}
	w.prev = cur
	snap.Truncated = len(all) > w.limit
	// Порядок нужен только первым limit строкам: частичный отбор вместо
	// сортировки всего списка (R0-D).
	for _, c := range firstConns(all, w.limit) {
		snap.Active = append(snap.Active, connToProto(c))
	}
	return snap
}

// closedIDs — id прошлого кадра, которых нет в cur, по возрастанию.
func (w *connWatch) closedIDs(cur map[string]struct{}) []string {
	var ids []string
	for id := range w.prev {
		if _, alive := cur[id]; !alive {
			ids = append(ids, id)
		}
	}
	sort.Strings(ids)
	return ids
}

func (s *Server) WatchConnections(ctx context.Context, req *connect.Request[lavrayv1.WatchConnectionsRequest], stream *connect.ServerStream[lavrayv1.ConnectionSnapshot]) error {
	w, interval, err := newConnWatch(req.Msg)
	if err != nil {
		return err
	}
	tick := time.NewTicker(interval)
	defer tick.Stop()
	for {
		if err := stream.Send(w.frame(s.inst.Traffic())); err != nil {
			return err
		}
		select {
		case <-ctx.Done():
			return nil
		case <-tick.C:
		}
	}
}
