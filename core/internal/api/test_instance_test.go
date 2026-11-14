// SPDX-License-Identifier: GPL-3.0-only

package api

// test_instance_test.go — URL- и полный тест (T-84) на подставных временных
// инстансах: параметры, параллелизм, закрытие каждого инстанса, отмена,
// сохранение отчёта. Настоящий sing-box — в test_instance_sbrun_test.go.

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"connectrpc.com/connect"
	lavrayv1 "github.com/anatolijlaptev1991-ctrl/lavray-android/core/internal/gen/lavrayv1"
	"github.com/anatolijlaptev1991-ctrl/lavray-android/core/internal/inst"
	"github.com/anatolijlaptev1991-ctrl/lavray-android/core/internal/model"
)

// fakeDialer — «временный инстанс»: DialContext идёт напрямую к цели (как
// туннель без потерь), ListenPacket — локальный UDP. closed считает Close.
type fakeDialer struct {
	dial   func(ctx context.Context, network, address string) (net.Conn, error)
	listen func(ctx context.Context, destination string) (net.PacketConn, error)
	onShut func()
	once   sync.Once
}

func (f *fakeDialer) DialContext(ctx context.Context, network, address string) (net.Conn, error) {
	if f.dial != nil {
		return f.dial(ctx, network, address)
	}
	return (&net.Dialer{}).DialContext(ctx, network, address)
}

func (f *fakeDialer) ListenPacket(ctx context.Context, destination string) (net.PacketConn, error) {
	if f.listen != nil {
		return f.listen(ctx, destination)
	}
	return newLoopbackPacketConn()
}

// loopbackPacketConn принимает любой net.Addr с "host:port" в String(), как
// пакетные соединения sing-box (обычный UDPConn такой адрес отвергает).
type loopbackPacketConn struct{ net.PacketConn }

func newLoopbackPacketConn() (net.PacketConn, error) {
	pc, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		return nil, err
	}
	return loopbackPacketConn{pc}, nil
}

func (c loopbackPacketConn) WriteTo(b []byte, addr net.Addr) (int, error) {
	to, err := net.ResolveUDPAddr("udp", addr.String())
	if err != nil {
		return 0, err
	}
	return c.PacketConn.WriteTo(b, to)
}

func (f *fakeDialer) Close() error {
	f.once.Do(func() {
		if f.onShut != nil {
			f.onShut()
		}
	})
	return nil
}

// fakeFactory считает созданные и закрытые инстансы и пик одновременных.
type fakeFactory struct {
	created, closed atomic.Int32
	active, peak    atomic.Int32
	configs         chan []byte
	make            func() *fakeDialer
}

func newFakeFactory(mk func() *fakeDialer) *fakeFactory {
	return &fakeFactory{make: mk, configs: make(chan []byte, 64)}
}

func (ff *fakeFactory) factory() inst.TestInstanceFactory {
	return func(_ context.Context, cfg []byte) (inst.TestDialer, error) {
		ff.created.Add(1)
		now := ff.active.Add(1)
		for {
			p := ff.peak.Load()
			if now <= p || ff.peak.CompareAndSwap(p, now) {
				break
			}
		}
		select {
		case ff.configs <- cfg:
		default:
		}
		d := ff.make()
		d.onShut = func() {
			ff.closed.Add(1)
			ff.active.Add(-1)
		}
		return d, nil
	}
}

// runTest гоняет TestLatency и возвращает все кадры и итоговую ошибку стрима.
func runTest(t *testing.T, e *stopEnv, req *lavrayv1.TestLatencyRequest) ([]*lavrayv1.LatencyResult, error) {
	t.Helper()
	stream, err := e.profile.TestLatency(context.Background(), connect.NewRequest(req))
	if err != nil {
		return nil, err
	}
	var frames []*lavrayv1.LatencyResult
	for stream.Receive() {
		frames = append(frames, stream.Msg())
	}
	return frames, stream.Err()
}

// testOutcome — итог стрима TestLatency: кадры и ошибка.
type testOutcome struct {
	frames []*lavrayv1.LatencyResult
	err    error
}

// startTest запускает TestLatency в фоне: клиентский вызов Connect по HTTP/1.1
// возвращается лишь с заголовками ответа, то есть с первым кадром или концом
// стрима, поэтому тесту, который вмешивается посреди прогона, ждать его нельзя.
func startTest(ctx context.Context, e *stopEnv, req *lavrayv1.TestLatencyRequest) <-chan testOutcome {
	out := make(chan testOutcome, 1)
	go func() {
		var res testOutcome
		stream, err := e.profile.TestLatency(ctx, connect.NewRequest(req))
		if err != nil {
			res.err = err
		} else {
			for stream.Receive() {
				res.frames = append(res.frames, stream.Msg())
			}
			res.err = stream.Err()
		}
		out <- res
	}()
	return out
}

func setIntSetting(t *testing.T, s *Server, key string, v int64) {
	t.Helper()
	if err := setSetting(t, s, key, intSetting(v)); err != nil {
		t.Fatalf("%s: %v", key, err)
	}
}

func setStrSetting(t *testing.T, s *Server, key, v string) {
	t.Helper()
	if err := setSetting(t, s, key, strSetting(v)); err != nil {
		t.Fatalf("%s: %v", key, err)
	}
}

func idsOf(e *stopEnv, ports ...int) []int64 {
	out := make([]int64, 0, len(ports))
	for _, p := range ports {
		out = append(out, e.ids[p])
	}
	return out
}

// Полный тест без единой проверки — INVALID_ARGUMENT, инстансы не поднимаются.
func TestFullTestRequiresAtLeastOneFlag(t *testing.T) {
	e := newStopEnv(t, 2001)
	ff := newFakeFactory(func() *fakeDialer { return &fakeDialer{} })
	e.s.newTestInst = ff.factory()

	_, err := runTest(t, e, &lavrayv1.TestLatencyRequest{
		ProfileIds: idsOf(e, 2001),
		Mode:       lavrayv1.TestLatencyRequest_MODE_FULL_TEST,
	})
	if connect.CodeOf(err) != connect.CodeInvalidArgument {
		t.Fatalf("code=%v err=%v", connect.CodeOf(err), err)
	}
	// Явно выключенные флаги — то же самое.
	off := false
	_, err = runTest(t, e, &lavrayv1.TestLatencyRequest{
		ProfileIds: idsOf(e, 2001), Mode: lavrayv1.TestLatencyRequest_MODE_FULL_TEST,
		FullLatency: &off, FullUdpLatency: &off, FullSpeed: &off, FullInOutIp: &off,
	})
	if connect.CodeOf(err) != connect.CodeInvalidArgument {
		t.Fatalf("выключенные флаги: code=%v err=%v", connect.CodeOf(err), err)
	}
	if n := ff.created.Load(); n != 0 {
		t.Fatalf("поднято инстансов: %d", n)
	}
}

// Адреса проверяются: схема только http/https, хост обязателен; то же — для
// настроек и UDP-цели.
func TestURLTestValidatesAddresses(t *testing.T) {
	e := newStopEnv(t, 2002)
	e.s.newTestInst = newFakeFactory(func() *fakeDialer { return &fakeDialer{} }).factory()
	for _, bad := range []string{"ftp://example.com/x", "http://", "example.com", "file:///etc/passwd", "javascript:alert(1)"} {
		u := bad
		_, err := runTest(t, e, &lavrayv1.TestLatencyRequest{
			ProfileIds: idsOf(e, 2002), Mode: lavrayv1.TestLatencyRequest_MODE_URL_TEST, Url: &u,
		})
		if connect.CodeOf(err) != connect.CodeInvalidArgument {
			t.Errorf("url %q: code=%v err=%v", bad, connect.CodeOf(err), err)
		}
	}
	on := true
	bad := "gopher://x/"
	_, err := runTest(t, e, &lavrayv1.TestLatencyRequest{
		ProfileIds: idsOf(e, 2002), Mode: lavrayv1.TestLatencyRequest_MODE_FULL_TEST,
		FullSpeed: &on, FullSpeedUrl: &bad,
	})
	if connect.CodeOf(err) != connect.CodeInvalidArgument {
		t.Errorf("full_speed_url: code=%v err=%v", connect.CodeOf(err), err)
	}

	for key, bad := range map[string]string{
		"test.url":        "ftp://x/",
		"test.url_dl":     "http://",
		"test.url_ip":     "not a url",
		"test.udp_target": "1.1.1.1",
	} {
		if err := setSetting(t, e.s, key, strSetting(bad)); connect.CodeOf(err) != connect.CodeInvalidArgument {
			t.Errorf("%s=%q принято: %v", key, bad, err)
		}
	}
	for _, bad := range []int64{0, 17, -1} {
		if err := setSetting(t, e.s, "test.concurrent", intSetting(bad)); connect.CodeOf(err) != connect.CodeInvalidArgument {
			t.Errorf("test.concurrent=%d принято: %v", bad, err)
		}
	}
	for key, good := range map[string]string{
		"test.url":        "http://127.0.0.1:8080/ping",
		"test.url_dl":     "https://example.com/10mb.test",
		"test.url_ip":     "https://cloudflare.com/cdn-cgi/trace",
		"test.udp_target": "dns.example.net:53",
	} {
		if err := setSetting(t, e.s, key, strSetting(good)); err != nil {
			t.Errorf("%s=%q отвергнуто: %v", key, good, err)
		}
	}
	for _, good := range []int64{1, 5, 16} {
		if err := setSetting(t, e.s, "test.concurrent", intSetting(good)); err != nil {
			t.Errorf("test.concurrent=%d отвергнуто: %v", good, err)
		}
	}
}

// Умолчания владельца (2026-10-29).
func TestInstanceTestDefaults(t *testing.T) {
	e := newStopEnv(t, 2003)
	s := e.s
	for key, want := range map[string]string{
		"test.url":        "https://www.gstatic.com/generate_204",
		"test.url_dl":     "http://cachefly.cachefly.net/10mb.test",
		"test.url_ip":     "https://cloudflare.com/cdn-cgi/trace",
		"test.udp_target": "1.1.1.1:53",
	} {
		if got := s.settingStr(key); got != want {
			t.Errorf("%s = %q, ожидалось %q", key, got, want)
		}
	}
	if got := s.settingInt("test.concurrent"); got != 5 {
		t.Errorf("test.concurrent = %d, ожидалось 5", got)
	}
}

// URL-тест: на каждый профиль свой инстанс, каждый закрыт, одновременных не
// больше test.concurrent, результат сохранён, запрос идёт на test.url.
func TestURLTestOneInstancePerProfileWithinLimit(t *testing.T) {
	ports := []int{2101, 2102, 2103, 2104, 2105, 2106}
	e := newStopEnv(t, ports...)
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		time.Sleep(60 * time.Millisecond) // инстансы успевают перекрыться
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()
	setStrSetting(t, e.s, "test.url", srv.URL+"/generate_204")
	setIntSetting(t, e.s, "test.concurrent", 2)
	ff := newFakeFactory(func() *fakeDialer { return &fakeDialer{} })
	e.s.newTestInst = ff.factory()

	frames, err := runTest(t, e, &lavrayv1.TestLatencyRequest{
		ProfileIds: idsOf(e, ports...), Mode: lavrayv1.TestLatencyRequest_MODE_URL_TEST,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(frames) != len(ports) {
		t.Fatalf("кадров %d, ожидалось %d", len(frames), len(ports))
	}
	for _, f := range frames {
		if f.Result.GetLatencyMs() <= 0 || f.Result.GetLastError() != "" || f.Total != int32(len(ports)) {
			t.Errorf("кадр %d: %+v", f.ProfileId, f)
		}
	}
	if got := int(ff.created.Load()); got != len(ports) {
		t.Errorf("создано инстансов %d, ожидалось %d", got, len(ports))
	}
	if ff.closed.Load() != ff.created.Load() {
		t.Errorf("закрыто %d из %d", ff.closed.Load(), ff.created.Load())
	}
	if peak := ff.peak.Load(); peak > 2 || peak < 2 {
		t.Errorf("пик одновременных инстансов %d, ожидалось 2 (test.concurrent)", peak)
	}
	if hits.Load() != int32(len(ports)) {
		t.Errorf("запросов к test.url: %d", hits.Load())
	}
	for _, port := range ports {
		if r := e.previous(t, port); r.LatencyMs == nil || *r.LatencyMs <= 0 || r.LastError != "" {
			t.Errorf("порт %d: результат не сохранён: %+v", port, r)
		}
	}

	// Конфиг тестового инстанса — без входящих (cfgbuild ForTest).
	select {
	case cfg := <-ff.configs:
		if strings.Contains(string(cfg), `"mixed"`) || strings.Contains(string(cfg), `"cache_file"`) {
			t.Errorf("в тестовом конфиге входящие или cache_file:\n%s", cfg)
		}
	default:
		t.Error("фабрика не получила конфиг")
	}
}

// Запрос может уменьшить параллелизм, но не превысить потолок 16.
func TestInstanceTestConcurrencyClamp(t *testing.T) {
	e := newStopEnv(t, 2201)
	for _, tc := range []struct {
		req  int32
		want int
	}{{3, 3}, {100, maxInstTestConcurrent}, {0, 5}} {
		c := tc.req
		plan, err := e.s.planInstanceTest(&lavrayv1.TestLatencyRequest{
			Mode: lavrayv1.TestLatencyRequest_MODE_URL_TEST, Concurrency: &c,
		})
		if err != nil {
			t.Fatal(err)
		}
		if plan.concurrency != tc.want {
			t.Errorf("concurrency=%d → %d, ожидалось %d", tc.req, plan.concurrency, tc.want)
		}
	}
}

// Регрессия T-84: StopTests прерывает все идущие проверки, каждый инстанс
// закрыт, прерванные профили сохраняют прежний результат.
func TestStopTestsCancelsInstances(t *testing.T) {
	ports := []int{2301, 2302, 2303}
	e := newStopEnv(t, ports...)
	var inFlight atomic.Int32
	reached := make(chan struct{}, len(ports))
	ff := newFakeFactory(func() *fakeDialer {
		return &fakeDialer{dial: func(ctx context.Context, _, _ string) (net.Conn, error) {
			inFlight.Add(1)
			reached <- struct{}{}
			<-ctx.Done() // «медленный сервер»: висит, пока тест не остановят
			return nil, ctx.Err()
		}}
	})
	e.s.newTestInst = ff.factory()
	setIntSetting(t, e.s, "test.concurrent", 3)
	setStrSetting(t, e.s, "test.url", "http://127.0.0.1:1/x")

	prev := map[int]model.TestResult{}
	for _, p := range ports {
		prev[p] = e.previous(t, p)
	}
	timeout := int32(60000)
	outcome := startTest(context.Background(), e, &lavrayv1.TestLatencyRequest{
		ProfileIds: idsOf(e, ports...), Mode: lavrayv1.TestLatencyRequest_MODE_URL_TEST, TimeoutMs: &timeout,
	})
	for range ports {
		select {
		case <-reached:
		case <-time.After(5 * time.Second):
			t.Fatal("проверки не начались")
		}
	}
	if _, err := e.profile.StopTests(context.Background(), connect.NewRequest(&lavrayv1.StopTestsRequest{})); err != nil {
		t.Fatal(err)
	}
	var res testOutcome
	select {
	case res = <-outcome:
	case <-time.After(10 * time.Second):
		t.Fatal("стрим не завершился после StopTests")
	}
	if res.err != nil {
		t.Fatalf("стрим после StopTests: %v", res.err)
	}
	if len(res.frames) != 0 {
		t.Errorf("по прерванным профилям пришло кадров: %d", len(res.frames))
	}
	if ff.created.Load() != int32(len(ports)) || ff.closed.Load() != ff.created.Load() {
		t.Errorf("создано %d, закрыто %d", ff.created.Load(), ff.closed.Load())
	}
	for _, p := range ports {
		got := e.previous(t, p)
		if got.LastError != "" || got.LatencyMs == nil || *got.LatencyMs != *prev[p].LatencyMs {
			t.Errorf("порт %d: прерванный тест затёр результат: было %+v, стало %+v", p, prev[p], got)
		}
	}
}

// Профиль без ответа: «таймаут N мс» в колонке результата, инстанс закрыт.
func TestURLTestTimeoutIsSavedAsError(t *testing.T) {
	e := newStopEnv(t, 2501)
	ff := newFakeFactory(func() *fakeDialer {
		return &fakeDialer{dial: func(ctx context.Context, _, _ string) (net.Conn, error) {
			<-ctx.Done()
			return nil, ctx.Err()
		}}
	})
	e.s.newTestInst = ff.factory()
	setStrSetting(t, e.s, "test.url", "http://127.0.0.1:1/x")
	timeout := int32(80)
	frames, err := runTest(t, e, &lavrayv1.TestLatencyRequest{
		ProfileIds: idsOf(e, 2501), Mode: lavrayv1.TestLatencyRequest_MODE_URL_TEST, TimeoutMs: &timeout,
	})
	if err != nil || len(frames) != 1 {
		t.Fatalf("кадров %d, ошибка %v", len(frames), err)
	}
	if got := frames[0].Result.GetLastError(); !strings.HasPrefix(got, "таймаут 80") {
		t.Errorf("LastError = %q", got)
	}
	if r := e.previous(t, 2501); r.LatencyMs != nil || r.LastError == "" {
		t.Errorf("результат: %+v", r)
	}
	if ff.closed.Load() != 1 {
		t.Errorf("закрыто %d", ff.closed.Load())
	}
}

// Полный тест: все четыре проверки, отчёт в кадре и в профиле.
func TestFullTestReportIsPersisted(t *testing.T) {
	e := newStopEnv(t, 2601)
	mux := http.NewServeMux()
	mux.HandleFunc("/ping", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) })
	mux.HandleFunc("/dl", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write(make([]byte, 1<<20))
	})
	mux.HandleFunc("/trace", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = fmt.Fprint(w, "fl=1\nip=203.0.113.9\nts=1\n")
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()
	udp, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer udp.Close()
	go func() {
		buf := make([]byte, 1500)
		for {
			n, from, err := udp.ReadFrom(buf)
			if err != nil {
				return
			}
			_, _ = udp.WriteTo(buf[:n], from)
		}
	}()
	setStrSetting(t, e.s, "test.url", srv.URL+"/ping")
	setStrSetting(t, e.s, "test.url_dl", srv.URL+"/dl")
	setStrSetting(t, e.s, "test.url_ip", srv.URL+"/trace")
	setStrSetting(t, e.s, "test.udp_target", udp.LocalAddr().String())
	ff := newFakeFactory(func() *fakeDialer {
		return &fakeDialer{listen: func(_ context.Context, dest string) (net.PacketConn, error) {
			if dest != udp.LocalAddr().String() {
				return nil, fmt.Errorf("цель UDP %q, ожидалась %s", dest, udp.LocalAddr())
			}
			return newLoopbackPacketConn()
		}}
	})
	e.s.newTestInst = ff.factory()
	// Профиль — SOCKS 192.0.2.1: входной IP — литерал, без разбора имени.

	on := true
	frames, err := runTest(t, e, &lavrayv1.TestLatencyRequest{
		ProfileIds: idsOf(e, 2601), Mode: lavrayv1.TestLatencyRequest_MODE_FULL_TEST,
		FullLatency: &on, FullUdpLatency: &on, FullSpeed: &on, FullInOutIp: &on,
	})
	if err != nil || len(frames) != 1 {
		t.Fatalf("кадров %d, ошибка %v", len(frames), err)
	}
	check := func(where string, r *lavrayv1.TestResult) {
		t.Helper()
		rep := r.GetFullReport()
		if rep == nil || r.GetLastError() != "" {
			t.Fatalf("%s: нет отчёта или ошибка: %+v", where, r)
		}
		if rep.GetLatencyMs() <= 0 || r.GetLatencyMs() != rep.GetLatencyMs() {
			t.Errorf("%s: задержка %d / %d", where, rep.GetLatencyMs(), r.GetLatencyMs())
		}
		if rep.GetUdpLatencyMs() <= 0 {
			t.Errorf("%s: UDP-задержка %d", where, rep.GetUdpLatencyMs())
		}
		if rep.GetDownloadBps() <= 0 {
			t.Errorf("%s: скорость %v", where, rep.GetDownloadBps())
		}
		if rep.GetEgressIp() != "203.0.113.9" || rep.GetIngressIp() != "192.0.2.1" {
			t.Errorf("%s: IP in=%q out=%q", where, rep.GetIngressIp(), rep.GetEgressIp())
		}
		if rep.Error != nil {
			t.Errorf("%s: error=%q", where, rep.GetError())
		}
	}
	check("кадр", frames[0].Result)

	// Тот же отчёт читается из профиля (его показывает экран «Серверы»).
	got, err := e.profile.GetProfile(context.Background(), connect.NewRequest(&lavrayv1.GetProfileRequest{Id: e.ids[2601]}))
	if err != nil {
		t.Fatal(err)
	}
	check("профиль", got.Msg.TestResult)
	if ff.closed.Load() != 1 {
		t.Errorf("закрыто %d", ff.closed.Load())
	}
}

// Частичный отказ: один замер упал, другие в отчёте, профиль не «недоступен».
func TestFullTestPartialFailureKeepsOtherResults(t *testing.T) {
	e := newStopEnv(t, 2701)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/dl" {
			http.Error(w, "nope", http.StatusForbidden)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()
	setStrSetting(t, e.s, "test.url", srv.URL+"/ping")
	setStrSetting(t, e.s, "test.url_dl", srv.URL+"/dl")
	e.s.newTestInst = newFakeFactory(func() *fakeDialer { return &fakeDialer{} }).factory()

	on := true
	frames, err := runTest(t, e, &lavrayv1.TestLatencyRequest{
		ProfileIds: idsOf(e, 2701), Mode: lavrayv1.TestLatencyRequest_MODE_FULL_TEST,
		FullLatency: &on, FullSpeed: &on,
	})
	if err != nil || len(frames) != 1 {
		t.Fatalf("кадров %d, ошибка %v", len(frames), err)
	}
	r := frames[0].Result
	rep := r.GetFullReport()
	if r.GetLastError() != "" || rep.GetLatencyMs() <= 0 || rep.DownloadBps != nil || !strings.Contains(rep.GetError(), "скорость") {
		t.Fatalf("частичный отказ: %+v / %+v", r, rep)
	}
}

// Все замеры упали: профиль «недоступен» (LastError), причины — в отчёте.
func TestFullTestAllFailedMarksProfileDown(t *testing.T) {
	e := newStopEnv(t, 2801)
	setStrSetting(t, e.s, "test.url", "http://127.0.0.1:1/x")
	e.s.newTestInst = newFakeFactory(func() *fakeDialer {
		return &fakeDialer{dial: func(context.Context, string, string) (net.Conn, error) {
			return nil, fmt.Errorf("connection refused")
		}}
	}).factory()
	on := true
	frames, err := runTest(t, e, &lavrayv1.TestLatencyRequest{
		ProfileIds: idsOf(e, 2801), Mode: lavrayv1.TestLatencyRequest_MODE_FULL_TEST, FullLatency: &on,
	})
	if err != nil || len(frames) != 1 {
		t.Fatalf("кадров %d, ошибка %v", len(frames), err)
	}
	r := frames[0].Result
	if !strings.Contains(r.GetLastError(), "connection refused") || r.LatencyMs != nil ||
		!strings.Contains(r.GetFullReport().GetError(), "connection refused") {
		t.Fatalf("результат: %+v", r)
	}
}

// Профили, которые нельзя свести к одному исходящему, и сборка без sing-box —
// «н/д», а не «недоступен».
func TestInstanceTestNotApplicable(t *testing.T) {
	e := newStopEnv(t, 2901)
	custom := &model.Profile{
		GroupID: 1, Type: model.TypeCustomConfig, Name: "custom", Address: "127.0.0.1", Port: 1,
		Detail: &model.CustomDetail{Core: "internal-full", FullConfigJSON: `{"outbounds":[]}`},
	}
	if err := e.s.st.CreateProfile(custom); err != nil {
		t.Fatal(err)
	}
	ff := newFakeFactory(func() *fakeDialer { return &fakeDialer{} })
	e.s.newTestInst = ff.factory()
	frames, err := runTest(t, e, &lavrayv1.TestLatencyRequest{
		ProfileIds: []int64{custom.ID}, Mode: lavrayv1.TestLatencyRequest_MODE_URL_TEST,
	})
	if err != nil || len(frames) != 1 || !strings.HasPrefix(frames[0].Result.GetLastError(), testNAPrefix) {
		t.Fatalf("custom_config: %+v err=%v", frames, err)
	}
	if ff.created.Load() != 0 {
		t.Errorf("для custom_config поднят инстанс")
	}

	e.s.newTestInst = func(context.Context, []byte) (inst.TestDialer, error) {
		return nil, inst.ErrTestInstanceUnsupported
	}
	frames, err = runTest(t, e, &lavrayv1.TestLatencyRequest{
		ProfileIds: idsOf(e, 2901), Mode: lavrayv1.TestLatencyRequest_MODE_URL_TEST,
	})
	if err != nil || len(frames) != 1 || !strings.HasPrefix(frames[0].Result.GetLastError(), testNAPrefix) {
		t.Fatalf("сборка без sing-box: %+v err=%v", frames, err)
	}
}

// Hysteria2 и TUIC проверяются URL-тестом так же, как остальные (идут через
// исходящее), в отличие от TCP-пинга.
func TestURLTestAppliesToQUICProtocols(t *testing.T) {
	e := newStopEnv(t, 3001)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) }))
	defer srv.Close()
	setStrSetting(t, e.s, "test.url", srv.URL)
	ff := newFakeFactory(func() *fakeDialer { return &fakeDialer{} })
	e.s.newTestInst = ff.factory()
	hy2 := &model.Profile{GroupID: 1, Type: model.TypeHysteria2, Name: "hy2", Address: "192.0.2.7", Port: 443,
		Detail: &model.Hysteria2Detail{Password: "secret"}}
	if err := e.s.st.CreateProfile(hy2); err != nil {
		t.Fatal(err)
	}
	frames, err := runTest(t, e, &lavrayv1.TestLatencyRequest{
		ProfileIds: []int64{hy2.ID}, Mode: lavrayv1.TestLatencyRequest_MODE_URL_TEST,
	})
	if err != nil || len(frames) != 1 || frames[0].Result.GetLatencyMs() <= 0 || frames[0].Result.GetLastError() != "" {
		t.Fatalf("hysteria2: %+v err=%v", frames, err)
	}
	if ff.created.Load() != 1 {
		t.Errorf("инстансов %d", ff.created.Load())
	}
}

func TestParseEgressIP(t *testing.T) {
	for in, want := range map[string]string{
		"fl=1\nip=203.0.113.9\nts=1\n": "203.0.113.9",
		"ip=2001:db8::1\n":             "2001:db8::1",
		"198.51.100.4\n":               "198.51.100.4",
		"ip=not-an-ip\n":               "",
		"":                             "",
		"<html>":                       "",
	} {
		got, ok := parseEgressIP(in)
		if got != want || ok != (want != "") {
			t.Errorf("parseEgressIP(%q) = %q, %v", in, got, ok)
		}
	}
}

func TestDNSQueryLayout(t *testing.T) {
	q := dnsQuery(0xABCD, "example.com")
	want := []byte{0xAB, 0xCD, 0x01, 0x00, 0, 1, 0, 0, 0, 0, 0, 0,
		7, 'e', 'x', 'a', 'm', 'p', 'l', 'e', 3, 'c', 'o', 'm', 0, 0, 1, 0, 1}
	if string(q) != string(want) {
		t.Fatalf("dnsQuery = % x", q)
	}
}
