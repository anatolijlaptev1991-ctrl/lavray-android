// SPDX-License-Identifier: GPL-3.0-only

package gateway

import (
	"context"
	"time"
)

// DefaultWatchdogInterval — как часто цикл опрашивает связь и проверяет порог.
// Порог — минуты, поэтому интервал в секундах возвращает правила быстро, а
// нагрузки почти не создаёт.
const DefaultWatchdogInterval = 10 * time.Second

// Run крутит watchdog до отмены ctx: пока probe() истинен (шлюзовый инстанс
// работает и связь есть) — Beat, затем всегда Tick. Первый обход делается сразу.
// interval <= 0 — DefaultWatchdogInterval.
func (s *Service) Run(ctx context.Context, interval time.Duration, probe func() bool) {
	if interval <= 0 {
		interval = DefaultWatchdogInterval
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		if probe() {
			s.Beat()
		}
		s.Tick()
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}
