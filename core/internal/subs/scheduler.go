// SPDX-License-Identifier: GPL-3.0-only

package subs

// scheduler.go — автообновление подписок по таймеру (FR-UI-126, T-20, B1-08).
//
// Раз в TickEvery планировщик просматривает группы и обновляет те, у которых
// прошёл интервал с последнего успешного обновления (Group.LastUpdateMs).
// Интервал группы (AutoInterval), по убыванию приоритета:
//  1. Group.UpdateIntervalMin > 0 — настройка подписки (FR-UI-263);
//  2. настройка sub.auto_update ≥ 30 (минуты) — общая (FR-UI-126, как в NekoBox);
//  3. SubscriptionInfo.UpdateIntervalHours > 0 — интервал, который назвал
//     провайдер (profile-update-interval).
//
// Не чаще MinAutoInterval (30 мин: защита провайдера). Архивные группы,
// группы с skip_auto_update и без URL не обновляются никогда.
//
// Деструктивное обновление (пустой ответ, удаление >50 % профилей) планировщик
// не применяет: Subscriber.Update без подтверждения возвращает
// ConfirmRequiredError и группу не трогает; обновить такую подписку можно
// вручную с подтверждением (FR-UI-258).

import (
	"context"
	"errors"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/anatolijlaptev1991-ctrl/lavray-android/core/internal/model"
)

const (
	// MinAutoInterval — самый частый допустимый интервал автообновления.
	MinAutoInterval = 30 * time.Minute
	// DefaultTickEvery — период просмотра групп.
	DefaultTickEvery = time.Minute
	// DefaultStartDelay — пауза после запуска ядра до первого просмотра:
	// не мешаем старту приложения и автоподключению.
	DefaultStartDelay = 30 * time.Second

	schedulerRetryBase     = 5 * time.Minute // первая пауза после сбоя; дальше вдвое больше
	schedulerUpdateTimeout = 3 * time.Minute // потолок одного обновления
)

// ErrBusy — обновление этой подписки уже идёт (вручную либо планировщиком).
// Update возвращает её, не выполнив работу; планировщик повторит на
// следующем просмотре без штрафной паузы.
var ErrBusy = errors.New("подписка: обновление уже выполняется")

// SchedulerConfig — зависимости планировщика (инъекция: часы и обновление
// подменяются в тестах).
type SchedulerConfig struct {
	// Groups — текущий список групп (store.ListGroups). Обязательно.
	Groups func() ([]*model.Group, error)
	// GlobalIntervalMin — настройка sub.auto_update в минутах; nil → 0.
	GlobalIntervalMin func() int
	// Update обновляет одну подписку без подтверждений. Обязательно.
	// ErrBusy — «занято», *ConfirmRequiredError — «нужно подтверждение»,
	// прочее — сбой.
	Update func(ctx context.Context, groupID int64) error
	// Now — часы; nil → time.Now.
	Now func() time.Time
	// TickEvery — период просмотра групп; ≤0 → DefaultTickEvery.
	TickEvery time.Duration
	// StartDelay — пауза перед первым просмотром в Run; 0 → DefaultStartDelay,
	// <0 → без паузы.
	StartDelay time.Duration
	// Log — журнал планировщика; nil → без вывода.
	Log func(format string, args ...any)
}

// Scheduler — планировщик автообновления подписок.
type Scheduler struct {
	cfg   SchedulerConfig
	mu    sync.Mutex
	state map[int64]*attemptState
}

// attemptState — состояние попыток группы (только в памяти).
type attemptState struct {
	nextTry  time.Time // раньше этого момента не пробовать
	failures int       // подряд неудачных попыток
}

// NewScheduler создаёт планировщик; Groups и Update обязательны.
func NewScheduler(cfg SchedulerConfig) *Scheduler {
	if cfg.Now == nil {
		cfg.Now = time.Now
	}
	if cfg.TickEvery <= 0 {
		cfg.TickEvery = DefaultTickEvery
	}
	if cfg.StartDelay == 0 {
		cfg.StartDelay = DefaultStartDelay
	}
	return &Scheduler{cfg: cfg, state: map[int64]*attemptState{}}
}

// AutoInterval — интервал автообновления группы; 0 — не обновлять.
// globalMin — настройка sub.auto_update (минуты).
func AutoInterval(g *model.Group, globalMin int) time.Duration {
	if g == nil || g.Kind != model.GroupSubscription || g.Archive || g.SkipAutoUpdate ||
		strings.TrimSpace(g.SubscriptionURL) == "" {
		return 0
	}
	var d time.Duration
	switch {
	case g.UpdateIntervalMin != nil && *g.UpdateIntervalMin > 0:
		d = time.Duration(*g.UpdateIntervalMin) * time.Minute
	case time.Duration(globalMin)*time.Minute >= MinAutoInterval:
		d = time.Duration(globalMin) * time.Minute
	case g.SubscriptionInfo != nil && g.SubscriptionInfo.UpdateIntervalHours != nil &&
		*g.SubscriptionInfo.UpdateIntervalHours > 0:
		d = time.Duration(*g.SubscriptionInfo.UpdateIntervalHours) * time.Hour
	default:
		return 0
	}
	return max(d, MinAutoInterval)
}

// Run крутит планировщик до отмены ctx.
func (s *Scheduler) Run(ctx context.Context) {
	if s.cfg.StartDelay > 0 {
		select {
		case <-time.After(s.cfg.StartDelay):
		case <-ctx.Done():
			return
		}
	}
	ticker := time.NewTicker(s.cfg.TickEvery)
	defer ticker.Stop()
	for {
		s.Tick(ctx)
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

// Tick — один просмотр групп: обновляет те, что подошли по сроку. Группы
// обновляются по очереди, не параллельно.
func (s *Scheduler) Tick(ctx context.Context) {
	groups, err := s.cfg.Groups()
	if err != nil {
		s.logf("автообновление подписок: список групп: %v", err)
		return
	}
	global := 0
	if s.cfg.GlobalIntervalMin != nil {
		global = s.cfg.GlobalIntervalMin()
	}
	now := s.cfg.Now()
	for _, g := range groups {
		if ctx.Err() != nil {
			return
		}
		interval := AutoInterval(g, global)
		if interval == 0 {
			s.forget(g.ID)
			continue
		}
		if !s.due(g, interval, now) {
			continue
		}
		s.updateOne(ctx, g, interval, now)
	}
}

// due — подошёл ли срок обновления группы.
func (s *Scheduler) due(g *model.Group, interval time.Duration, now time.Time) bool {
	s.mu.Lock()
	st := s.state[g.ID]
	s.mu.Unlock()
	if st != nil && now.Before(st.nextTry) {
		return false
	}
	if g.LastUpdateMs == 0 {
		return true // ни разу не обновлялась
	}
	last := time.UnixMilli(g.LastUpdateMs)
	if last.After(now) {
		return true // часы переводили назад: иначе ждали бы до «будущего» момента
	}
	return !now.Before(last.Add(interval))
}

func (s *Scheduler) updateOne(ctx context.Context, g *model.Group, interval time.Duration, now time.Time) {
	defer func() {
		if r := recover(); r != nil {
			s.logf("автообновление подписки «%s»: внутренняя ошибка: %v", g.Name, r)
			s.setRetry(g.ID, now, interval, true)
		}
	}()
	uctx, cancel := context.WithTimeout(ctx, schedulerUpdateTimeout)
	defer cancel()
	err := s.cfg.Update(uctx, g.ID)

	var confirm *ConfirmRequiredError
	switch {
	case err == nil:
		s.logf("автообновление подписки «%s»: обновлена", g.Name)
		s.reset(g.ID, now.Add(interval))
	case errors.Is(err, ErrBusy):
		// Идёт ручное обновление: на следующем просмотре посмотрим снова.
	case ctx.Err() != nil:
		// Ядро останавливается — не считаем сбоем подписки.
	case errors.As(err, &confirm):
		s.logf("автообновление подписки «%s» не применено: %s. Обновите вручную и подтвердите", g.Name, confirm.Reason)
		s.reset(g.ID, now.Add(interval))
	default:
		s.logf("автообновление подписки «%s»: %s", g.Name, scrubURLs(err.Error()))
		s.setRetry(g.ID, now, interval, true)
	}
}

// setRetry назначает следующую попытку после сбоя: 5 мин, 10, 20… но не позже
// обычного интервала группы.
func (s *Scheduler) setRetry(id int64, now time.Time, interval time.Duration, failed bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	st := s.state[id]
	if st == nil {
		st = &attemptState{}
		s.state[id] = st
	}
	if failed {
		st.failures++
	}
	shift := min(st.failures-1, 10)
	backoff := min(schedulerRetryBase<<shift, interval)
	st.nextTry = now.Add(backoff)
}

func (s *Scheduler) reset(id int64, nextTry time.Time) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.state[id] = &attemptState{nextTry: nextTry}
}

func (s *Scheduler) forget(id int64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.state, id)
}

func (s *Scheduler) logf(format string, args ...any) {
	if s.cfg.Log != nil {
		s.cfg.Log(format, args...)
	}
}

// urlPattern — любая ссылка в тексте ошибки: URL подписки несёт токен
// пользователя (…/sub/<токен>), в журнал он не должен попадать.
var urlPattern = regexp.MustCompile(`[A-Za-z][A-Za-z0-9+.-]*://[^\s"']+`)

func scrubURLs(s string) string {
	return urlPattern.ReplaceAllString(s, "<url>")
}
