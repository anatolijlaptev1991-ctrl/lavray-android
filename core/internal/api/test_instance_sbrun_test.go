// SPDX-License-Identifier: GPL-3.0-only

//go:build singbox_run

package api

// test_instance_sbrun_test.go — URL- и полный тест (T-84) на настоящем
// sing-box: профиль SOCKS5 указывает на локальный SOCKS-сервер, цели — на
// loopback. Внешних адресов тесты не трогают.

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"connectrpc.com/connect"
	lavrayv1 "github.com/anatolijlaptev1991-ctrl/lavray-android/core/internal/gen/lavrayv1"
	"github.com/anatolijlaptev1991-ctrl/lavray-android/core/internal/model"
	"github.com/anatolijlaptev1991-ctrl/lavray-android/core/internal/testsocks"
)

// socksProfile — SOCKS5-профиль на 127.0.0.1:port.
func socksProfile(t *testing.T, e *stopEnv, port int) int64 {
	t.Helper()
	p := &model.Profile{
		GroupID: 1, Type: model.TypeSOCKS, Name: "socks", Address: "127.0.0.1", Port: port,
		Detail: &model.SocksHTTPDetail{Proto: "socks5"},
	}
	if err := e.s.st.CreateProfile(p); err != nil {
		t.Fatal(err)
	}
	return p.ID
}

// onePassResult гоняет TestLatency по одному профилю и возвращает единственный кадр.
func onePassResult(t *testing.T, e *stopEnv, req *lavrayv1.TestLatencyRequest) *lavrayv1.TestResult {
	t.Helper()
	frames, err := runTest(t, e, req)
	if err != nil || len(frames) != 1 {
		t.Fatalf("кадров %d, ошибка %v", len(frames), err)
	}
	return frames[0].Result
}

// Регрессия T-84: URL-тест идёт через исходящее профиля — SOCKS-сервер видит
// цель — и даёт положительную задержку.
func TestURLTestThroughSocksOutbound(t *testing.T) {
	e := newStopEnv(t)
	socks := testsocks.Start(t)
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	defer target.Close()
	setStrSetting(t, e.s, "test.url", target.URL+"/generate_204")
	id := socksProfile(t, e, socks.Port())

	r := onePassResult(t, e, &lavrayv1.TestLatencyRequest{
		ProfileIds: []int64{id}, Mode: lavrayv1.TestLatencyRequest_MODE_URL_TEST,
	})
	if r.GetLatencyMs() <= 0 || r.GetLastError() != "" {
		t.Fatalf("результат: %+v", r)
	}
	dests := socks.TCPDestinations()
	if len(dests) != 1 || dests[0] != target.Listener.Addr().String() {
		t.Fatalf("SOCKS-сервер профиля увидел цели %v, ожидалась %s", dests, target.Listener.Addr())
	}
	saved, err := e.s.st.GetProfile(id)
	if err != nil || saved.TestResult.LatencyMs == nil || *saved.TestResult.LatencyMs <= 0 {
		t.Fatalf("результат не сохранён: %+v err=%v", saved, err)
	}
}

// Мимо профиля тест не ходит: сервер профиля недоступен — тест падает, хотя
// цель на loopback открыта и «напрямую» ответила бы.
func TestURLTestNeverFallsBackToDirect(t *testing.T) {
	e := newStopEnv(t)
	var hits atomic.Int32
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hits.Add(1)
		w.WriteHeader(http.StatusNoContent)
	}))
	defer target.Close()
	dead, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	deadPort := dead.Addr().(*net.TCPAddr).Port
	_ = dead.Close()
	setStrSetting(t, e.s, "test.url", target.URL)
	id := socksProfile(t, e, deadPort)

	timeout := int32(3000)
	r := onePassResult(t, e, &lavrayv1.TestLatencyRequest{
		ProfileIds: []int64{id}, Mode: lavrayv1.TestLatencyRequest_MODE_URL_TEST, TimeoutMs: &timeout,
	})
	if r.GetLastError() == "" || r.LatencyMs != nil {
		t.Fatalf("ожидалась ошибка: %+v", r)
	}
	if n := hits.Load(); n != 0 {
		t.Fatalf("цель получила %d запросов мимо профиля", n)
	}
}

// Регрессия T-84: полный тест считает скорость по телу 4 МиБ.
func TestFullTestSpeedAgainstLocalServer(t *testing.T) {
	e := newStopEnv(t)
	socks := testsocks.Start(t)
	const size = 4 << 20
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Length", fmt.Sprint(size))
		_, _ = w.Write(make([]byte, size))
	}))
	defer target.Close()
	setStrSetting(t, e.s, "test.url_dl", target.URL+"/4mb.test")
	id := socksProfile(t, e, socks.Port())

	on := true
	r := onePassResult(t, e, &lavrayv1.TestLatencyRequest{
		ProfileIds: []int64{id}, Mode: lavrayv1.TestLatencyRequest_MODE_FULL_TEST, FullSpeed: &on,
	})
	rep := r.GetFullReport()
	if r.GetLastError() != "" || rep.GetDownloadBps() <= 0 {
		t.Fatalf("результат: %+v / %+v", r, rep)
	}
	if rep.LatencyMs != nil || rep.UdpLatencyMs != nil {
		t.Errorf("в отчёте замеры, которых не просили: %+v", rep)
	}
	// 4 МиБ за время теста: скорость осмысленна, а не «1 байт за наносекунду».
	if mib := rep.GetDownloadBps() / 8 / (1 << 20); mib < 0.1 {
		t.Errorf("скорость %.3f МиБ/с слишком мала для loopback", mib)
	}
	saved, err := e.s.st.GetProfile(id)
	if err != nil || parseFullReport(saved.TestResult.FullReport).GetDownloadBps() <= 0 {
		t.Fatalf("отчёт не сохранён в профиле: %+v err=%v", saved, err)
	}
}

// Регрессия T-84: UDP-замер идёт на цель из test.udp_target — SOCKS-сервер
// профиля видит именно её, а локальное UDP-эхо отвечает.
func TestUDPLatencyUsesConfiguredTarget(t *testing.T) {
	e := newStopEnv(t)
	socks := testsocks.Start(t)
	echo, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer echo.Close()
	go func() {
		buf := make([]byte, 1500)
		for {
			n, from, err := echo.ReadFrom(buf)
			if err != nil {
				return
			}
			_, _ = echo.WriteTo(buf[:n], from)
		}
	}()
	target := echo.LocalAddr().String()
	setStrSetting(t, e.s, "test.udp_target", target)
	id := socksProfile(t, e, socks.Port())

	on := true
	r := onePassResult(t, e, &lavrayv1.TestLatencyRequest{
		ProfileIds: []int64{id}, Mode: lavrayv1.TestLatencyRequest_MODE_FULL_TEST, FullUdpLatency: &on,
	})
	if r.GetLastError() != "" || r.GetFullReport().GetUdpLatencyMs() <= 0 {
		t.Fatalf("результат: %+v", r)
	}
	dests := socks.UDPDestinations()
	if len(dests) == 0 || dests[0] != target {
		t.Fatalf("SOCKS-сервер увидел UDP-цели %v, ожидалась %s", dests, target)
	}
	if strings.Contains(strings.Join(dests, ","), "8.8.8.8") {
		t.Fatalf("захардкоженный резолвер в трафике: %v", dests)
	}
}

// Все четыре проверки сразу на настоящем инстансе.
func TestFullTestAllChecksThroughSocks(t *testing.T) {
	e := newStopEnv(t)
	socks := testsocks.Start(t)
	mux := http.NewServeMux()
	mux.HandleFunc("/ping", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) })
	mux.HandleFunc("/dl", func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write(make([]byte, 1<<20)) })
	mux.HandleFunc("/trace", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = fmt.Fprint(w, "fl=1\nip=203.0.113.77\n")
	})
	target := httptest.NewServer(mux)
	defer target.Close()
	echo, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer echo.Close()
	go func() {
		buf := make([]byte, 1500)
		for {
			n, from, err := echo.ReadFrom(buf)
			if err != nil {
				return
			}
			_, _ = echo.WriteTo(buf[:n], from)
		}
	}()
	setStrSetting(t, e.s, "test.url", target.URL+"/ping")
	setStrSetting(t, e.s, "test.url_dl", target.URL+"/dl")
	setStrSetting(t, e.s, "test.url_ip", target.URL+"/trace")
	setStrSetting(t, e.s, "test.udp_target", echo.LocalAddr().String())
	id := socksProfile(t, e, socks.Port())

	on := true
	r := onePassResult(t, e, &lavrayv1.TestLatencyRequest{
		ProfileIds: []int64{id}, Mode: lavrayv1.TestLatencyRequest_MODE_FULL_TEST,
		FullLatency: &on, FullUdpLatency: &on, FullSpeed: &on, FullInOutIp: &on,
	})
	rep := r.GetFullReport()
	if r.GetLastError() != "" || rep.Error != nil ||
		rep.GetLatencyMs() <= 0 || rep.GetUdpLatencyMs() <= 0 || rep.GetDownloadBps() <= 0 ||
		rep.GetIngressIp() != "127.0.0.1" || rep.GetEgressIp() != "203.0.113.77" {
		t.Fatalf("результат: %+v / %+v", r, rep)
	}
}

// Регрессия T-84: StopTests прерывает идущие проверки и закрывает тестовые
// инстансы — соединение с целью через туннель обрывается.
func TestStopTestsCancelsRealInstances(t *testing.T) {
	e := newStopEnv(t)
	socks := testsocks.Start(t)
	hanging := make(chan struct{}, 1)
	released := make(chan struct{}, 1)
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hanging <- struct{}{}
		<-r.Context().Done() // отвечать не собираемся: ждём обрыва соединения
		released <- struct{}{}
	}))
	defer target.Close()
	setStrSetting(t, e.s, "test.url", target.URL)
	id := socksProfile(t, e, socks.Port())

	timeout := int32(60000)
	outcome := startTest(context.Background(), e, &lavrayv1.TestLatencyRequest{
		ProfileIds: []int64{id}, Mode: lavrayv1.TestLatencyRequest_MODE_URL_TEST, TimeoutMs: &timeout,
	})
	select {
	case <-hanging:
	case <-time.After(10 * time.Second):
		t.Fatal("запрос не дошёл до цели")
	}
	if _, err := e.profile.StopTests(context.Background(), connect.NewRequest(&lavrayv1.StopTestsRequest{})); err != nil {
		t.Fatal(err)
	}
	select {
	case res := <-outcome:
		if res.err != nil || len(res.frames) != 0 {
			t.Fatalf("после StopTests: кадров %d, ошибка %v", len(res.frames), res.err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("стрим не завершился после StopTests")
	}
	select {
	case <-released:
	case <-time.After(10 * time.Second):
		t.Fatal("соединение через туннель не оборвано: инстанс не закрыт")
	}
}

// Несколько настоящих инстансов одновременно: у каждого свой реестр и свой
// box, общего состояния нет; сервер профиля видит все запросы.
func TestURLTestRunsRealInstancesInParallel(t *testing.T) {
	e := newStopEnv(t)
	socks := testsocks.Start(t)
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		time.Sleep(50 * time.Millisecond)
		w.WriteHeader(http.StatusNoContent)
	}))
	defer target.Close()
	setStrSetting(t, e.s, "test.url", target.URL)
	setIntSetting(t, e.s, "test.concurrent", 4)
	const n = 8
	ids := make([]int64, 0, n)
	for i := 0; i < n; i++ {
		ids = append(ids, socksProfile(t, e, socks.Port()))
	}

	frames, err := runTest(t, e, &lavrayv1.TestLatencyRequest{
		ProfileIds: ids, Mode: lavrayv1.TestLatencyRequest_MODE_URL_TEST,
	})
	if err != nil || len(frames) != n {
		t.Fatalf("кадров %d, ошибка %v", len(frames), err)
	}
	for _, f := range frames {
		if f.Result.GetLatencyMs() <= 0 || f.Result.GetLastError() != "" {
			t.Errorf("профиль %d: %+v", f.ProfileId, f.Result)
		}
	}
	if got := len(socks.TCPDestinations()); got != n {
		t.Fatalf("сервер профиля увидел %d запросов, ожидалось %d", got, n)
	}
}
