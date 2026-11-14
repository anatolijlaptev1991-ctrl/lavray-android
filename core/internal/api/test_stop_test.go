// SPDX-License-Identifier: GPL-3.0-only

package api

// test_stop_test.go — D-05: остановленный (StopTests) или оборванный тест не
// перезаписывает прошлый результат профиля ошибкой «context canceled».

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"connectrpc.com/connect"
	lavrayv1 "github.com/anatolijlaptev1991-ctrl/lavray-android/core/internal/gen/lavrayv1"
	"github.com/anatolijlaptev1991-ctrl/lavray-android/core/internal/gen/lavrayv1connect"
	"github.com/anatolijlaptev1991-ctrl/lavray-android/core/internal/model"
)

// stopEnv — сервер с подменяемым dial и клиент ProfileService.
type stopEnv struct {
	s       *Server
	profile lavrayv1connect.ProfileServiceClient
	ids     map[int]int64 // порт профиля → id
}

func newStopEnv(t *testing.T, ports ...int) *stopEnv {
	t.Helper()
	s, st := subAutoTestServer(t)
	if _, err := st.GetGroup(1); err != nil {
		if err := st.CreateGroup(&model.Group{ID: 1, Name: "Default", Kind: model.GroupBasic}); err != nil {
			t.Fatal(err)
		}
	}
	mux := http.NewServeMux()
	s.Register(mux)
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	e := &stopEnv{s: s, profile: lavrayv1connect.NewProfileServiceClient(srv.Client(), srv.URL), ids: map[int]int64{}}
	lat, at := int32(55), int64(1700000000000)
	for _, port := range ports {
		p := &model.Profile{
			GroupID: 1, Type: model.TypeSOCKS, Name: "p", Address: "192.0.2.1", Port: port,
			Detail: &model.SocksHTTPDetail{Proto: "socks5"},
		}
		if err := st.CreateProfile(p); err != nil {
			t.Fatal(err)
		}
		// Прошлый удачный результат, который прерванный тест затирать не должен.
		if err := st.SaveTestResult(p.ID, model.TestResult{LatencyMs: &lat, TestedAtMs: &at}); err != nil {
			t.Fatal(err)
		}
		e.ids[port] = p.ID
	}
	return e
}

func (e *stopEnv) previous(t *testing.T, port int) model.TestResult {
	t.Helper()
	p, err := e.s.st.GetProfile(e.ids[port])
	if err != nil {
		t.Fatal(err)
	}
	return p.TestResult
}

func portOf(addr string) string {
	_, port, _ := net.SplitHostPort(addr)
	return port
}

func TestStoppedTestDoesNotOverwritePreviousResult(t *testing.T) {
	const fast, slow = 1001, 1002
	e := newStopEnv(t, fast, slow)

	started := make(chan struct{}, 1)
	e.s.dial = func(ctx context.Context, _, addr string) (net.Conn, error) {
		if portOf(addr) == "1001" {
			c, peer := net.Pipe() // быстрый сервер: соединение сразу
			_ = peer.Close()
			return c, nil
		}
		started <- struct{}{}
		<-ctx.Done() // медленный: висит, пока тест не остановят
		return nil, ctx.Err()
	}

	// Быстрый профиль обязан получить свежий результат, медленный (прерванный
	// StopTests) — остаться с прежним.
	prevFast := e.previous(t, fast)
	prevSlow := e.previous(t, slow)

	var frames []*lavrayv1.LatencyResult
	ids := []int64{e.ids[fast], e.ids[slow]}
	timeout := int32(30000)
	stream, err := e.profile.TestLatency(context.Background(), connect.NewRequest(&lavrayv1.TestLatencyRequest{
		ProfileIds: ids, Mode: lavrayv1.TestLatencyRequest_MODE_TCP_PING, TimeoutMs: &timeout,
	}))
	if err != nil {
		t.Fatal(err)
	}
	select {
	case <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("тест медленного профиля не начался")
	}
	if _, err := e.profile.StopTests(context.Background(), connect.NewRequest(&lavrayv1.StopTestsRequest{})); err != nil {
		t.Fatalf("StopTests: %v", err)
	}
	for stream.Receive() {
		frames = append(frames, stream.Msg())
	}
	if err := stream.Err(); err != nil {
		t.Fatalf("стрим после StopTests: %v", err)
	}

	slowRes := e.previous(t, slow)
	if slowRes.LastError != "" || slowRes.LatencyMs == nil || *slowRes.LatencyMs != *prevSlow.LatencyMs ||
		slowRes.TestedAtMs == nil || *slowRes.TestedAtMs != *prevSlow.TestedAtMs {
		t.Fatalf("прерванное измерение затёрло прошлый результат: было %+v, стало %+v", prevSlow, slowRes)
	}
	fastRes := e.previous(t, fast)
	if fastRes.LastError != "" || fastRes.LatencyMs == nil || *fastRes.TestedAtMs == *prevFast.TestedAtMs {
		t.Fatalf("завершённое до остановки измерение должно сохраниться свежим: было %+v, стало %+v", prevFast, fastRes)
	}
	for _, f := range frames {
		if f.ProfileId == e.ids[slow] {
			t.Errorf("по прерванному профилю пришёл кадр: %+v", f.Result)
		}
		if strings.Contains(f.GetResult().GetLastError(), "canceled") {
			t.Errorf("в кадре ошибка отмены: %q", f.GetResult().GetLastError())
		}
	}
	if len(frames) != 1 || frames[0].ProfileId != e.ids[fast] {
		t.Errorf("ожидался один кадр по быстрому профилю, получено %d", len(frames))
	}
}

// Собственный таймаут измерения — не отмена: «таймаут N мс» сохраняется как прежде.
func TestOwnTimeoutIsStillSavedAsError(t *testing.T) {
	const port = 1003
	e := newStopEnv(t, port)
	e.s.dial = func(ctx context.Context, _, _ string) (net.Conn, error) {
		<-ctx.Done() // дедлайн измерения
		return nil, ctx.Err()
	}

	timeout := int32(50)
	stream, err := e.profile.TestLatency(context.Background(), connect.NewRequest(&lavrayv1.TestLatencyRequest{
		ProfileIds: []int64{e.ids[port]}, Mode: lavrayv1.TestLatencyRequest_MODE_TCP_PING, TimeoutMs: &timeout,
	}))
	if err != nil {
		t.Fatal(err)
	}
	frames := 0
	for stream.Receive() {
		frames++
	}
	if err := stream.Err(); err != nil || frames != 1 {
		t.Fatalf("стрим: кадров %d, ошибка %v", frames, err)
	}
	res := e.previous(t, port)
	if res.LatencyMs != nil || !strings.HasPrefix(res.LastError, "таймаут") {
		t.Fatalf("таймаут не сохранён как ошибка: %+v", res)
	}
}
