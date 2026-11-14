// SPDX-License-Identifier: GPL-3.0-only

package api

// stats_service_test.go — StatsService.Subscribe (T-79): кадры по тегам,
// интервал, финальный кадр.

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"connectrpc.com/connect"

	lavrayv1 "github.com/anatolijlaptev1991-ctrl/lavray-android/core/internal/gen/lavrayv1"
	"github.com/anatolijlaptev1991-ctrl/lavray-android/core/internal/traffic"
)

func (e *trafficEnv) subscribe(ctx context.Context, req *lavrayv1.SubscribeStatsRequest, errOut *error) <-chan *lavrayv1.StatsSnapshot {
	return frames(func() (*connect.ServerStreamForClient[lavrayv1.StatsSnapshot], error) {
		return e.stats.Subscribe(ctx, connect.NewRequest(req))
	}, errOut)
}

// pump непрерывно наращивает счётчики соединений: любой кадр потока видит
// прирост, как при живом трафике. Останавливается вместе с тестом.
func (e *trafficEnv) pump(ids ...string) (stopPump func()) {
	stop := make(chan struct{})
	done := make(chan struct{})
	go func() {
		defer close(done)
		for {
			select {
			case <-stop:
				return
			case <-time.After(15 * time.Millisecond):
				for _, id := range ids {
					e.src.AddBytes(id, 1000, 5000)
				}
			}
		}
	}()
	var once sync.Once
	stopPump = func() { once.Do(func() { close(stop); <-done }) }
	e.t.Cleanup(stopPump)
	return stopPump
}

func tagByName(f *lavrayv1.StatsSnapshot, name string) *lavrayv1.TagStat {
	for _, ts := range f.Tags {
		if ts.Tag == name {
			return ts
		}
	}
	return nil
}

func TestStatsSubscribeFillsTags(t *testing.T) {
	e := newTrafficEnv(t)
	e.connect()
	e.open(idA, "proxy", "a:1", 1)
	e.open(idB, "direct", "b:1", 2)
	e.pump(idA, idB)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var serr error
	ch := e.subscribe(ctx, &lavrayv1.SubscribeStatsRequest{IntervalMs: int32p(250)}, &serr)
	f, ok := next(ch, 3*time.Second)
	if !ok || f.Final {
		t.Fatalf("первый кадр: %+v", f)
	}
	proxy, direct := tagByName(f, "proxy"), tagByName(f, "direct")
	if proxy == nil || direct == nil || proxy.UplinkBps <= 0 || proxy.DownlinkBps <= 0 || direct.DownlinkBps <= 0 {
		t.Fatalf("скорости по тегам: %+v", f.Tags)
	}
	if proxy.DownlinkTotal <= 0 || f.Session == nil || f.Session.GetProfileId() != e.profile.ID ||
		f.Session.StartedAtMs == 0 || f.Session.DownlinkTotal < proxy.DownlinkTotal {
		t.Fatalf("итоги: теги %+v, сессия %+v", f.Tags, f.Session)
	}

	// Отбор тегов: только запрошенные.
	var serr2 error
	only := e.subscribe(ctx, &lavrayv1.SubscribeStatsRequest{IntervalMs: int32p(250), Tags: []string{"direct"}}, &serr2)
	g, ok := next(only, 3*time.Second)
	if !ok || len(g.Tags) != 1 || g.Tags[0].Tag != "direct" {
		t.Fatalf("tags=[direct]: %+v", g)
	}
	// Слишком много тегов — INVALID_ARGUMENT.
	var serr3 error
	many := make([]string, 101)
	for range e.subscribe(ctx, &lavrayv1.SubscribeStatsRequest{Tags: many}, &serr3) {
	}
	if !isCode(serr3, connect.CodeInvalidArgument) {
		t.Errorf("101 тег: %v", serr3)
	}
}

// Без инстанса поток ждёт, а не падает; с подключением кадры начинаются сами.
func TestStatsSubscribeWaitsForInstance(t *testing.T) {
	e := newTrafficEnv(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var serr error
	ch := e.subscribe(ctx, &lavrayv1.SubscribeStatsRequest{IntervalMs: int32p(250)}, &serr)
	if f, ok := next(ch, 600*time.Millisecond); ok || f != nil {
		t.Fatalf("без инстанса пришёл кадр: %+v", f)
	}
	e.open(idA, "proxy", "a:1", 1)
	e.connect()
	e.pump(idA)
	if f, ok := next(ch, 3*time.Second); !ok || f.Final || tagByName(f, "proxy") == nil {
		t.Fatalf("после подключения: %+v", f)
	}
	cancel()
	for range ch {
	}
	if serr != nil && !isCode(serr, connect.CodeCanceled) {
		t.Errorf("отмена потока: %v", serr)
	}
}

func TestStatsIntervalSettingAndOverride(t *testing.T) {
	e := newTrafficEnv(t)
	interval := func(override *int32) (time.Duration, error) {
		return e.srv.statsInterval(&lavrayv1.SubscribeStatsRequest{IntervalMs: override})
	}
	if got, err := interval(nil); err != nil || got != time.Second {
		t.Errorf("по умолчанию: %v %v, ожидалась 1 с", got, err)
	}
	for setting, want := range map[string]time.Duration{
		"700": 700 * time.Millisecond, "500": 500 * time.Millisecond, "100": 500 * time.Millisecond,
		"-5": 500 * time.Millisecond, "5000": 5 * time.Second, "9999": 5 * time.Second, "0": 0,
	} {
		if err := e.st.SetSetting("stats.interval_ms", setting); err != nil {
			t.Fatal(err)
		}
		if got, err := interval(nil); err != nil || got != want {
			t.Errorf("stats.interval_ms=%s: %v %v, ожидалось %v", setting, got, err, want)
		}
		if got, err := interval(int32p(0)); err != nil || got != want {
			t.Errorf("stats.interval_ms=%s, запрос 0 = «из настроек»: %v %v", setting, got, err)
		}
	}
	// Переопределение запросом: 250..10000 и не зависит от настройки (она 0).
	for override, want := range map[int32]time.Duration{250: 250 * time.Millisecond, 3000: 3 * time.Second, 10000: 10 * time.Second} {
		if got, err := interval(&override); err != nil || got != want {
			t.Errorf("запрос %d: %v %v", override, got, err)
		}
	}
	for _, bad := range []int32{1, 249, 10001, -1, 1 << 30} {
		if _, err := interval(&bad); !isCode(err, connect.CodeInvalidArgument) {
			t.Errorf("запрос %d: %v, ожидался INVALID_ARGUMENT", bad, err)
		}
	}
	// Тот же отказ по сети.
	var serr error
	for range e.subscribe(context.Background(), &lavrayv1.SubscribeStatsRequest{IntervalMs: int32p(100)}, &serr) {
	}
	if !isCode(serr, connect.CodeInvalidArgument) {
		t.Errorf("Subscribe interval_ms=100: %v", serr)
	}
}

// Интервал 0 в настройках — периодических кадров нет, только финальный.
func TestStatsZeroIntervalOnlyFinal(t *testing.T) {
	e := newTrafficEnv(t)
	if err := e.st.SetSetting("stats.interval_ms", "0"); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var serr error
	ch := e.subscribe(ctx, &lavrayv1.SubscribeStatsRequest{}, &serr)
	e.connect()
	e.open(idA, "proxy", "a:1", 1)
	e.src.AddBytes(idA, 40, 80)
	if f, ok := next(ch, 700*time.Millisecond); ok || f != nil {
		t.Fatalf("при интервале 0 пришёл кадр до остановки: %+v", f)
	}
	e.disconnect(&lavrayv1.DisconnectRequest{})
	f, ok := next(ch, 3*time.Second)
	if !ok || !f.Final || f.Session == nil || f.Session.UplinkTotal != 40 || f.Session.DownlinkTotal != 80 {
		t.Fatalf("финальный кадр: %+v", f)
	}
	if extra, ok := next(ch, 500*time.Millisecond); ok {
		t.Fatalf("после финального кадра пришёл ещё один: %+v", extra)
	}
}

func TestStatsFinalFrameOnDisconnect(t *testing.T) {
	e := newTrafficEnv(t)
	e.connect()
	e.open(idA, "proxy", "a:1", 1)
	stopPump := e.pump(idA)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var serr error
	ch := e.subscribe(ctx, &lavrayv1.SubscribeStatsRequest{IntervalMs: int32p(250)}, &serr)
	if f, ok := next(ch, 3*time.Second); !ok || f.Final {
		t.Fatalf("обычный кадр: %+v", f)
	}
	// Трафик останавливается до отключения: иначе байты, добавленные после
	// финального кадра, расходятся с итогом источника (флейк CI).
	stopPump()
	e.disconnect(&lavrayv1.DisconnectRequest{})
	var final *lavrayv1.StatsSnapshot
	for i := 0; i < 10 && final == nil; i++ {
		f, ok := next(ch, 3*time.Second)
		if !ok {
			t.Fatal("финальный кадр не пришёл")
		}
		if f.Final {
			final = f
		}
	}
	if final == nil {
		t.Fatal("финального кадра нет")
	}
	up, down := e.src.Total()
	if final.Session == nil || final.Session.UplinkTotal != up || final.Session.DownlinkTotal != down || up == 0 {
		t.Fatalf("итоги финального кадра %+v, источника ↑%d ↓%d", final.Session, up, down)
	}
	if p := tagByName(final, "proxy"); p == nil || p.UplinkBps != 0 || p.DownlinkBps != 0 || p.DownlinkTotal == 0 {
		t.Fatalf("финальный кадр: скорость должна быть нулевой, объём — нет: %+v", final.Tags)
	}
	// Поток остаётся открытым: новое подключение даёт новые кадры без переподписки.
	e.runner.SetTraffic(e.src)
	e.connect()
	for i := 0; ; i++ {
		f, ok := next(ch, 3*time.Second)
		if !ok || i > 20 {
			t.Fatal("после переподключения кадры не возобновились")
		}
		if !f.Final {
			break
		}
	}
}

// Короткие соединения исчезают из списков источника между тактами (sing-box
// очищает список закрытых): общая скорость по тегам всё равно не нулевая.
func TestStatsSpeedSurvivesClearedClosedList(t *testing.T) {
	e := newTrafficEnv(t)
	e.connect()
	stop, done := make(chan struct{}), make(chan struct{})
	go func() {
		defer close(done)
		for i := 0; ; i++ {
			select {
			case <-stop:
				return
			case <-time.After(10 * time.Millisecond):
				id := fmt.Sprintf("%08d-0000-4000-8000-000000000000", i)
				e.src.Open(traffic.ConnInfo{ID: id, Outbound: "proxy", Destination: "x:1", StartMs: int64(i)})
				e.src.AddBytes(id, 1000, 4000)
				e.src.Finish(id, 0)
				e.src.ClearClosed()
			}
		}
	}()
	t.Cleanup(func() { close(stop); <-done })
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var serr error
	ch := e.subscribe(ctx, &lavrayv1.SubscribeStatsRequest{IntervalMs: int32p(250)}, &serr)
	deadline := time.After(5 * time.Second)
	for {
		select {
		case f := <-ch:
			var down float64
			for _, ts := range f.GetTags() {
				down += ts.DownlinkBps
			}
			if down > 0 {
				return
			}
		case <-deadline:
			t.Fatal("скорость осталась нулевой: байты короткоживущих соединений потеряны")
		}
	}
}
