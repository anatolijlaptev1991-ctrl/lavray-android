// SPDX-License-Identifier: GPL-3.0-only

// test_service.go — TestLatency/StopTests/ClearTestResults/ResetTraffic
// (FR-UI-200…209, 211). TCP-пинг — dial address:port с таймаутом и
// конкурентностью из запроса. URL- и Full-тест (T-84) идут через временный
// инстанс sing-box на профиль (test_instance.go). Результат сохраняется в
// профиль (TestResult) и отдаётся стримом.
package api

import (
	"context"
	"errors"
	"net"
	"strconv"
	"sync"
	"time"

	"connectrpc.com/connect"
	lavrayv1 "github.com/anatolijlaptev1991-ctrl/lavray-android/core/internal/gen/lavrayv1"
	"github.com/anatolijlaptev1991-ctrl/lavray-android/core/internal/model"
	"github.com/anatolijlaptev1991-ctrl/lavray-android/core/internal/store"
)

const (
	defaultTestTimeoutMs  = 10000
	defaultTestConcurrent = 10
)

// testNAPrefix — начало TestResult.LastError для теста, который к этому
// профилю не применим (T-23): это не «сервер недоступен». Клиент показывает
// такой результат как «н/д» и не относит профиль к недоступным.
const testNAPrefix = "n/a: "

// tcpPingNotApplicable возвращает причину, по которой TCP-пинг не имеет
// смысла для профиля, либо "". Hysteria2 и TUIC работают по UDP/QUIC: на их
// порту TCP никто не слушает, и TCP-connect докладывал бы «недоступен» у
// живого сервера (B1-10).
func tcpPingNotApplicable(p *model.Profile) string {
	if p == nil {
		return ""
	}
	switch p.Type {
	case model.TypeHysteria2:
		return "Hysteria2 работает по UDP/QUIC — TCP-пинг неприменим"
	case model.TypeTUIC:
		return "TUIC работает по UDP/QUIC — TCP-пинг неприменим"
	}
	return ""
}

// TestLatency — стрим результатов по профилям (один кадр на профиль).
func (s *Server) TestLatency(ctx context.Context, req *connect.Request[lavrayv1.TestLatencyRequest], stream *connect.ServerStream[lavrayv1.LatencyResult]) error {
	msg := req.Msg
	if len(msg.ProfileIds) == 0 {
		return invalidArg("profile_ids не заданы")
	}
	timeoutMs := int(msg.GetTimeoutMs())
	if timeoutMs <= 0 {
		timeoutMs = defaultTestTimeoutMs
	}
	concurrency := int(msg.GetConcurrency())
	if concurrency <= 0 {
		concurrency = defaultTestConcurrent
	}
	if concurrency > 64 {
		concurrency = 64
	}
	timeout := time.Duration(timeoutMs) * time.Millisecond

	// TCP-пинг по умолчанию; URL- и Full-тест — через временные инстансы: их
	// параметры и число одновременных инстансов проверяются до любой работы.
	probe := func(ctx context.Context, p *model.Profile) (model.TestResult, bool) {
		return s.tcpPing(ctx, p, timeout)
	}
	if msg.Mode == lavrayv1.TestLatencyRequest_MODE_URL_TEST || msg.Mode == lavrayv1.TestLatencyRequest_MODE_FULL_TEST {
		plan, err := s.planInstanceTest(msg)
		if err != nil {
			return err
		}
		concurrency = plan.concurrency
		probe = func(ctx context.Context, p *model.Profile) (model.TestResult, bool) {
			return s.instanceTest(ctx, p, plan)
		}
	}

	// Профили читаем заранее: стрим не должен падать на середине из-за
	// удалённого во время теста профиля.
	items := make([]testItem, 0, len(msg.ProfileIds))
	for _, id := range msg.ProfileIds {
		p, err := s.st.GetProfile(id)
		if err != nil {
			continue // удалённый профиль пропускаем, а не валим весь батч
		}
		items = append(items, testItem{p: p})
	}
	if len(items) == 0 {
		return invalidArg("ни одного профиля из списка не существует")
	}

	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	// Single-flight «побеждает последний»: идущие батчи с теми же профилями
	// отменяются (нагрузка не умножается), StopTests отменит все батчи.
	defer s.beginTestBatch(items, cancel)()

	sem := make(chan struct{}, concurrency)
	var wg sync.WaitGroup
	var sendMu sync.Mutex
	var firstErr error
	done := 0
	total := len(items)

	for i, it := range items {
		if runCtx.Err() != nil {
			break
		}
		wg.Add(1)
		sem <- struct{}{}
		go func(idx int, p *model.Profile) {
			defer wg.Done()
			defer func() { <-sem }()

			res, aborted := probe(runCtx, p)
			if aborted {
				// D-05: StopTests/обрыв стрима прервали измерение. Это не
				// «сервер недоступен»: прошлый результат профиля не трогаем и
				// кадр не шлём.
				return
			}
			// T-22: пишем ТОЛЬКО результат (TestedAt/Latency/LastError) отдельным
			// UPDATE. Раньше сюда шла копия профиля, прочитанная при старте
			// батча, целиком через UpdateProfile: она затирала правки
			// (избранное, заметки, имя) и не сохраняла задержку (UpdateProfile
			// не пишет её колонки). Удалённый за время теста профиль — не ошибка.
			if err := s.st.SaveTestResult(p.ID, res); err != nil && !errors.Is(err, store.ErrNotFound) {
				s.logs.Logf("warn", "test", "сохранение результата профиля %d: %v", p.ID, err)
			}

			sendMu.Lock()
			defer sendMu.Unlock()
			if firstErr != nil {
				return
			}
			done++
			if err := stream.Send(&lavrayv1.LatencyResult{
				ProfileId: p.ID,
				Result:    testResultToProto(res),
				Index:     int32(done),
				Total:     int32(total),
			}); err != nil {
				firstErr = err
				cancel()
			}
			_ = idx
		}(i, it.p)
	}
	wg.Wait()
	if firstErr != nil {
		return firstErr
	}
	if ctx.Err() != nil {
		return nil
	}
	return nil
}

// tcpPing измеряет время установки TCP-соединения с сервером профиля.
// aborted=true — измерение прервано отменой ctx (StopTests, закрытие стрима),
// а не ответом сервера: результат тогда сохранять и показывать нельзя (D-05).
// Собственный таймаут измерения отменой не считается — это честное
// «таймаут N мс».
func (s *Server) tcpPing(ctx context.Context, p *model.Profile, timeout time.Duration) (res model.TestResult, aborted bool) {
	now := time.Now().UnixMilli()
	if why := tcpPingNotApplicable(p); why != "" {
		return model.TestResult{TestedAtMs: &now, LastError: testNAPrefix + why}, false
	}
	if p == nil || p.Address == "" || p.Port <= 0 {
		return model.TestResult{TestedAtMs: &now, LastError: "нет адреса/порта"}, false
	}
	host := p.Address
	if ip := net.ParseIP(host); ip == nil && host == "" {
		return model.TestResult{TestedAtMs: &now, LastError: "пустой адрес"}, false
	}
	addr := net.JoinHostPort(host, strconv.Itoa(p.Port))
	dialCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	dial := s.dial
	if dial == nil {
		dial = (&net.Dialer{}).DialContext
	}
	start := time.Now()
	conn, err := dial(dialCtx, "tcp", addr)
	elapsed := time.Since(start)
	if err != nil {
		if ctx.Err() != nil {
			// Причина — для вызывающих, которым результат всё же нужен (selfcheck).
			return model.TestResult{LastError: ctx.Err().Error()}, true
		}
		msg := err.Error()
		if errors.Is(err, context.DeadlineExceeded) {
			msg = "таймаут " + strconv.Itoa(int(timeout/time.Millisecond)) + " мс"
		}
		return model.TestResult{TestedAtMs: &now, LastError: msg}, false
	}
	_ = conn.Close()
	ms := int32(elapsed / time.Millisecond)
	if ms <= 0 {
		ms = 1
	}
	return model.TestResult{LatencyMs: &ms, TestedAtMs: &now}, false
}

// StopTests — прерывает текущий батч (FR-UI-203).
func (s *Server) StopTests(ctx context.Context, req *connect.Request[lavrayv1.StopTestsRequest]) (*connect.Response[lavrayv1.Empty], error) {
	s.cancelTestBatches()
	return connect.NewResponse(&lavrayv1.Empty{}), nil
}

// ClearTestResults — очищает результаты у выбранных профилей (FR-UI-204).
func (s *Server) ClearTestResults(ctx context.Context, req *connect.Request[lavrayv1.ClearTestResultsRequest]) (*connect.Response[lavrayv1.Empty], error) {
	ids := req.Msg.Ids
	if len(ids) == 0 {
		list, err := s.st.ListProfiles(-1)
		if err != nil {
			return nil, mapErr(err)
		}
		for _, p := range list {
			ids = append(ids, p.ID)
		}
	}
	for _, id := range ids {
		// T-22: UpdateProfile не пишет колонки результата — сбрасываем отдельным UPDATE.
		if err := s.st.SaveTestResult(id, model.TestResult{}); err != nil && !errors.Is(err, store.ErrNotFound) {
			return nil, mapErr(err)
		}
	}
	return connect.NewResponse(&lavrayv1.Empty{}), nil
}

// ResetTraffic — обнуление счётчиков трафика профилей (FR-UI-211).
func (s *Server) ResetTraffic(ctx context.Context, req *connect.Request[lavrayv1.ResetTrafficRequest]) (*connect.Response[lavrayv1.Empty], error) {
	ids := req.Msg.Ids
	if len(ids) == 0 {
		list, err := s.st.ListProfiles(-1)
		if err != nil {
			return nil, mapErr(err)
		}
		for _, p := range list {
			ids = append(ids, p.ID)
		}
	}
	if err := s.resetProfileTraffic(ids); err != nil {
		return nil, err
	}
	return connect.NewResponse(&lavrayv1.Empty{}), nil
}
