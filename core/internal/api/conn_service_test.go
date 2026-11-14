// SPDX-License-Identifier: GPL-3.0-only

package api

// conn_service_test.go — ConnectionService (T-79): список, поток, закрытие.

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"connectrpc.com/connect"

	lavrayv1 "github.com/anatolijlaptev1991-ctrl/lavray-android/core/internal/gen/lavrayv1"
	"github.com/anatolijlaptev1991-ctrl/lavray-android/core/internal/traffic"
)

const (
	idA = "11111111-1111-4111-8111-111111111111"
	idB = "22222222-2222-4222-8222-222222222222"
	idC = "33333333-3333-4333-8333-333333333333"
	idD = "44444444-4444-4444-8444-444444444444"
)

// short — короткое имя соединения для сравнения порядка: a..d для idA..idD, иначе
// первый символ id.
func short(id string) string {
	switch id {
	case idA:
		return "a"
	case idB:
		return "b"
	case idC:
		return "c"
	case idD:
		return "d"
	}
	return id[:1]
}

func ids(conns []*lavrayv1.Connection) string {
	var out []string
	for _, c := range conns {
		out = append(out, short(c.Id))
	}
	return strings.Join(out, "")
}

// Нет инстанса — пустой список, не ошибка (экран открыт до подключения).
// Неверная страница отвергается и без инстанса.
func TestListConnectionsWithoutInstanceIsEmptyNotError(t *testing.T) {
	e := newTrafficEnv(t)
	got, err := e.list(&lavrayv1.ListConnectionsRequest{})
	if err != nil || len(got) != 0 {
		t.Fatalf("без инстанса: %v, err=%v", got, err)
	}
	for name, page := range map[string]*lavrayv1.Page{
		"limit<0":     {Limit: -1},
		"limit>5000":  {Limit: 5001},
		"offset<0":    {Offset: -1},
		"limit=5000+": {Limit: 6000},
	} {
		if _, err := e.list(&lavrayv1.ListConnectionsRequest{Page: page}); !isCode(err, connect.CodeInvalidArgument) {
			t.Errorf("%s: %v, ожидался INVALID_ARGUMENT", name, err)
		}
	}
	if _, err := e.list(&lavrayv1.ListConnectionsRequest{Page: &lavrayv1.Page{Limit: 5000}}); err != nil {
		t.Errorf("limit=5000 — граница допустима: %v", err)
	}
}

func TestListConnectionsFiltersCaseInsensitive(t *testing.T) {
	e := newTrafficEnv(t)
	e.connect()
	e.open(idA, "proxy", "Example.COM:443", 1)
	e.open(idB, "direct", "other.org:80", 2)
	e.src.Open(traffic.ConnInfo{ID: idC, Outbound: "Proxy-2", Destination: "1.2.3.4:443", Domain: "cdn.EXAMPLE.net", StartMs: 3})

	for name, tc := range map[string]struct {
		req  *lavrayv1.ListConnectionsRequest
		want string
	}{
		"без фильтров":          {&lavrayv1.ListConnectionsRequest{}, "cba"},
		"outbound в верхнем":    {&lavrayv1.ListConnectionsRequest{OutboundFilter: strp("PROXY")}, "ca"},
		"destination в нижнем":  {&lavrayv1.ListConnectionsRequest{DestinationFilter: strp("example")}, "ca"},
		"по имени из сниффинга": {&lavrayv1.ListConnectionsRequest{DestinationFilter: strp("cdn.example.NET")}, "c"},
		"оба фильтра":           {&lavrayv1.ListConnectionsRequest{OutboundFilter: strp("proxy-2"), DestinationFilter: strp("EXAMPLE")}, "c"},
		"ничего не подходит":    {&lavrayv1.ListConnectionsRequest{DestinationFilter: strp("nope")}, ""},
	} {
		got, err := e.list(tc.req)
		if err != nil || ids(got) != tc.want {
			t.Errorf("%s: получено %q (err=%v), ожидалось %q", name, ids(got), err, tc.want)
		}
	}
}

func TestListConnectionsIncludesClosedAndBytes(t *testing.T) {
	e := newTrafficEnv(t)
	e.connect()
	e.src.Open(traffic.ConnInfo{ID: idA, Outbound: "proxy", Destination: "a.example:443", StartMs: 100, Network: "tcp",
		Chain: []string{"sel", "proxy"}, Process: "chrome.exe", Rule: "domain => route(proxy)", Domain: "a.example"})
	e.open(idB, "direct", "b.example:80", 200)
	e.open(idC, "block", "c.example:80", 300)
	e.src.AddBytes(idA, 111, 222)
	e.src.Finish(idB, 5000)
	e.src.Finish(idC, 6000)
	e.open(idD, "proxy", "d.example:443", 400)

	got, err := e.list(&lavrayv1.ListConnectionsRequest{})
	if err != nil {
		t.Fatal(err)
	}
	// Активные сначала (новые выше), затем закрытые (закрывшиеся позже — выше).
	if ids(got) != "da"+"cb" {
		t.Fatalf("порядок %q, ожидался dacb", ids(got))
	}
	a := got[1]
	if a.UploadBytes != 111 || a.DownloadBytes != 222 || a.Network != "tcp" || a.Process == nil || *a.Process != "chrome.exe" ||
		a.GetResolvedDestination() != "a.example" || a.GetRule() == "" || strings.Join(a.Chain, ">") != "sel>proxy" ||
		a.Status != lavrayv1.Connection_STATUS_ACTIVE || a.EndedAtMs != nil {
		t.Fatalf("активное соединение: %+v", a)
	}
	if b := got[3]; b.Status != lavrayv1.Connection_STATUS_CLOSED || b.GetEndedAtMs() != 5000 {
		t.Fatalf("закрытое соединение: %+v", b)
	}
	if c := got[2]; c.Status != lavrayv1.Connection_STATUS_BLOCKED || c.GetEndedAtMs() != 6000 {
		t.Fatalf("заблокированное соединение: %+v", c)
	}
	// Страницы: размер 2 со смещением 1.
	page, err := e.list(&lavrayv1.ListConnectionsRequest{Page: &lavrayv1.Page{Offset: 1, Limit: 2}})
	if err != nil || ids(page) != "ac" {
		t.Fatalf("страница: %q err=%v", ids(page), err)
	}
	if page, _ = e.list(&lavrayv1.ListConnectionsRequest{Page: &lavrayv1.Page{Offset: 10}}); len(page) != 0 {
		t.Fatalf("смещение за концом: %q", ids(page))
	}
}

func (e *trafficEnv) watch(ctx context.Context, req *lavrayv1.WatchConnectionsRequest, errOut *error) <-chan *lavrayv1.ConnectionSnapshot {
	return frames(func() (*connect.ServerStreamForClient[lavrayv1.ConnectionSnapshot], error) {
		return e.conns.WatchConnections(ctx, connect.NewRequest(req))
	}, errOut)
}

func snapIDs(s *lavrayv1.ConnectionSnapshot) string { return ids(s.Active) }

func TestWatchConnectionsSendsFullActiveEveryFrame(t *testing.T) {
	e := newTrafficEnv(t)
	e.connect()
	e.open(idA, "proxy", "a:1", 1)
	e.open(idB, "direct", "b:1", 2)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var werr error
	ch := e.watch(ctx, &lavrayv1.WatchConnectionsRequest{IntervalMs: int32p(250)}, &werr)
	for i := 1; i <= 3; i++ {
		f, ok := next(ch, 3*time.Second)
		if !ok {
			t.Fatalf("кадр %d не пришёл", i)
		}
		if snapIDs(f) != "ba" || f.Truncated || len(f.ClosedIds) != 0 {
			t.Fatalf("кадр %d: active=%q truncated=%v closed=%v", i, snapIDs(f), f.Truncated, f.ClosedIds)
		}
		if got := f.Active[1].UploadBytes; got != int64(100*(i-1)) {
			t.Fatalf("кадр %d: байты %d — каждый кадр несёт текущие счётчики", i, got)
		}
		e.src.AddBytes(idA, 100, 0)
	}
}

func TestWatchClosedIdsExactlyOnce(t *testing.T) {
	e := newTrafficEnv(t)
	e.connect()
	e.open(idA, "proxy", "a:1", 1)
	e.open(idB, "proxy", "b:1", 2)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var werr error
	ch := e.watch(ctx, &lavrayv1.WatchConnectionsRequest{IntervalMs: int32p(250)}, &werr)
	first, _ := next(ch, 3*time.Second)
	if first == nil || snapIDs(first) != "ba" {
		t.Fatalf("первый кадр: %+v", first)
	}
	e.src.Finish(idA, 0)
	var closedSeen int
	for i := 0; i < 4; i++ {
		f, ok := next(ch, 3*time.Second)
		if !ok {
			t.Fatal("кадр не пришёл")
		}
		closedSeen += len(f.ClosedIds)
		for _, id := range f.ClosedIds {
			if id != idA {
				t.Fatalf("закрытым назван %s", id)
			}
		}
		if snapIDs(f) != "b" {
			t.Fatalf("кадр %d: активные %q", i, snapIDs(f))
		}
	}
	if closedSeen != 1 {
		t.Fatalf("закрытие объявлено %d раз, ожидалось 1", closedSeen)
	}
}

func TestWatchTruncated(t *testing.T) {
	e := newTrafficEnv(t)
	e.connect()
	for i := 1; i <= 5; i++ {
		e.open(fmt.Sprintf("%d%d%d%d%d%d%d%d-0000-4000-8000-000000000000", i, i, i, i, i, i, i, i), "proxy", "x:1", int64(i))
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var werr error
	ch := e.watch(ctx, &lavrayv1.WatchConnectionsRequest{IntervalMs: int32p(250), ActivePageLimit: int32p(2)}, &werr)
	f, _ := next(ch, 3*time.Second)
	if f == nil || !f.Truncated || snapIDs(f) != "54" {
		t.Fatalf("limit=2 из 5: %+v — ожидались два новейших и truncated", f)
	}
	// Усечённое соединение не «закрыто»: оно просто не вошло в кадр.
	e.src.Finish("33333333-0000-4000-8000-000000000000", 0)
	f, _ = next(ch, 3*time.Second)
	if f == nil || snapIDs(f) != "54" || !f.Truncated {
		t.Fatalf("после закрытия третьего: %+v", f)
	}
	// Граница: ровно по лимиту — не усечено.
	var werr2 error
	ch2 := e.watch(ctx, &lavrayv1.WatchConnectionsRequest{IntervalMs: int32p(250), ActivePageLimit: int32p(4)}, &werr2)
	if f, _ = next(ch2, 3*time.Second); f == nil || f.Truncated || len(f.Active) != 4 {
		t.Fatalf("limit=4 при 4 активных: %+v", f)
	}
}

func TestWatchIntervalClamped(t *testing.T) {
	for in, want := range map[int32]time.Duration{
		0: time.Second, 1: 250 * time.Millisecond, 249: 250 * time.Millisecond, 250: 250 * time.Millisecond,
		1000: time.Second, 10000: 10 * time.Second, 10001: 10 * time.Second, 1 << 30: 10 * time.Second,
	} {
		got, err := watchInterval(&in)
		if err != nil || got != want {
			t.Errorf("interval_ms=%d: %v (err=%v), ожидалось %v", in, got, err, want)
		}
	}
	if got, err := watchInterval(nil); err != nil || got != time.Second {
		t.Errorf("не задан: %v %v", got, err)
	}
	neg := int32(-1)
	if _, err := watchInterval(&neg); !isCode(err, connect.CodeInvalidArgument) {
		t.Errorf("-1: %v", err)
	}

	e := newTrafficEnv(t)
	for name, req := range map[string]*lavrayv1.WatchConnectionsRequest{
		"interval<0": {IntervalMs: int32p(-5)},
		"limit<0":    {ActivePageLimit: int32p(-1)},
		"limit>5000": {ActivePageLimit: int32p(5001)},
	} {
		var werr error
		ch := e.watch(context.Background(), req, &werr)
		for range ch {
		}
		if !isCode(werr, connect.CodeInvalidArgument) {
			t.Errorf("%s: %v, ожидался INVALID_ARGUMENT", name, werr)
		}
	}
	// Слишком малый интервал поднимается до 250 мс: кадры не чаще.
	e.connect()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var werr error
	ch := e.watch(ctx, &lavrayv1.WatchConnectionsRequest{IntervalMs: int32p(1)}, &werr)
	next(ch, 3*time.Second)
	start := time.Now()
	next(ch, 3*time.Second)
	if gap := time.Since(start); gap < 200*time.Millisecond {
		t.Errorf("кадры идут через %v при interval_ms=1: нижняя граница 250 мс не держится", gap)
	}
}

// Перезапуск инстанса: поток живёт, закрытые старого инстанса объявляются один
// раз, подписка переходит на новый источник.
func TestWatchAcrossInstanceRestart(t *testing.T) {
	e := newTrafficEnv(t)
	e.connect()
	e.open(idA, "proxy", "a:1", 1)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var werr error
	ch := e.watch(ctx, &lavrayv1.WatchConnectionsRequest{IntervalMs: int32p(250)}, &werr)
	if f, _ := next(ch, 3*time.Second); f == nil || snapIDs(f) != "a" {
		t.Fatalf("до остановки: %+v", f)
	}

	e.disconnect(&lavrayv1.DisconnectRequest{})
	var closed []string
	deadline := time.Now().Add(5 * time.Second)
	for len(closed) == 0 && time.Now().Before(deadline) {
		f, ok := next(ch, 3*time.Second)
		if !ok {
			t.Fatal("поток оборвался при остановке инстанса")
		}
		if len(f.Active) != 0 {
			t.Fatalf("после остановки активных быть не должно: %q", snapIDs(f))
		}
		closed = append(closed, f.ClosedIds...)
	}
	if len(closed) != 1 || closed[0] != idA {
		t.Fatalf("закрытые после остановки: %v", closed)
	}

	// Новый запуск — новый источник (у настоящего раннера так и есть).
	second := traffic.NewFakeSource()
	second.Open(traffic.ConnInfo{ID: idB, Outbound: "direct", Destination: "b:1", StartMs: 9})
	e.runner.SetTraffic(second)
	e.connect()
	for i := 0; ; i++ {
		f, ok := next(ch, 3*time.Second)
		if !ok || i > 20 {
			t.Fatal("после перезапуска кадр с новым соединением не пришёл")
		}
		if snapIDs(f) == "b" && len(f.ClosedIds) == 0 {
			break
		}
	}
}

func TestWatchCancelUnsubscribes(t *testing.T) {
	e := newTrafficEnv(t)
	e.connect()
	ctx, cancel := context.WithCancel(context.Background())
	var werr error
	ch := e.watch(ctx, &lavrayv1.WatchConnectionsRequest{IntervalMs: int32p(250)}, &werr)
	next(ch, 3*time.Second)
	// Поток берёт список на каждом кадре и подписок на источник не держит:
	// утечь после отмены нечему.
	if n := e.src.Subscribers(); n != 0 {
		t.Fatalf("WatchConnections держит %d подписок на источник", n)
	}
	cancel()
	for range ch { // поток закрылся
	}
}

func TestCloseConnectionUnknownNotFound(t *testing.T) {
	e := newTrafficEnv(t)
	closeConn := func(id string) error {
		_, err := e.conns.CloseConnection(context.Background(), connect.NewRequest(&lavrayv1.CloseConnectionRequest{Id: id}))
		return err
	}
	// Мусор вместо id — INVALID_ARGUMENT, даже когда инстанса нет.
	for _, bad := range []string{"", "   ", "not-a-uuid", "1111", idA + "x", "../../etc/passwd"} {
		if err := closeConn(bad); !isCode(err, connect.CodeInvalidArgument) {
			t.Errorf("id %q: %v, ожидался INVALID_ARGUMENT", bad, err)
		}
	}
	if err := closeConn(idA); !isCode(err, connect.CodeNotFound) {
		t.Errorf("нет инстанса: %v, ожидался NOT_FOUND", err)
	}
	e.connect()
	if err := closeConn(idA); !isCode(err, connect.CodeNotFound) {
		t.Errorf("неизвестный id: %v, ожидался NOT_FOUND", err)
	}
	e.open(idA, "proxy", "a:1", 1)
	if err := closeConn(idA); err != nil {
		t.Fatalf("известный id: %v", err)
	}
	if err := closeConn(idA); !isCode(err, connect.CodeNotFound) {
		t.Errorf("уже закрыто: %v, ожидался NOT_FOUND", err)
	}
	if calls := e.src.CloseCalls(); len(calls) != 3 || calls[0] != idA {
		t.Errorf("источнику переданы вызовы %v", calls)
	}
}
