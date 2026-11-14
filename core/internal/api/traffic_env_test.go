// SPDX-License-Identifier: GPL-3.0-only

package api

// traffic_env_test.go — стенд тестов соединений и статистики (T-79): настоящий
// сервер на временном хранилище, FakeRunner с подставным источником
// traffic.FakeSource и подставные часы учёта. Сеть не используется.

import (
	"context"
	"errors"
	"net/http"
	"sync"
	"testing"
	"time"

	"connectrpc.com/connect"

	lavrayv1 "github.com/anatolijlaptev1991-ctrl/lavray-android/core/internal/gen/lavrayv1"
	"github.com/anatolijlaptev1991-ctrl/lavray-android/core/internal/gen/lavrayv1connect"
	"github.com/anatolijlaptev1991-ctrl/lavray-android/core/internal/inst"
	"github.com/anatolijlaptev1991-ctrl/lavray-android/core/internal/model"
	"github.com/anatolijlaptev1991-ctrl/lavray-android/core/internal/store"
	"github.com/anatolijlaptev1991-ctrl/lavray-android/core/internal/traffic"
)

// fakeAcctClock — часы учёта: время и «таймер» в руках теста.
type fakeAcctClock struct {
	mu        sync.Mutex
	now       time.Time
	ticks     chan time.Time
	intervals []time.Duration
}

func newFakeAcctClock() *fakeAcctClock {
	return &fakeAcctClock{now: time.UnixMilli(1_700_000_000_000), ticks: make(chan time.Time, 8)}
}

func (c *fakeAcctClock) clock() acctClock {
	return acctClock{
		now: func() time.Time {
			c.mu.Lock()
			defer c.mu.Unlock()
			return c.now
		},
		ticker: func(d time.Duration) (<-chan time.Time, func()) {
			c.mu.Lock()
			c.intervals = append(c.intervals, d)
			c.mu.Unlock()
			return c.ticks, func() {}
		},
	}
}

// fire «проходит» d времени и подаёт сигнал таймера.
func (c *fakeAcctClock) fire(d time.Duration) {
	c.mu.Lock()
	c.now = c.now.Add(d)
	now := c.now
	c.mu.Unlock()
	c.ticks <- now
}

func (c *fakeAcctClock) requested() []time.Duration {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]time.Duration(nil), c.intervals...)
}

type trafficEnv struct {
	t       *testing.T
	srv     *Server
	st      *store.Store
	runner  *inst.FakeRunner
	src     *traffic.FakeSource
	clock   *fakeAcctClock
	core    lavrayv1connect.CoreServiceClient
	conns   lavrayv1connect.ConnectionServiceClient
	stats   lavrayv1connect.StatsServiceClient
	profile *model.Profile
	url     string
	http    *http.Client
}

func newTrafficEnv(t *testing.T) *trafficEnv {
	t.Helper()
	runner := &inst.FakeRunner{}
	src := traffic.NewFakeSource()
	runner.SetTraffic(src)
	clock := newFakeAcctClock()
	var srv *Server
	st, ts := newE2EServer(t, Deps{Runner: runner}, func(s *Server) {
		srv = s
		s.acct.clock = clock.clock()
	})
	p := &model.Profile{GroupID: 1, Type: model.TypeTrojan, Name: "p", Address: "example.org", Port: 443,
		Detail: &model.TrojanVlessDetail{Password: "secret"}}
	if err := st.CreateProfile(p); err != nil {
		t.Fatal(err)
	}
	hc := ts.Client()
	return &trafficEnv{
		t: t, srv: srv, st: st, runner: runner, src: src, clock: clock, profile: p, url: ts.URL, http: hc,
		core:  lavrayv1connect.NewCoreServiceClient(hc, ts.URL),
		conns: lavrayv1connect.NewConnectionServiceClient(hc, ts.URL),
		stats: lavrayv1connect.NewStatsServiceClient(hc, ts.URL),
	}
}

func (e *trafficEnv) connect() {
	e.t.Helper()
	_, err := e.core.Connect(context.Background(), connect.NewRequest(&lavrayv1.ConnectRequest{ProfileId: &e.profile.ID}))
	if err != nil {
		e.t.Fatalf("Connect: %v", err)
	}
}

func (e *trafficEnv) disconnect(req *lavrayv1.DisconnectRequest) *lavrayv1.DisconnectResponse {
	e.t.Helper()
	res, err := e.core.Disconnect(context.Background(), connect.NewRequest(req))
	if err != nil {
		e.t.Fatalf("Disconnect: %v", err)
	}
	return res.Msg
}

// open добавляет в источник активное соединение.
func (e *trafficEnv) open(id, outbound, dest string, startMs int64) {
	e.src.Open(traffic.ConnInfo{ID: id, Outbound: outbound, Destination: dest, StartMs: startMs, Network: "tcp", Inbound: "mixed-in"})
}

func (e *trafficEnv) list(req *lavrayv1.ListConnectionsRequest) ([]*lavrayv1.Connection, error) {
	res, err := e.conns.ListConnections(context.Background(), connect.NewRequest(req))
	if err != nil {
		return nil, err
	}
	return res.Msg.Connections, nil
}

// profileTraffic — счётчики профиля из базы.
func (e *trafficEnv) profileTraffic() model.TrafficCounters {
	e.t.Helper()
	p, err := e.st.GetProfile(e.profile.ID)
	if err != nil {
		e.t.Fatal(err)
	}
	return p.Traffic
}

// sessions — все сессии подключения (для проверок учёта).
func (e *trafficEnv) sessions() []*store.Session {
	e.t.Helper()
	rows, err := e.st.DB().Query(`SELECT id FROM sessions ORDER BY id`)
	if err != nil {
		e.t.Fatal(err)
	}
	var ids []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			e.t.Fatal(err)
		}
		ids = append(ids, id)
	}
	_ = rows.Close()
	var out []*store.Session
	for _, id := range ids {
		s, err := e.st.GetSession(id)
		if err != nil {
			e.t.Fatal(err)
		}
		out = append(out, s)
	}
	return out
}

func eventually(t *testing.T, what string, cond func() bool) {
	t.Helper()
	waitUntil(t, what, cond)
}

// frames читает серверный поток в канал; канал закрывается вместе с потоком,
// ошибка потока кладётся в *errOut.
func frames[T any](open func() (*connect.ServerStreamForClient[T], error), errOut *error) <-chan *T {
	out := make(chan *T, 64)
	go func() {
		defer close(out)
		stream, err := open()
		if err != nil {
			*errOut = err
			return
		}
		defer stream.Close()
		for stream.Receive() {
			out <- stream.Msg()
		}
		*errOut = stream.Err()
	}()
	return out
}

// next ждёт следующий кадр; false — кадра нет за d.
func next[T any](ch <-chan *T, d time.Duration) (*T, bool) {
	select {
	case m, ok := <-ch:
		return m, ok
	case <-time.After(d):
		return nil, false
	}
}

func isCode(err error, code connect.Code) bool {
	var ce *connect.Error
	return errors.As(err, &ce) && ce.Code() == code
}
