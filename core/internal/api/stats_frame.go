// SPDX-License-Identifier: GPL-3.0-only

package api

// stats_frame.go — сборка кадров статистики (T-79): общее для потока
// StatsService.Subscribe и ответа Disconnect.

import (
	lavrayv1 "github.com/anatolijlaptev1991-ctrl/lavray-android/core/internal/gen/lavrayv1"
	"github.com/anatolijlaptev1991-ctrl/lavray-android/core/internal/inst"
	"github.com/anatolijlaptev1991-ctrl/lavray-android/core/internal/traffic"
)

// maxStatsTags — сколько тегов можно запросить в Subscribe.
const maxStatsTags = 100

// sessionStat — итоги текущей (или только что закончившейся) сессии: профиль и
// время старта из запуска, байты — из источника (читается и после остановки).
func sessionStat(run *inst.RunInfo, src traffic.Source) *lavrayv1.SessionStat {
	up, down := src.Total()
	st := &lavrayv1.SessionStat{UplinkTotal: up, DownlinkTotal: down}
	if run != nil {
		st.StartedAtMs = run.StartedAtMs
		if run.ProfileID != 0 {
			st.ProfileId = int64p(run.ProfileID)
		}
	}
	return st
}

// tagFilter — отбор тегов запроса: пустой список — все теги.
func tagFilter(tags []string) (func(string) bool, error) {
	if len(tags) > maxStatsTags {
		return nil, invalidArg("tags: не больше %d тегов", maxStatsTags)
	}
	if len(tags) == 0 {
		return func(string) bool { return true }, nil
	}
	set := make(map[string]struct{}, len(tags))
	for _, t := range tags {
		set[t] = struct{}{}
	}
	return func(tag string) bool { _, ok := set[tag]; return ok }, nil
}

// tagStats переводит скорости счётчика в сообщения; noRates обнуляет скорости
// (финальный кадр: соединений уже нет, скорость не имеет смысла).
func tagStats(rates []traffic.TagRate, keep func(string) bool, noRates bool) []*lavrayv1.TagStat {
	var out []*lavrayv1.TagStat
	for _, r := range rates {
		if !keep(r.Tag) {
			continue
		}
		ts := &lavrayv1.TagStat{Tag: r.Tag, UplinkTotal: r.UpTotal, DownlinkTotal: r.DownTotal}
		if !noRates {
			ts.UplinkBps, ts.DownlinkBps = r.UpBps, r.DownBps
		}
		out = append(out, ts)
	}
	return out
}

// finalSnapshot — финальные счётчики сессии для Disconnect (FR-UI-211/214).
func finalSnapshot(run *inst.RunInfo, src traffic.Source) *lavrayv1.StatsSnapshot {
	return &lavrayv1.StatsSnapshot{TsMs: nowMs(), Session: sessionStat(run, src), Final: true}
}
