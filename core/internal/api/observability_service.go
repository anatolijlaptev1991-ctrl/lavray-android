// SPDX-License-Identifier: GPL-3.0-only

// observability_service.go — LogService + StatsService + DiagnosticsService
// (W7). ConnectionService — connection_service.go. Тонкие обёртки над
// logs.Mux / inst-событиями / cfgbuild dry-run; источники статистики и
// соединений — за интерфейсами (clash-api инстанса — W8-релиз; тесты —
// фейковые источники).
package api

import (
	"context"
	"strings"
	"time"

	"connectrpc.com/connect"
	"github.com/anatolijlaptev1991-ctrl/lavray-android/core/internal/cfgbuild"
	lavrayv1 "github.com/anatolijlaptev1991-ctrl/lavray-android/core/internal/gen/lavrayv1"
	"github.com/anatolijlaptev1991-ctrl/lavray-android/core/internal/inst"
	"github.com/anatolijlaptev1991-ctrl/lavray-android/core/internal/logs"
)

// ---- LogService -----------------------------------------------------------------

func logLevelToProto(l string) lavrayv1.LogLevel {
	switch l {
	case logs.LevelTrace:
		return lavrayv1.LogLevel_LOG_LEVEL_TRACE
	case logs.LevelDebug:
		return lavrayv1.LogLevel_LOG_LEVEL_DEBUG
	case logs.LevelWarn:
		return lavrayv1.LogLevel_LOG_LEVEL_WARN
	case logs.LevelError, logs.LevelPanic:
		return lavrayv1.LogLevel_LOG_LEVEL_ERROR
	default:
		return lavrayv1.LogLevel_LOG_LEVEL_INFO
	}
}

// utf8Repl — замена невалидных байт: proto3 string с невалидным UTF-8 не
// маршалится, и один такой кадр оборвал бы весь поток журнала.
const utf8Repl = "?"

func logLineToProto(l logs.Line) *lavrayv1.LogLine {
	return &lavrayv1.LogLine{
		TsMs:    l.TsMs,
		Level:   logLevelToProto(l.Level),
		Source:  strings.ToValidUTF8(l.Source, utf8Repl),
		Message: strings.ToValidUTF8(l.Message, utf8Repl),
	}
}

func (s *Server) GetTail(ctx context.Context, req *connect.Request[lavrayv1.GetTailRequest]) (*connect.Response[lavrayv1.GetTailResponse], error) {
	n := int(req.Msg.Lines)
	if n <= 0 {
		n = 200
	}
	if n > 2000 {
		n = 2000
	}
	lines := s.logs.Tail(n)
	out := make([]*lavrayv1.LogLine, 0, len(lines))
	for _, l := range lines {
		out = append(out, logLineToProto(l))
	}
	return connect.NewResponse(&lavrayv1.GetTailResponse{Lines: out}), nil
}

func (s *Server) Subscribe(ctx context.Context, req *connect.Request[lavrayv1.SubscribeLogsRequest], stream *connect.ServerStream[lavrayv1.LogLine]) error {
	msg := req.Msg
	tailN := 0
	if msg.TailLines != nil {
		tailN = min(int(*msg.TailLines), 2000)
	}
	// T-70: tail_lines — «сколько истории отдать сразу (0 = нет)», те же
	// фильтры, что у живого потока. История и регистрация подписчика берутся
	// атомарно (Mux.Open): строка на стыке не дублируется и не теряется.
	keep := logFilter(msg)
	sub := s.logs.Open(256, tailN, keep)
	defer sub.Close()
	for _, l := range sub.History {
		if err := stream.Send(logLineToProto(l)); err != nil {
			return err
		}
	}
	maxPS := 0
	if msg.MaxLinesPerSecond != nil {
		maxPS = int(*msg.MaxLinesPerSecond)
	}
	tick := time.NewTicker(time.Second)
	defer tick.Stop()
	return pumpLogs(ctx, sub, logs.NewFloodGate(maxPS), time.Now, tick.C, keep, func(l logs.Line) error {
		return stream.Send(logLineToProto(l))
	})
}

// ---- StatsService -----------------------------------------------------------------

func (s *Server) GetTrafficTotals(ctx context.Context, req *connect.Request[lavrayv1.GetTrafficTotalsRequest]) (*connect.Response[lavrayv1.GetTrafficTotalsResponse], error) {
	s.flushAccounting() // T-79: идущая сессия видна сразу, а не после таймера
	totals, err := s.trafficTotals(req.Msg.ProfileIds)
	if err != nil {
		return nil, mapErr(err)
	}
	return connect.NewResponse(&lavrayv1.GetTrafficTotalsResponse{Totals: totals}), nil
}

// ---- DiagnosticsService --------------------------------------------------------------

func (s *Server) CheckRoute(ctx context.Context, req *connect.Request[lavrayv1.CheckRouteRequest]) (*connect.Response[lavrayv1.CheckRouteResponse], error) {
	msg := req.Msg
	addr := strings.TrimSpace(msg.Address)
	// T-29: достаточно адреса ИЛИ имени процесса (приложение без адреса).
	proc := strings.TrimSpace(msg.GetProcessName())
	if addr == "" && proc == "" {
		return nil, invalidArg("нужен address или process_name")
	}
	// T-30: правила берутся из того же routeRules, что строит конфиг
	// (rule_set, ip_*, тег bypass); запущенный профиль не нужен. В режиме TUN
	// учитываются TUN-списки исключений.
	preset, err := s.activePreset()
	if err != nil {
		return nil, mapErr(err)
	}
	table, err := cfgbuild.BuildRouteTable(preset, s.buildSettings(s.tunnelIsTUN()))
	if err != nil {
		return nil, mapErr(err)
	}
	v := table.Evaluate(cfgbuild.ParseRouteQuery(addr, proc), s.ruleSetLookup(table))
	if len(v.Unresolved) > 0 {
		s.logs.Logf(logs.LevelWarn, logs.SourceCore, "тестер маршрута: не вычислено %v", v.Unresolved)
	}
	resp := &lavrayv1.CheckRouteResponse{Outbound: outboundToProto(v.Outbound), MatchedList: strp(v.List)}
	if v.Index >= 0 {
		resp.MatchedRule = strp(v.Rule + " → " + v.Outbound)
	}
	return connect.NewResponse(resp), nil
}

// GetPublicIp — «каков мой адрес наружу». Поле via выбирает путь запроса
// (T-16): THROUGH_TUNNEL — через mixed-in работающего инстанса (туннель не
// поднят → FAILED_PRECONDITION), DIRECT — напрямую, UNSPECIFIED — через
// туннель, если соединение активно, иначе напрямую.
func (s *Server) GetPublicIp(ctx context.Context, req *connect.Request[lavrayv1.GetPublicIpRequest]) (*connect.Response[lavrayv1.GetPublicIpResponse], error) {
	client, err := s.httpVia(req.Msg.Via)
	if err != nil {
		return nil, err
	}
	ip, err := s.fetchPublicIP(ctx, client)
	if err != nil {
		return nil, unavailable(err)
	}
	return connect.NewResponse(&lavrayv1.GetPublicIpResponse{Ip: strpIf(ip.String()), IsIpv6: ip.Is6()}), nil
}

// CheckLeaks — IP напрямую и через туннель, прямой IPv6 (FR-UI-273). Что не
// измерено, перечислено в LeakReport.unchecked: пустое поле не означает
// «в порядке». DNS-эгресс не измеряется никогда: контролируемого тест-домена,
// который показал бы адрес резолвера, у приложения нет.
func (s *Server) CheckLeaks(ctx context.Context, req *connect.Request[lavrayv1.Empty]) (*connect.Response[lavrayv1.LeakReport], error) {
	out := &lavrayv1.LeakReport{
		TunnelActive: s.inst.Status().State == inst.StateReady,
		Unchecked:    []string{"dns"},
	}
	tunnel, up := s.tunnelClient()
	if up {
		if ip, err := s.fetchPublicIP(ctx, tunnel); err == nil {
			out.TunnelIp = strpIf(ip.String())
		}
	}
	if out.TunnelIp == nil {
		out.Unchecked = append(out.Unchecked, "tunnel_ip")
	}
	if s.directIsTunneled() {
		out.Unchecked = append(out.Unchecked, "direct_ip", "ipv6")
		return connect.NewResponse(out), nil
	}
	if ip, err := s.fetchPublicIP(ctx, s.http); err == nil {
		out.DirectIp = strpIf(ip.String())
	} else {
		out.Unchecked = append(out.Unchecked, "direct_ip")
	}
	if up && s.ipv6Measurable() {
		out.Ipv6DirectAvailable = s.ipv6DirectAvailable(ctx)
	} else {
		out.Unchecked = append(out.Unchecked, "ipv6")
	}
	return connect.NewResponse(out), nil
}
