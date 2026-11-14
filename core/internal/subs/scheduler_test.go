// SPDX-License-Identifier: GPL-3.0-only

package subs

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/anatolijlaptev1991-ctrl/lavray-android/core/internal/model"
)

// T-20 (B1-08, D2-126): автообновление подписок. Часы подменены: время
// движется только вызовом clock.advance, реальных пауз нет.

type fakeClock struct {
	mu  sync.Mutex
	now time.Time
}

// newFakeClock стартует с настоящего «сейчас»: Apply пишет LastUpdateMs по
// системным часам, и сквозной тест должен видеть согласованное время.
func newFakeClock() *fakeClock {
	return &fakeClock{now: time.Now().Truncate(time.Second)}
}

func (c *fakeClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *fakeClock) advance(d time.Duration) {
	c.mu.Lock()
	c.now = c.now.Add(d)
	c.mu.Unlock()
}

func i32(v int32) *int32 { return &v }

// subGroup — группа-подписка, обновлённая ago назад (0 — ни разу).
func subGroup(clock *fakeClock, id int64, ago time.Duration) *model.Group {
	g := &model.Group{ID: id, Name: fmt.Sprintf("sub%d", id), Kind: model.GroupSubscription,
		SubscriptionURL: fmt.Sprintf("https://example.com/sub/%d", id)}
	if ago > 0 {
		g.LastUpdateMs = clock.Now().Add(-ago).UnixMilli()
	}
	return g
}

// recorder — Update, запоминающий вызовы.
type recorder struct {
	mu    sync.Mutex
	calls []int64
	err   error
}

func (r *recorder) update(_ context.Context, id int64) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.calls = append(r.calls, id)
	return r.err
}

func (r *recorder) take() []int64 {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := r.calls
	r.calls = nil
	return out
}

func sameIDs(got []int64, want ...int64) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range got {
		if got[i] != want[i] {
			return false
		}
	}
	return true
}

func TestAutoInterval(t *testing.T) {
	hours := func(h int32) *model.SubscriptionInfo { return &model.SubscriptionInfo{UpdateIntervalHours: &h} }
	mk := func(mod func(g *model.Group)) *model.Group {
		g := &model.Group{ID: 1, Kind: model.GroupSubscription, SubscriptionURL: "https://example.com/s"}
		mod(g)
		return g
	}
	cases := []struct {
		name   string
		g      *model.Group
		global int
		want   time.Duration
	}{
		{"ничего не задано — выключено", mk(func(*model.Group) {}), 0, 0},
		{"интервал группы", mk(func(g *model.Group) { g.UpdateIntervalMin = i32(90) }), 0, 90 * time.Minute},
		{"интервал группы важнее общего", mk(func(g *model.Group) { g.UpdateIntervalMin = i32(90) }), 600, 90 * time.Minute},
		{"интервал группы меньше минимума поднимается до 30 мин", mk(func(g *model.Group) { g.UpdateIntervalMin = i32(5) }), 0, 30 * time.Minute},
		{"общий интервал", mk(func(*model.Group) {}), 120, 2 * time.Hour},
		{"общий интервал <30 мин — выключено", mk(func(*model.Group) {}), 20, 0},
		{"отрицательный общий (как -30 в NekoBox) — выключено", mk(func(*model.Group) {}), -30, 0},
		{"интервал провайдера", mk(func(g *model.Group) { g.SubscriptionInfo = hours(12) }), 0, 12 * time.Hour},
		{"общий важнее интервала провайдера", mk(func(g *model.Group) { g.SubscriptionInfo = hours(12) }), 60, time.Hour},
		{"skip_auto_update", mk(func(g *model.Group) { g.SkipAutoUpdate = true; g.UpdateIntervalMin = i32(60) }), 60, 0},
		{"архив", mk(func(g *model.Group) { g.Archive = true; g.UpdateIntervalMin = i32(60) }), 60, 0},
		{"обычная группа", mk(func(g *model.Group) { g.Kind = model.GroupBasic; g.UpdateIntervalMin = i32(60) }), 60, 0},
		{"без URL", mk(func(g *model.Group) { g.SubscriptionURL = "  "; g.UpdateIntervalMin = i32(60) }), 60, 0},
		{"nil", nil, 60, 0},
	}
	for _, tc := range cases {
		if got := AutoInterval(tc.g, tc.global); got != tc.want {
			t.Errorf("%s: AutoInterval = %v, ожидалось %v", tc.name, got, tc.want)
		}
	}
}

func TestSchedulerUpdatesOnlyDueGroups(t *testing.T) {
	clock := newFakeClock()
	skip := subGroup(clock, 3, 5*time.Hour)
	skip.SkipAutoUpdate = true
	archived := subGroup(clock, 4, 5*time.Hour)
	archived.Archive = true
	basic := subGroup(clock, 6, 5*time.Hour)
	basic.Kind = model.GroupBasic
	groups := []*model.Group{
		subGroup(clock, 1, 61*time.Minute), // прошёл час — пора
		subGroup(clock, 2, 10*time.Minute), // рано
		skip, archived,
		subGroup(clock, 5, 0), // ни разу не обновлялась
		basic,
	}
	rec := &recorder{}
	s := NewScheduler(SchedulerConfig{
		Groups:            func() ([]*model.Group, error) { return groups, nil },
		GlobalIntervalMin: func() int { return 60 },
		Update:            rec.update,
		Now:               clock.Now,
	})

	s.Tick(context.Background())
	if got := rec.take(); !sameIDs(got, 1, 5) {
		t.Fatalf("первый просмотр обновил %v, ожидались группы 1 и 5", got)
	}

	// Тот же момент: срок записан, повторов нет, даже если LastUpdateMs не менялся.
	s.Tick(context.Background())
	if got := rec.take(); len(got) != 0 {
		t.Fatalf("повторный просмотр в тот же момент обновил %v", got)
	}

	// Через 50 минут «рано» превращается в «пора» (10+50 = 60), остальные — нет.
	clock.advance(50 * time.Minute)
	s.Tick(context.Background())
	if got := rec.take(); !sameIDs(got, 2) {
		t.Fatalf("через 50 мин обновлено %v, ожидалась группа 2", got)
	}

	// Ещё час: снова пора всем подходящим, пропускаемые так и не тронуты.
	clock.advance(time.Hour)
	s.Tick(context.Background())
	if got := rec.take(); !sameIDs(got, 1, 2, 5) {
		t.Fatalf("через час обновлено %v, ожидались 1, 2, 5 (skip/архив/обычная — никогда)", got)
	}
}

func TestSchedulerGroupIntervalAndGlobalOff(t *testing.T) {
	clock := newFakeClock()
	own := subGroup(clock, 1, 45*time.Minute)
	own.UpdateIntervalMin = i32(30) // своя настройка работает и при выключенном общем
	other := subGroup(clock, 2, 45*time.Minute)
	rec := &recorder{}
	s := NewScheduler(SchedulerConfig{
		Groups: func() ([]*model.Group, error) { return []*model.Group{own, other}, nil },
		Update: rec.update,
		Now:    clock.Now,
	})
	s.Tick(context.Background())
	if got := rec.take(); !sameIDs(got, 1) {
		t.Fatalf("обновлено %v, ожидалась только группа со своим интервалом", got)
	}
}

func TestSchedulerClockMovedBackIsDue(t *testing.T) {
	clock := newFakeClock()
	g := subGroup(clock, 1, 0)
	g.UpdateIntervalMin = i32(60)
	g.LastUpdateMs = clock.Now().Add(48 * time.Hour).UnixMilli() // «обновлена в будущем»
	rec := &recorder{}
	s := NewScheduler(SchedulerConfig{
		Groups: func() ([]*model.Group, error) { return []*model.Group{g}, nil },
		Update: rec.update,
		Now:    clock.Now,
	})
	s.Tick(context.Background())
	if got := rec.take(); !sameIDs(got, 1) {
		t.Fatalf("при LastUpdateMs в будущем обновлено %v, ожидалась группа 1", got)
	}
}

func TestSchedulerBackoffAfterFailure(t *testing.T) {
	clock := newFakeClock()
	g := subGroup(clock, 1, 0)
	g.UpdateIntervalMin = i32(240)
	rec := &recorder{err: errors.New("сеть недоступна")}
	s := NewScheduler(SchedulerConfig{
		Groups: func() ([]*model.Group, error) { return []*model.Group{g}, nil },
		Update: rec.update,
		Now:    clock.Now,
	})
	step := func(wait time.Duration, wantCalls int, why string) {
		t.Helper()
		clock.advance(wait)
		s.Tick(context.Background())
		if got := rec.take(); len(got) != wantCalls {
			t.Fatalf("%s: вызовов Update %d, ожидалось %d", why, len(got), wantCalls)
		}
	}
	step(0, 1, "первая попытка")
	step(time.Minute, 0, "через минуту после сбоя — пауза 5 мин")
	step(4*time.Minute, 1, "через 5 минут — вторая попытка")
	step(9*time.Minute, 0, "после второго сбоя пауза 10 мин")
	step(time.Minute, 1, "через 10 минут — третья попытка")

	rec.err = nil
	step(20*time.Minute, 1, "через 20 минут — успех")
	step(time.Hour, 0, "после успеха ждём обычный интервал (4 ч)")
	step(3*time.Hour, 1, "через 4 часа после успеха — пора")
}

func TestSchedulerBusyIsRetriedNextTick(t *testing.T) {
	clock := newFakeClock()
	g := subGroup(clock, 1, 0)
	g.UpdateIntervalMin = i32(60)
	rec := &recorder{err: ErrBusy}
	s := NewScheduler(SchedulerConfig{
		Groups: func() ([]*model.Group, error) { return []*model.Group{g}, nil },
		Update: rec.update,
		Now:    clock.Now,
	})
	s.Tick(context.Background())
	clock.advance(time.Minute)
	s.Tick(context.Background())
	if got := rec.take(); len(got) != 2 {
		t.Fatalf("при «занято» ожидались повторы на каждом просмотре, вызовов %d", len(got))
	}
}

// Деструктивное обновление планировщик не применяет и не долбит провайдера:
// повтор — через обычный интервал.
func TestSchedulerConfirmRequiredWaitsFullInterval(t *testing.T) {
	clock := newFakeClock()
	g := subGroup(clock, 1, 0)
	g.UpdateIntervalMin = i32(60)
	rec := &recorder{err: &ConfirmRequiredError{Reason: "обновление удаляет 3 из 4 профилей (75%)"}}
	var logged []string
	s := NewScheduler(SchedulerConfig{
		Groups: func() ([]*model.Group, error) { return []*model.Group{g}, nil },
		Update: rec.update,
		Now:    clock.Now,
		Log:    func(format string, args ...any) { logged = append(logged, fmt.Sprintf(format, args...)) },
	})
	s.Tick(context.Background())
	clock.advance(30 * time.Minute)
	s.Tick(context.Background())
	if got := rec.take(); len(got) != 1 {
		t.Fatalf("вызовов %d, ожидался 1 (повтор не раньше интервала)", len(got))
	}
	clock.advance(30 * time.Minute)
	s.Tick(context.Background())
	if got := rec.take(); len(got) != 1 {
		t.Fatalf("через интервал вызовов %d, ожидался 1", len(got))
	}
	if len(logged) == 0 || !strings.Contains(logged[0], "удаляет 3 из 4") || !strings.Contains(logged[0], "вручную") {
		t.Fatalf("в журнале нет причины и подсказки: %q", logged)
	}
}

// URL подписки несёт токен пользователя: в журнал он не попадает.
func TestSchedulerLogDoesNotLeakSubscriptionURL(t *testing.T) {
	clock := newFakeClock()
	g := subGroup(clock, 1, 0)
	g.UpdateIntervalMin = i32(60)
	rec := &recorder{err: fmt.Errorf(`подписка: Get "https://panel.example.com/sub/8f3a1c9e7b2d4f60a1b2c3d4e5f60718?t=SECRET": dial tcp 203.0.113.1:443: i/o timeout (после 3 попыток)`)}
	var logged []string
	s := NewScheduler(SchedulerConfig{
		Groups: func() ([]*model.Group, error) { return []*model.Group{g}, nil },
		Update: rec.update,
		Now:    clock.Now,
		Log:    func(format string, args ...any) { logged = append(logged, fmt.Sprintf(format, args...)) },
	})
	s.Tick(context.Background())
	if len(logged) != 1 {
		t.Fatalf("записей в журнале %d, ожидалась 1: %q", len(logged), logged)
	}
	for _, secret := range []string{"8f3a1c9e7b2d4f60", "SECRET", "panel.example.com/sub"} {
		if strings.Contains(logged[0], secret) {
			t.Fatalf("в журнале утёк фрагмент URL %q: %s", secret, logged[0])
		}
	}
	if !strings.Contains(logged[0], "i/o timeout") {
		t.Fatalf("причина сбоя потеряна: %s", logged[0])
	}
}

func TestSchedulerSurvivesPanicInUpdate(t *testing.T) {
	clock := newFakeClock()
	g := subGroup(clock, 1, 0)
	g.UpdateIntervalMin = i32(60)
	calls := 0
	s := NewScheduler(SchedulerConfig{
		Groups: func() ([]*model.Group, error) { return []*model.Group{g}, nil },
		Update: func(context.Context, int64) error { calls++; panic("boom") },
		Now:    clock.Now,
	})
	s.Tick(context.Background()) // не должно упасть
	if calls != 1 {
		t.Fatalf("вызовов %d", calls)
	}
}

// Run: циклы просмотра идут сами и прекращаются по отмене ctx.
func TestSchedulerRunLoopStopsOnCancel(t *testing.T) {
	g := &model.Group{ID: 1, Name: "s", Kind: model.GroupSubscription, SubscriptionURL: "https://example.com/s"}
	g.UpdateIntervalMin = i32(30)
	var ticks atomic.Int32
	called := make(chan struct{}, 8)
	s := NewScheduler(SchedulerConfig{
		Groups:     func() ([]*model.Group, error) { ticks.Add(1); return []*model.Group{g}, nil },
		Update:     func(context.Context, int64) error { called <- struct{}{}; return nil },
		TickEvery:  10 * time.Millisecond,
		StartDelay: -1,
	})
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { s.Run(ctx); close(done) }()

	select {
	case <-called:
	case <-time.After(5 * time.Second):
		t.Fatal("Run не обновил группу")
	}
	for ticks.Load() < 3 {
		select {
		case <-time.After(5 * time.Second):
			t.Fatal("Run не делает повторных просмотров")
		default:
			time.Sleep(5 * time.Millisecond)
		}
	}
	cancel()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("Run не остановился после отмены ctx")
	}
}

// Сквозной сценарий: настоящее хранилище и подписка на httptest. Пора —
// группа обновлена и LastUpdateMs сдвинулся; skip_auto_update не тронута;
// деструктивный ответ ничего не удаляет.
func TestSchedulerEndToEnd(t *testing.T) {
	var hitsOK, hitsSkip, hitsShrink atomic.Int32
	mux := http.NewServeMux()
	mux.HandleFunc("/ok", func(w http.ResponseWriter, r *http.Request) {
		hitsOK.Add(1)
		fmt.Fprint(w, "trojan://p@fresh.example.org:443#fresh\ntrojan://p@keep.example.org:443#keep\n")
	})
	mux.HandleFunc("/skip", func(w http.ResponseWriter, r *http.Request) {
		hitsSkip.Add(1)
		fmt.Fprint(w, "trojan://p@x.example.org:443#x")
	})
	mux.HandleFunc("/shrink", func(w http.ResponseWriter, r *http.Request) {
		hitsShrink.Add(1)
		fmt.Fprint(w, "trojan://only@example.org:443#only") // из 4 остаётся 1: 75 % > 50 %
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	st := newTestStore(t)
	clock := newFakeClock()
	staleMs := clock.Now().Add(-3 * time.Hour).UnixMilli()

	okGroup := newSubGroup(t, st, srv.URL+"/ok")
	skipGroup := newSubGroup(t, st, srv.URL+"/skip")
	shrinkGroup := newSubGroup(t, st, srv.URL+"/shrink")
	for _, g := range []*model.Group{okGroup, skipGroup, shrinkGroup} {
		g.UpdateIntervalMin = i32(60)
		g.LastUpdateMs = staleMs
		if g.ID == skipGroup.ID {
			g.SkipAutoUpdate = true
		}
		if err := st.UpdateGroup(g); err != nil {
			t.Fatal(err)
		}
	}
	addProfile(t, st, okGroup.ID, "keep-old", "keep.example.org", 443, 0)
	addProfile(t, st, okGroup.ID, "gone", "gone.example.org", 443, 0)
	addProfile(t, st, skipGroup.ID, "skip-old", "skip.example.org", 443, 0)
	for i := 0; i < 4; i++ {
		addProfile(t, st, shrinkGroup.ID, fmt.Sprintf("s%d", i), fmt.Sprintf("s%d.example.com", i), 443, 0)
	}

	sub := NewSubscriber(nil, Options{AllowPrivateURL: true})
	var logged []string
	s := NewScheduler(SchedulerConfig{
		Groups: st.ListGroups,
		Update: func(ctx context.Context, id int64) error {
			g, err := st.GetGroup(id)
			if err != nil {
				return err
			}
			_, err = sub.Update(ctx, st, g, false) // без подтверждения
			return err
		},
		Now: clock.Now,
		Log: func(format string, args ...any) { logged = append(logged, fmt.Sprintf(format, args...)) },
	})
	s.Tick(context.Background())

	if hitsOK.Load() != 1 || hitsSkip.Load() != 0 || hitsShrink.Load() != 1 {
		t.Fatalf("запросов к провайдерам: ok=%d skip=%d shrink=%d, ожидалось 1/0/1", hitsOK.Load(), hitsSkip.Load(), hitsShrink.Load())
	}
	// обновлённая группа: состав из ответа, сроки сдвинулись
	list, _ := st.ListProfiles(okGroup.ID)
	if len(list) != 2 {
		t.Fatalf("группа ok: %d профилей, ожидалось 2 (fresh + keep)", len(list))
	}
	g, _ := st.GetGroup(okGroup.ID)
	if g.LastUpdateMs <= staleMs {
		t.Fatalf("LastUpdateMs не сдвинулся: %d", g.LastUpdateMs)
	}
	// группа с skip_auto_update не тронута
	if list, _ := st.ListProfiles(skipGroup.ID); len(list) != 1 || list[0].Name != "skip-old" {
		t.Fatalf("группа skip изменена: %+v", list)
	}
	// деструктивный ответ ничего не удалил
	if list, _ := st.ListProfiles(shrinkGroup.ID); len(list) != 4 {
		t.Fatalf("группа shrink без подтверждения изменилась: %d профилей", len(list))
	}
	foundConfirm := false
	for _, l := range logged {
		if strings.Contains(l, "не применено") {
			foundConfirm = true
		}
	}
	if !foundConfirm {
		t.Fatalf("в журнале нет записи о неприменённом деструктивном обновлении: %q", logged)
	}

	// Пока интервал не прошёл — к провайдерам не ходим.
	clock.advance(10 * time.Minute)
	s.Tick(context.Background())
	if hitsOK.Load() != 1 || hitsShrink.Load() != 1 {
		t.Fatalf("повторные запросы раньше срока: ok=%d shrink=%d", hitsOK.Load(), hitsShrink.Load())
	}
	// Через интервал снова пора: ok обновляется, shrink пробуется и снова не применяется.
	clock.advance(time.Hour)
	s.Tick(context.Background())
	if hitsOK.Load() != 2 || hitsShrink.Load() != 2 {
		t.Fatalf("через интервал запросов ok=%d shrink=%d, ожидалось 2/2", hitsOK.Load(), hitsShrink.Load())
	}
	if list, _ := st.ListProfiles(shrinkGroup.ID); len(list) != 4 {
		t.Fatalf("группа shrink изменилась при повторной попытке: %d профилей", len(list))
	}
}
