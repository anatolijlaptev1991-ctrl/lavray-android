// SPDX-License-Identifier: GPL-3.0-only

package api

// traffic_accounting_test.go — учёт трафика сессии, Disconnect.final_stats и
// GetTrafficSummary (T-79).

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"connectrpc.com/connect"

	lavrayv1 "github.com/anatolijlaptev1991-ctrl/lavray-android/core/internal/gen/lavrayv1"
	"github.com/anatolijlaptev1991-ctrl/lavray-android/core/internal/model"
	"github.com/anatolijlaptev1991-ctrl/lavray-android/core/internal/traffic"
)

func (c *fakeAcctClock) nowMs() int64 {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now.UnixMilli()
}

func TestDisconnectReturnsFinalStats(t *testing.T) {
	e := newTrafficEnv(t)
	// Без подключения Disconnect не ошибка и итогов не несёт.
	if res := e.disconnect(&lavrayv1.DisconnectRequest{}); res.FinalStats != nil {
		t.Fatalf("без подключения: %+v", res.FinalStats)
	}
	e.connect()
	e.open(idA, "proxy", "a:1", 1)
	e.src.AddBytes(idA, 1234, 5678)
	res := e.disconnect(&lavrayv1.DisconnectRequest{})
	fs := res.FinalStats
	if fs == nil || !fs.Final || fs.Session == nil || fs.Session.UplinkTotal != 1234 || fs.Session.DownlinkTotal != 5678 ||
		fs.Session.GetProfileId() != e.profile.ID || fs.Session.StartedAtMs == 0 || fs.TsMs == 0 {
		t.Fatalf("final_stats: %+v", fs)
	}
	// Итог уже в базе: профиль и сессия.
	if got := e.profileTraffic(); got.UplinkBytes != 1234 || got.DownlinkBytes != 5678 {
		t.Fatalf("счётчики профиля: %+v", got)
	}
	sessions := e.sessions()
	if len(sessions) != 1 || sessions[0].EndedAtMs == nil || sessions[0].UplinkBytes != 1234 || sessions[0].DownlinkBytes != 5678 ||
		sessions[0].ProfileID == nil || *sessions[0].ProfileID != e.profile.ID || sessions[0].GroupID == nil || *sessions[0].GroupID != 1 {
		t.Fatalf("сессия: %+v", sessions)
	}
	// collect_stats = false — быстрая остановка без итогов, но учёт всё равно закрыт.
	e.runner.SetTraffic(traffic.NewFakeSource())
	e.connect()
	no := false
	if res := e.disconnect(&lavrayv1.DisconnectRequest{CollectStats: &no}); res.FinalStats != nil {
		t.Fatalf("collect_stats=false: %+v", res.FinalStats)
	}
	if sessions = e.sessions(); len(sessions) != 2 || sessions[1].EndedAtMs == nil {
		t.Fatalf("вторая сессия не закрыта: %+v", sessions)
	}
}

func TestTrafficFlushEvery30s(t *testing.T) {
	e := newTrafficEnv(t)
	e.connect()
	eventually(t, "таймер учёта создан", func() bool { return len(e.clock.requested()) == 1 })
	if got := e.clock.requested()[0]; got != 30*time.Second {
		t.Fatalf("период сброса %v, ожидалось 30 с", got)
	}
	e.open(idA, "proxy", "a:1", 1)
	e.src.AddBytes(idA, 1000, 4000)
	// До срабатывания таймера в базе ничего нет (читаем минуя GetTrafficTotals,
	// который сбрасывает сам).
	if got := e.profileTraffic(); got != (model.TrafficCounters{}) {
		t.Fatalf("до таймера: %+v", got)
	}
	e.clock.fire(30 * time.Second)
	eventually(t, "первый сброс", func() bool { return e.profileTraffic().UplinkBytes == 1000 })
	if got := e.profileTraffic(); got.DownlinkBytes != 4000 {
		t.Fatalf("после первого сброса: %+v", got)
	}
	s := e.sessions()[0]
	if s.UplinkBytes != 1000 || s.DownlinkBytes != 4000 || s.UpdatedAtMs != e.clock.nowMs() || s.EndedAtMs != nil {
		t.Fatalf("сессия после сброса: %+v (часы %d)", s, e.clock.nowMs())
	}
	// Второй период: в профиль уходит прибавка, а не итог целиком.
	e.src.AddBytes(idA, 500, 700)
	e.clock.fire(30 * time.Second)
	eventually(t, "второй сброс", func() bool { return e.profileTraffic().UplinkBytes == 1500 })
	if got := e.profileTraffic(); got.DownlinkBytes != 4700 {
		t.Fatalf("после второго сброса: %+v", got)
	}
	// Тишина: сброс без новых байт ничего не меняет.
	e.clock.fire(30 * time.Second)
	eventually(t, "третий сброс", func() bool { return e.sessions()[0].UpdatedAtMs == e.clock.nowMs() })
	if got := e.profileTraffic(); got.UplinkBytes != 1500 || got.DownlinkBytes != 4700 {
		t.Fatalf("после тихого периода: %+v", got)
	}
	e.disconnect(&lavrayv1.DisconnectRequest{})
	if got := e.profileTraffic(); got.UplinkBytes != 1500 || got.DownlinkBytes != 4700 {
		t.Fatalf("итоговый сброс удвоил учтённое: %+v", got)
	}
}

// Падение инстанса: остановка не удалась, счётчики и конец сессии записаны.
func TestTrafficFlushOnCrash(t *testing.T) {
	t.Run("падение вне Disconnect", func(t *testing.T) {
		e := newTrafficEnv(t)
		e.connect()
		e.open(idA, "proxy", "a:1", 1)
		e.src.AddBytes(idA, 700, 900)
		e.runner.StopErr = errors.New("boom")
		if err := e.srv.inst.Stop(context.Background()); err == nil {
			t.Fatal("остановка должна была упасть")
		}
		eventually(t, "сессия закрыта по падению", func() bool {
			s := e.sessions()
			return len(s) == 1 && s[0].EndedAtMs != nil
		})
		if got := e.profileTraffic(); got.UplinkBytes != 700 || got.DownlinkBytes != 900 {
			t.Fatalf("счётчики после падения: %+v", got)
		}
	})
	t.Run("ошибка Disconnect", func(t *testing.T) {
		e := newTrafficEnv(t)
		e.connect()
		e.open(idA, "proxy", "a:1", 1)
		e.src.AddBytes(idA, 70, 90)
		e.runner.StopErr = errors.New("boom")
		_, err := e.core.Disconnect(context.Background(), connect.NewRequest(&lavrayv1.DisconnectRequest{}))
		if err == nil {
			t.Fatal("Disconnect должен вернуть ошибку остановки")
		}
		if got := e.profileTraffic(); got.UplinkBytes != 70 || got.DownlinkBytes != 90 {
			t.Fatalf("счётчики при ошибке остановки: %+v", got)
		}
		if s := e.sessions(); len(s) != 1 || s[0].EndedAtMs == nil {
			t.Fatalf("сессия: %+v", s)
		}
	})
}

// Перезапуск профиля — две сессии, первая закрыта, байты не смешаны.
func TestRestartStartsNewSession(t *testing.T) {
	e := newTrafficEnv(t)
	e.connect()
	e.open(idA, "proxy", "a:1", 1)
	e.src.AddBytes(idA, 10, 20)
	second := traffic.NewFakeSource()
	e.runner.SetTraffic(second)
	if _, err := e.core.RestartProfile(context.Background(), connect.NewRequest(&lavrayv1.RestartProfileRequest{})); err != nil {
		t.Fatalf("RestartProfile: %v", err)
	}
	second.Open(traffic.ConnInfo{ID: idB, Outbound: "proxy", Destination: "b:1"})
	second.AddBytes(idB, 5, 6)
	e.disconnect(&lavrayv1.DisconnectRequest{})
	s := e.sessions()
	if len(s) != 2 || s[0].EndedAtMs == nil || s[0].UplinkBytes != 10 || s[1].EndedAtMs == nil || s[1].UplinkBytes != 5 {
		t.Fatalf("сессии: %+v", s)
	}
	if got := e.profileTraffic(); got.UplinkBytes != 15 || got.DownlinkBytes != 26 {
		t.Fatalf("счётчики профиля: %+v", got)
	}
}

func TestGetTrafficTotalsReflectsAccounting(t *testing.T) {
	e := newTrafficEnv(t)
	totals := func() *lavrayv1.TrafficCounters {
		t.Helper()
		res, err := e.stats.GetTrafficTotals(context.Background(), connect.NewRequest(&lavrayv1.GetTrafficTotalsRequest{ProfileIds: []int64{e.profile.ID}}))
		if err != nil {
			t.Fatal(err)
		}
		return res.Msg.Totals[e.profile.ID]
	}
	e.connect()
	e.open(idA, "proxy", "a:1", 1)
	e.src.AddBytes(idA, 300, 900)
	// Чтение не отстаёт от живого трафика на период таймера.
	if got := totals(); got.UplinkBytes != 300 || got.DownlinkBytes != 900 {
		t.Fatalf("во время сессии: %+v", got)
	}
	e.src.AddBytes(idA, 100, 100)
	e.disconnect(&lavrayv1.DisconnectRequest{})
	if got := totals(); got.UplinkBytes != 400 || got.DownlinkBytes != 1000 {
		t.Fatalf("после сессии: %+v", got)
	}
	// Вторая сессия копится поверх первой.
	second := traffic.NewFakeSource()
	e.runner.SetTraffic(second)
	e.connect()
	second.Open(traffic.ConnInfo{ID: idB, Outbound: "proxy", Destination: "b:1"})
	second.AddBytes(idB, 50, 60)
	e.disconnect(&lavrayv1.DisconnectRequest{})
	if got := totals(); got.UplinkBytes != 450 || got.DownlinkBytes != 1060 {
		t.Fatalf("после второй сессии: %+v", got)
	}
}

func TestGetTrafficSummaryByGroup(t *testing.T) {
	e := newTrafficEnv(t)
	if err := e.st.CreateGroup(&model.Group{ID: 2, Name: "G2", Kind: model.GroupBasic}); err != nil {
		t.Fatal(err)
	}
	if err := e.st.CreateGroup(&model.Group{ID: 3, Name: "G3 пустая", Kind: model.GroupBasic}); err != nil {
		t.Fatal(err)
	}
	p2 := &model.Profile{GroupID: 2, Type: model.TypeTrojan, Name: "p2", Address: "example.net", Port: 443,
		Detail: &model.TrojanVlessDetail{Password: "secret"}}
	if err := e.st.CreateProfile(p2); err != nil {
		t.Fatal(err)
	}
	session := func(profileID, up, down int64) {
		t.Helper()
		src := traffic.NewFakeSource()
		src.Open(traffic.ConnInfo{ID: idA, Outbound: "proxy", Destination: "a:1"})
		src.AddBytes(idA, up, down)
		e.runner.SetTraffic(src)
		if _, err := e.core.Connect(context.Background(), connect.NewRequest(&lavrayv1.ConnectRequest{ProfileId: &profileID})); err != nil {
			t.Fatal(err)
		}
		e.disconnect(&lavrayv1.DisconnectRequest{})
		time.Sleep(3 * time.Millisecond) // начала сессий различимы по миллисекундам
	}
	session(e.profile.ID, 100, 200)
	session(p2.ID, 10, 20)
	session(e.profile.ID, 300, 400)

	summary := func(req *lavrayv1.GetTrafficSummaryRequest) ([]*lavrayv1.GroupTrafficSummary, error) {
		res, err := e.stats.GetTrafficSummary(context.Background(), connect.NewRequest(req))
		if err != nil {
			return nil, err
		}
		return res.Msg.Groups, nil
	}
	all, err := summary(&lavrayv1.GetTrafficSummaryRequest{})
	if err != nil || len(all) != 2 {
		t.Fatalf("все группы: %v err=%v", all, err)
	}
	if g := all[0]; g.GroupId != 1 || g.UplinkBytes != 400 || g.DownlinkBytes != 600 || g.SessionCount != 2 ||
		g.GetFirstSessionMs() == 0 || g.GetLastSessionMs() <= g.GetFirstSessionMs() {
		t.Fatalf("группа 1: %+v", g)
	}
	if g := all[1]; g.GroupId != 2 || g.UplinkBytes != 10 || g.DownlinkBytes != 20 || g.SessionCount != 1 {
		t.Fatalf("группа 2: %+v", g)
	}
	one, err := summary(&lavrayv1.GetTrafficSummaryRequest{GroupId: int64p(2)})
	if err != nil || len(one) != 1 || one[0].GroupId != 2 || one[0].SessionCount != 1 {
		t.Fatalf("group_id=2: %v err=%v", one, err)
	}
	// since_ms включительно: начало последней сессии берёт её одну.
	last := all[0].GetLastSessionMs()
	recent, _ := summary(&lavrayv1.GetTrafficSummaryRequest{SinceMs: &last})
	if len(recent) != 1 || recent[0].GroupId != 1 || recent[0].SessionCount != 1 || recent[0].UplinkBytes != 300 {
		t.Fatalf("since_ms = начало последней сессии: %v", recent)
	}
	future := last + 1
	if none, err := summary(&lavrayv1.GetTrafficSummaryRequest{SinceMs: &future}); err != nil || len(none) != 0 {
		t.Fatalf("since_ms позже всех сессий: %v err=%v", none, err)
	}
	// Группа без сессий — одна нулевая запись.
	empty, err := summary(&lavrayv1.GetTrafficSummaryRequest{GroupId: int64p(3)})
	if err != nil || len(empty) != 1 || empty[0].GroupId != 3 || empty[0].SessionCount != 0 || empty[0].UplinkBytes != 0 {
		t.Fatalf("группа без сессий: %v err=%v", empty, err)
	}
	// Ошибки.
	if _, err := summary(&lavrayv1.GetTrafficSummaryRequest{GroupId: int64p(999)}); !isCode(err, connect.CodeNotFound) {
		t.Errorf("нет группы: %v, ожидался NOT_FOUND", err)
	}
	neg := int64(-1)
	if _, err := summary(&lavrayv1.GetTrafficSummaryRequest{SinceMs: &neg}); !isCode(err, connect.CodeInvalidArgument) {
		t.Errorf("since_ms < 0: %v, ожидался INVALID_ARGUMENT", err)
	}
}

// Адреса назначения — приватные данные: ни список, ни поток, ни закрытие, ни
// учёт не пишут их в журнал ядра.
func TestDestinationsNeverLogged(t *testing.T) {
	e := newTrafficEnv(t)
	const secret = "secret-dest-7f3a.example.org"
	e.connect()
	e.src.Open(traffic.ConnInfo{ID: idA, Outbound: "proxy", Destination: secret + ":443", Domain: secret, Process: "private-app.exe", StartMs: 1})
	e.src.AddBytes(idA, 10, 10)
	if _, err := e.list(&lavrayv1.ListConnectionsRequest{DestinationFilter: strp(secret)}); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	var werr, serr error
	w := e.watch(ctx, &lavrayv1.WatchConnectionsRequest{IntervalMs: int32p(250)}, &werr)
	s := e.subscribe(ctx, &lavrayv1.SubscribeStatsRequest{IntervalMs: int32p(250)}, &serr)
	next(w, 3*time.Second)
	next(s, 3*time.Second)
	if _, err := e.conns.CloseConnection(ctx, connect.NewRequest(&lavrayv1.CloseConnectionRequest{Id: idA})); err != nil {
		t.Fatal(err)
	}
	e.clock.fire(30 * time.Second)
	cancel()
	e.disconnect(&lavrayv1.DisconnectRequest{})
	for _, l := range e.srv.logs.Tail(2000) {
		if strings.Contains(l.Message, secret) || strings.Contains(l.Message, "private-app") {
			t.Fatalf("в журнале ядра адрес или процесс: %q", l.Message)
		}
	}
}
