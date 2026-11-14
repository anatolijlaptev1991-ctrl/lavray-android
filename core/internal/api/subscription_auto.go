// SPDX-License-Identifier: GPL-3.0-only

// subscription_auto.go — автообновление подписок по таймеру (FR-UI-126, T-20):
// обновление одной группы без стрима и планировщик (internal/subs.Scheduler),
// подключённый к настройкам и журналу сервера.
package api

import (
	"context"
	"time"

	"github.com/anatolijlaptev1991-ctrl/lavray-android/core/internal/logs"
	"github.com/anatolijlaptev1991-ctrl/lavray-android/core/internal/subs"
)

// AutoUpdateSubscription обновляет одну подписку по запросу планировщика.
// Те же правила, что у ручного обновления (updateOne): на группу один
// обновляющий за раз (subsRunning; занято → subs.ErrBusy) и снапшот «до»
// для RollbackSubscription. Отличие одно: подтверждений нет — деструктивное
// обновление (пустой ответ, удаление >50 % профилей) не применяется и группа
// не меняется, ошибка — *subs.ConfirmRequiredError.
func (s *Server) AutoUpdateSubscription(ctx context.Context, groupID int64) error {
	s.subsMu.Lock()
	if s.subsRunning == nil {
		s.subsRunning = map[int64]bool{}
	}
	if s.subsRunning[groupID] {
		s.subsMu.Unlock()
		return subs.ErrBusy
	}
	s.subsRunning[groupID] = true
	s.subsMu.Unlock()
	defer func() {
		s.subsMu.Lock()
		delete(s.subsRunning, groupID)
		s.subsMu.Unlock()
	}()

	g, err := s.st.GetGroup(groupID)
	if err != nil {
		return err
	}
	if g.Archive || g.SkipAutoUpdate {
		return nil // пользователь отключил автообновление после выбора группы
	}
	res, err := s.subscriberWith(subRequest{}).Update(ctx, s.st, g, false)
	if err != nil {
		return err
	}
	if res != nil && res.Snapshot != nil {
		s.subsMu.Lock()
		if s.subsSnap == nil {
			s.subsSnap = map[int64]*subs.Snapshot{}
		}
		s.subsSnap[groupID] = res.Snapshot
		s.subsMu.Unlock()
	}
	return nil
}

// StartSubscriptionScheduler запускает планировщик автообновления подписок
// и возвращает функцию остановки: она отменяет планировщик и ждёт, пока
// закончится текущее обновление (не дольше 5 с). Восстановление копии
// перезапускает планировщик через s.sched (sched_ctl.go).
func (s *Server) StartSubscriptionScheduler(ctx context.Context) (stop func()) {
	return s.sched.start(ctx, s.runSubscriptionScheduler)
}

// runSubscriptionScheduler — один запуск планировщика; stop отменяет его.
func (s *Server) runSubscriptionScheduler(ctx context.Context) (stop func()) {
	sched := subs.NewScheduler(subs.SchedulerConfig{
		Groups:            s.st.ListGroups,
		GlobalIntervalMin: func() int { return s.settingInt("sub.auto_update") },
		Update:            s.AutoUpdateSubscription,
		Log: func(format string, args ...any) {
			s.logs.Logf(logs.LevelInfo, logs.SourceSubs, format, args...)
		},
	})
	ctx, cancel := context.WithCancel(ctx)
	done := make(chan struct{})
	go func() {
		defer close(done)
		sched.Run(ctx)
	}()
	return func() {
		cancel()
		select {
		case <-done:
		case <-time.After(5 * time.Second):
		}
	}
}
