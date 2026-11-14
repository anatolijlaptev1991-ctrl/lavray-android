// SPDX-License-Identifier: GPL-3.0-only

// update_scheduler.go — фоновая проверка обновлений раз в 6 часов ± 30 минут,
// если включена настройка update.auto_check (по умолчанию да). Проверка
// только обновляет состояние (last_checked_at, serial): ничего не скачивает и
// не устанавливает. Без вшитого корня доверия планировщик ничего не делает.
package api

import (
	"context"
	"math/rand/v2"
	"time"

	lavrayv1 "github.com/anatolijlaptev1991-ctrl/lavray-android/core/internal/gen/lavrayv1"
	"github.com/anatolijlaptev1991-ctrl/lavray-android/core/internal/logs"
	"github.com/anatolijlaptev1991-ctrl/lavray-android/core/internal/updates"
)

const (
	updateCheckInterval = 6 * time.Hour
	updateCheckJitter   = 30 * time.Minute
	updateFirstDelay    = time.Minute // первая проверка после старта, если давно не проверяли
)

// nextUpdateDelay — 6 ч ± 30 мин; rnd возвращает число из [0, 1).
func nextUpdateDelay(rnd func() float64) time.Duration {
	return updateCheckInterval + time.Duration((rnd()*2-1)*float64(updateCheckJitter))
}

// StartUpdateScheduler запускает фоновые проверки; возвращённая функция
// останавливает цикл и дожидается его завершения.
func (s *Server) StartUpdateScheduler(ctx context.Context) (stop func()) {
	ctx, cancel := context.WithCancel(ctx)
	done := make(chan struct{})
	go func() {
		defer close(done)
		if !s.upd.configured() {
			return
		}
		delay := s.firstUpdateDelay()
		for {
			select {
			case <-ctx.Done():
				return
			case <-time.After(delay):
			}
			s.updateTick(ctx)
			delay = nextUpdateDelay(rand.Float64)
		}
	}()
	return func() { cancel(); <-done }
}

// firstUpdateDelay: недавно проверяли — ждём остаток интервала, иначе через минуту.
func (s *Server) firstUpdateDelay() time.Duration {
	last := s.loadUpdateState().LastCheckedAt
	if last > 0 {
		if rest := updateCheckInterval - time.Since(time.UnixMilli(last)); rest > updateFirstDelay {
			return rest
		}
	}
	return updateFirstDelay
}

// updateTick — одна фоновая проверка; false — автопроверка выключена или
// обновления не настроены.
func (s *Server) updateTick(ctx context.Context) bool {
	if !s.upd.configured() || !s.settingBool("update.auto_check") {
		return false
	}
	channel := lavrayv1.UpdateChannel_UPDATE_CHANNEL_STABLE
	if s.settingStr("update.channel") == updates.ChannelBeta {
		channel = lavrayv1.UpdateChannel_UPDATE_CHANNEL_BETA
	}
	c, st, err := s.updateClient(channel)
	if err == nil {
		if _, err = c.Check(ctx); err == nil {
			s.saveUpdateState(st)
			return true
		}
	}
	s.logs.Logf(logs.LevelWarn, "update", "фоновая проверка обновлений не удалась: %v", err)
	return true
}
