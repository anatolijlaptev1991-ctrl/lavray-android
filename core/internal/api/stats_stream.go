// SPDX-License-Identifier: GPL-3.0-only

package api

// stats_stream.go — StatsService.Subscribe (T-79). Поток живёт дольше одного
// подключения: пока инстанса нет, он ждёт; на каждую сессию шлёт кадры со
// скоростями по тегам и итогами сессии, а при остановке — финальный кадр
// (final = true), после чего снова ждёт. Клиенту не нужно переподписываться.

import (
	"context"
	"time"

	"connectrpc.com/connect"

	lavrayv1 "github.com/anatolijlaptev1991-ctrl/lavray-android/core/internal/gen/lavrayv1"
	"github.com/anatolijlaptev1991-ctrl/lavray-android/core/internal/inst"
	"github.com/anatolijlaptev1991-ctrl/lavray-android/core/internal/traffic"
)

// statsSvc — отдельный receiver для StatsService.Subscribe (коллизия имён
// с LogService.Subscribe на одном Server).
type statsSvc struct{ *Server }

func (s statsSvc) Subscribe(ctx context.Context, req *connect.Request[lavrayv1.SubscribeStatsRequest], stream *connect.ServerStream[lavrayv1.StatsSnapshot]) error {
	interval, err := s.statsInterval(req.Msg)
	if err != nil {
		return err
	}
	keep, err := tagFilter(req.Msg.Tags)
	if err != nil {
		return err
	}
	events, cancel := s.inst.Subscribe()
	defer cancel()
	for {
		src := s.inst.Traffic()
		if src == nil {
			select {
			case <-ctx.Done():
				return nil
			case _, ok := <-events:
				if !ok {
					return nil // Shutdown ядра
				}
			}
			continue
		}
		closed, err := s.streamSession(ctx, stream, src, interval, keep, events)
		if err != nil || closed || ctx.Err() != nil {
			return err // closed — Shutdown ядра: финальный кадр уже отправлен
		}
	}
}

// streamSession ведёт поток одной сессии до её конца (финальный кадр отправлен)
// или до отмены. База скоростей берётся сразу при входе, поэтому первый кадр
// приходит через interval и уже со скоростью, а не с нулём. closed — канал
// событий закрыт (Shutdown ядра): ждать следующей сессии бессмысленно.
func (s statsSvc) streamSession(ctx context.Context, stream *connect.ServerStream[lavrayv1.StatsSnapshot],
	src traffic.Source, interval time.Duration, keep func(string) bool, events <-chan inst.Event) (closed bool, err error) {
	run := s.inst.Status().Run
	meter := traffic.NewMeter()
	var tick <-chan time.Time
	if interval > 0 {
		meter.TickSource(time.Now(), src)
		t := time.NewTicker(interval)
		defer t.Stop()
		tick = t.C
	}
	for {
		select {
		case <-ctx.Done():
			return false, nil
		case now := <-tick:
			if s.inst.Traffic() != src { // события могли потеряться: сверяем по источнику
				return false, stream.Send(finalFrame(run, src, meter, keep))
			}
			frame := &lavrayv1.StatsSnapshot{
				TsMs:    now.UnixMilli(),
				Tags:    tagStats(meter.TickSource(now, src), keep, false),
				Session: sessionStat(run, src),
			}
			if err := stream.Send(frame); err != nil {
				return false, err
			}
		case ev, ok := <-events:
			if end, closed := sessionOver(ev, ok, s.inst.Traffic(), src); end {
				return closed, stream.Send(finalFrame(run, src, meter, keep))
			}
		}
	}
}

// sessionOver — чем событие инстанса оборачивается для потока сессии src: end —
// сессия кончилась (пора слать итог), closed — канал событий закрыт (Shutdown
// ядра). Событие остановки, пока источник инстанса всё ещё src, пролежало в
// буфере, пока поток ждал Send, и относится к прошлой сессии: оно пропускается,
// иначе посреди живой сессии ушёл бы ложный финальный кадр.
func sessionOver(ev inst.Event, ok bool, cur, src traffic.Source) (end, closed bool) {
	if !ok {
		return true, true
	}
	stopped := ev.Status.State == inst.StateIdle || ev.Status.State == inst.StateCrashed
	return stopped && cur != src, false
}

// finalFrame — кадр после остановки: итоги сессии и накопленные объёмы по тегам.
func finalFrame(run *inst.RunInfo, src traffic.Source, meter *traffic.Meter, keep func(string) bool) *lavrayv1.StatsSnapshot {
	now := time.Now()
	return &lavrayv1.StatsSnapshot{
		TsMs:    now.UnixMilli(),
		Tags:    tagStats(meter.TickSource(now, src), keep, true),
		Session: sessionStat(run, src),
		Final:   true,
	}
}
