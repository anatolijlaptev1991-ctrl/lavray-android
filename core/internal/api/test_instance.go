// SPDX-License-Identifier: GPL-3.0-only

package api

// test_instance.go — URL-тест и полный тест через временный инстанс sing-box
// (T-84, FR-UI-201/202). На каждый профиль поднимается свой инстанс с одним
// исходящим профиля (inst.TestDialer), делаются замеры (test_probes.go) и
// инстанс закрывается сразу после проверки. Активный инстанс inst.Manager не
// затрагивается; число одновременных инстансов ограничено test.concurrent.

import (
	"context"
	"errors"
	"net"
	"net/netip"
	"strings"
	"time"

	"connectrpc.com/connect"
	"google.golang.org/protobuf/encoding/protojson"

	"github.com/anatolijlaptev1991-ctrl/lavray-android/core/internal/cfgbuild"
	lavrayv1 "github.com/anatolijlaptev1991-ctrl/lavray-android/core/internal/gen/lavrayv1"
	"github.com/anatolijlaptev1991-ctrl/lavray-android/core/internal/inst"
	"github.com/anatolijlaptev1991-ctrl/lavray-android/core/internal/model"
	"github.com/anatolijlaptev1991-ctrl/lavray-android/core/internal/presets"
)

const (
	// maxInstTestConcurrent — потолок одновременных тестовых инстансов: каждый
	// — отдельный box.Box со своей памятью.
	maxInstTestConcurrent = 16
	// defaultInstTestConcurrent — запас на случай испорченной настройки.
	defaultInstTestConcurrent = 5
	// defaultSpeedTimeoutSec — запас для испорченной test.dl_timeout.
	defaultSpeedTimeoutSec = 10
	// ipResolveTimeout — предел разбора имени сервера для «входного IP».
	ipResolveTimeout = 5 * time.Second
)

// instTestPlan — разобранный запрос URL- или полного теста: что мерить и чем.
type instTestPlan struct {
	full    bool // false — URL-тест
	timeout time.Duration
	url     string // задержка (URL-тест и full_latency)

	udp, speed, inOut bool // флаги полного теста (задержка — latency)
	latency           bool
	udpTarget         string
	speedURL          string
	speedTimeout      time.Duration
	ipURL             string

	concurrency int
	st          *cfgbuild.BuildSettings
	preset      *presets.Preset
}

// planInstanceTest разбирает запрос и проверяет его до любой работы: флаги
// полного теста, адреса (запрос → настройка → умолчание), параллелизм.
func (s *Server) planInstanceTest(msg *lavrayv1.TestLatencyRequest) (*instTestPlan, error) {
	plan := &instTestPlan{full: msg.Mode == lavrayv1.TestLatencyRequest_MODE_FULL_TEST}
	plan.timeout = time.Duration(defaultTestTimeoutMs) * time.Millisecond
	if ms := msg.GetTimeoutMs(); ms > 0 {
		plan.timeout = time.Duration(ms) * time.Millisecond
	}

	plan.latency = !plan.full || msg.GetFullLatency()
	if plan.full {
		plan.udp = msg.GetFullUdpLatency()
		plan.speed = msg.GetFullSpeed()
		plan.inOut = msg.GetFullInOutIp()
		if !plan.latency && !plan.udp && !plan.speed && !plan.inOut {
			return nil, invalidArg("полный тест: выберите хотя бы одну проверку (full_latency, full_udp_latency, full_speed, full_in_out_ip)")
		}
	}

	if plan.latency {
		url, err := s.testAddress("url", msg.Url, "test.url", validateTestURL)
		if err != nil {
			return nil, err
		}
		plan.url = url
	}
	if plan.udp {
		target, err := s.testAddress("test.udp_target", nil, "test.udp_target", validateUDPTarget)
		if err != nil {
			return nil, err
		}
		plan.udpTarget = target
	}
	if plan.speed {
		url, err := s.testAddress("full_speed_url", msg.FullSpeedUrl, "test.url_dl", validateTestURL)
		if err != nil {
			return nil, err
		}
		plan.speedURL = url
		sec := s.settingInt("test.dl_timeout")
		if sec <= 0 {
			sec = defaultSpeedTimeoutSec
		}
		plan.speedTimeout = time.Duration(sec) * time.Second
		if ms := msg.GetFullSpeedTimeoutMs(); ms > 0 {
			plan.speedTimeout = time.Duration(ms) * time.Millisecond
		}
	}
	if plan.inOut {
		url, err := s.testAddress("test.url_ip", nil, "test.url_ip", validateTestURL)
		if err != nil {
			return nil, err
		}
		plan.ipURL = url
	}

	plan.concurrency = int(msg.GetConcurrency())
	if plan.concurrency <= 0 {
		plan.concurrency = s.settingInt("test.concurrent")
	}
	if plan.concurrency <= 0 {
		plan.concurrency = defaultInstTestConcurrent
	}
	if plan.concurrency > maxInstTestConcurrent {
		plan.concurrency = maxInstTestConcurrent
	}

	// Настройки сборки и набор правил — один раз на весь батч.
	plan.st = s.buildSettings(false)
	preset, err := s.activePreset()
	if err != nil {
		return nil, mapErr(err)
	}
	plan.preset = preset
	return plan, nil
}

// testAddress возвращает адрес замера: значение из запроса, а если его нет —
// настройка key. Проверяет выбранное; name — как назвать поле в ошибке.
func (s *Server) testAddress(name string, fromRequest *string, key string, validate func(string) error) (string, error) {
	value, field := "", name
	if fromRequest != nil && strings.TrimSpace(*fromRequest) != "" {
		value = strings.TrimSpace(*fromRequest)
	} else {
		value, field = strings.TrimSpace(s.settingStr(key)), key
	}
	if err := validate(value); err != nil {
		return "", invalidArg("%s: %v", field, err)
	}
	return value, nil
}

// instTestNotApplicable возвращает причину, по которой профиль нельзя проверить
// через временный инстанс, либо "". Это не «сервер недоступен»: клиент
// показывает «н/д» (testNAPrefix).
func instTestNotApplicable(p *model.Profile) string {
	if p == nil {
		return ""
	}
	switch p.Type {
	case model.TypeNaive, model.TypeCustomExternal:
		return "профиль работает через внешнее ядро — URL- и полный тест неприменимы"
	case model.TypeCustomConfig:
		return "произвольный конфиг ядра не сводится к одному исходящему — URL- и полный тест неприменимы"
	}
	return ""
}

// testInstanceFactory — фабрика временных инстансов (в тестах подменяется).
func (s *Server) testInstanceFactory() inst.TestInstanceFactory {
	if s.newTestInst != nil {
		return s.newTestInst
	}
	return inst.NewTestInstance
}

// testConfigJSON собирает конфиг временного инстанса профиля: cfgbuild с
// ForTest (пустые inbounds, без пользовательских правил).
func (s *Server) testConfigJSON(p *model.Profile, plan *instTestPlan) ([]byte, error) {
	var group *model.Group
	if p.GroupID != 0 {
		g, err := s.st.GetGroup(p.GroupID)
		if err != nil {
			return nil, err
		}
		group = g
	}
	res, err := cfgbuild.Build(cfgbuild.BuildRequest{
		Profile:  p,
		Group:    group,
		Settings: plan.st,
		Lookup:   s.st.GetProfile,
		Preset:   plan.preset,
		ForTest:  true,
	})
	if err != nil {
		return nil, err
	}
	return cfgbuild.MarshalIndent(res.Config)
}

// instanceTest проверяет один профиль. aborted=true — измерение прервано
// отменой ctx (StopTests, закрытие стрима): результат тогда не сохраняется и
// не показывается (D-05).
func (s *Server) instanceTest(ctx context.Context, p *model.Profile, plan *instTestPlan) (res model.TestResult, aborted bool) {
	now := time.Now().UnixMilli()
	fail := func(msg string) (model.TestResult, bool) {
		return model.TestResult{TestedAtMs: &now, LastError: msg}, false
	}
	if why := instTestNotApplicable(p); why != "" {
		return fail(testNAPrefix + why)
	}
	if ctx.Err() != nil {
		return model.TestResult{LastError: ctx.Err().Error()}, true
	}
	cfg, err := s.testConfigJSON(p, plan)
	if err != nil {
		return fail(errorText(err)) // ошибка конфига — в колонку результата (FR-UI-208)
	}

	startCtx, cancel := context.WithTimeout(ctx, inst.StartTimeout)
	ti, err := s.testInstanceFactory()(startCtx, cfg)
	cancel()
	if err != nil {
		switch {
		case ctx.Err() != nil:
			return model.TestResult{LastError: ctx.Err().Error()}, true
		case errors.Is(err, inst.ErrTestInstanceUnsupported):
			return fail(testNAPrefix + "эта сборка ядра не содержит sing-box")
		}
		return fail("тестовый инстанс: " + errorText(err))
	}
	// Инстанс закрывается сразу после проверки (в том числе при отмене).
	defer func() {
		if err := ti.Close(); err != nil {
			s.logs.Logf("warn", "test", "закрытие тестового инстанса профиля %d: %v", p.ID, err)
		}
	}()

	if !plan.full {
		ms, err := probeURL(ctx, ti, plan.url, plan.timeout)
		if ctx.Err() != nil {
			return model.TestResult{LastError: ctx.Err().Error()}, true
		}
		if err != nil {
			return fail(err.Error())
		}
		return model.TestResult{LatencyMs: &ms, TestedAtMs: &now}, false
	}
	return s.fullTest(ctx, ti, p, plan, now)
}

// fullTest выполняет выбранные замеры по очереди (скорость последней: она
// загружает канал и исказила бы остальное) и собирает FullTestReport.
func (s *Server) fullTest(ctx context.Context, ti inst.TestDialer, p *model.Profile, plan *instTestPlan, now int64) (model.TestResult, bool) {
	rep := &lavrayv1.FullTestReport{}
	var failures []string
	ok := false
	// step выполняет замер; false — тест прерван отменой.
	step := func(name string, run func() error) bool {
		err := run()
		if ctx.Err() != nil {
			return false
		}
		if err != nil {
			failures = append(failures, name+": "+err.Error())
		} else {
			ok = true
		}
		return true
	}

	var latency *int32
	if plan.latency {
		if !step("задержка", func() error {
			ms, err := probeURL(ctx, ti, plan.url, plan.timeout)
			if err == nil {
				latency, rep.LatencyMs = &ms, &ms
			}
			return err
		}) {
			return model.TestResult{LastError: ctx.Err().Error()}, true
		}
	}
	if plan.udp {
		if !step("UDP", func() error {
			ms, err := probeUDP(ctx, ti, plan.udpTarget, plan.timeout)
			if err == nil {
				rep.UdpLatencyMs = &ms
			}
			return err
		}) {
			return model.TestResult{LastError: ctx.Err().Error()}, true
		}
	}
	if plan.inOut {
		if !step("IP", func() error {
			if in := s.ingressIP(ctx, p); in != "" {
				rep.IngressIp = &in
			}
			out, err := probeEgressIP(ctx, ti, plan.ipURL, plan.timeout)
			if err == nil {
				rep.EgressIp = &out
			} else if rep.IngressIp != nil {
				// Входной адрес известен без сети — это уже результат, а сбой
				// эхо-сервиса попадёт в отчёт отдельной строкой.
				failures = append(failures, "IP выхода: "+err.Error())
				return nil
			}
			return err
		}) {
			return model.TestResult{LastError: ctx.Err().Error()}, true
		}
	}
	if plan.speed {
		if !step("скорость", func() error {
			bps, err := probeSpeed(ctx, ti, plan.speedURL, plan.speedTimeout)
			if err == nil {
				rep.DownloadBps = &bps
			}
			return err
		}) {
			return model.TestResult{LastError: ctx.Err().Error()}, true
		}
	}

	res := model.TestResult{LatencyMs: latency, TestedAtMs: &now}
	if len(failures) > 0 {
		msg := strings.Join(failures, "; ")
		rep.Error = &msg
		if !ok {
			res.LastError = msg // ни один замер не удался — сервер «не отвечает»
		}
	}
	res.FullReport = marshalFullReport(rep)
	return res, false
}

// ingressIP — IP сервера, к которому подключается профиль (первое звено
// цепочки): адрес-литерал как есть, имя — разбором. Разбор имени — тот же
// запрос, который сделал бы сам sing-box при подключении, а не тестовый трафик.
func (s *Server) ingressIP(ctx context.Context, p *model.Profile) string {
	entry := p
	if d, ok := p.Detail.(*model.ChainDetail); ok && p.Type == model.TypeChain && len(d.ProfileIDs) > 0 {
		first, err := s.st.GetProfile(d.ProfileIDs[0]) // хранимый порядок: вход → выход
		if err != nil {
			return ""
		}
		entry = first
	}
	host := strings.TrimSpace(entry.Address)
	if host == "" {
		return ""
	}
	if a, err := netip.ParseAddr(host); err == nil {
		return a.String()
	}
	lookup := s.lookupIP
	if lookup == nil {
		lookup = defaultLookupIP
	}
	lctx, cancel := context.WithTimeout(ctx, ipResolveTimeout)
	defer cancel()
	if a, err := lookup(lctx, host); err == nil && a.IsValid() {
		return a.Unmap().String()
	}
	return ""
}

// defaultLookupIP — системный резолвер: первый адрес имени.
func defaultLookupIP(ctx context.Context, host string) (netip.Addr, error) {
	addrs, err := net.DefaultResolver.LookupNetIP(ctx, "ip", host)
	if err != nil {
		return netip.Addr{}, err
	}
	if len(addrs) == 0 {
		return netip.Addr{}, errors.New("адрес не найден")
	}
	return addrs[0], nil
}

// errorText — текст ошибки для колонки результата: у Connect-ошибки без кода.
func errorText(err error) string {
	if ce := new(connect.Error); errors.As(err, &ce) {
		return ce.Message()
	}
	return err.Error()
}

// marshalFullReport — FullTestReport → JSON для profiles.full_report (имена
// полей контракта, snake_case).
func marshalFullReport(rep *lavrayv1.FullTestReport) string {
	b, err := protojson.MarshalOptions{UseProtoNames: true}.Marshal(rep)
	if err != nil {
		return ""
	}
	return string(b)
}

// parseFullReport — JSON из profiles.full_report → FullTestReport. Пусто и
// не-JSON (текстовый отчёт NekoBox, перенесённый импортом) дают nil.
func parseFullReport(raw string) *lavrayv1.FullTestReport {
	raw = strings.TrimSpace(raw)
	if !strings.HasPrefix(raw, "{") {
		return nil
	}
	rep := &lavrayv1.FullTestReport{}
	if err := (protojson.UnmarshalOptions{DiscardUnknown: true}).Unmarshal([]byte(raw), rep); err != nil {
		return nil
	}
	return rep
}
