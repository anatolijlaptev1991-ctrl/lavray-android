// SPDX-License-Identifier: GPL-3.0-only

package api

// stats_interval.go — интервал кадров StatsService.Subscribe (T-79).

import (
	"time"

	lavrayv1 "github.com/anatolijlaptev1991-ctrl/lavray-android/core/internal/gen/lavrayv1"
)

const (
	// settingStatsInterval — интервал кадров из настроек, мс.
	settingStatsInterval = "stats.interval_ms"

	// Переопределение в запросе: 0 — «из настроек», иначе 250..10000 мс.
	statsOverrideMinMs = 250
	statsOverrideMaxMs = 10000
	// Значение из настроек: 0 — кадры выключены (только финальный), иначе
	// зажимается в 500..5000 мс (FR-UI-168).
	statsSettingMinMs = 500
	statsSettingMaxMs = 5000
)

// statsInterval возвращает период кадров потока. 0 — периодических кадров нет,
// поток шлёт только финальный. Интервал в запросе вне {0} ∪ [250..10000] —
// INVALID_ARGUMENT.
func (s *Server) statsInterval(req *lavrayv1.SubscribeStatsRequest) (time.Duration, error) {
	if ms := optInt32(req.IntervalMs); ms != 0 {
		if ms < statsOverrideMinMs || ms > statsOverrideMaxMs {
			return 0, invalidArg("interval_ms должен быть 0 или от %d до %d", statsOverrideMinMs, statsOverrideMaxMs)
		}
		return time.Duration(ms) * time.Millisecond, nil
	}
	ms := s.settingInt(settingStatsInterval)
	if ms == 0 {
		return 0, nil
	}
	return time.Duration(max(statsSettingMinMs, min(ms, statsSettingMaxMs))) * time.Millisecond, nil
}
