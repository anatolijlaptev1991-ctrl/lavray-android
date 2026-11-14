// SPDX-License-Identifier: GPL-3.0-only

// selfcheck.go — «Почему не работает?»: 8 проверок контракта (T-32). Каждая
// возвращает OK/WARN/FAIL либо UNSPECIFIED = «не проверено» с кодом причины в
// detail (UI переводит коды chk.d.*). Непроверенное никогда не выдаётся за «ок».
package api

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"strconv"
	"sync"
	"time"

	"connectrpc.com/connect"

	lavrayv1 "github.com/anatolijlaptev1991-ctrl/lavray-android/core/internal/gen/lavrayv1"
	"github.com/anatolijlaptev1991-ctrl/lavray-android/core/internal/inst"
	"github.com/anatolijlaptev1991-ctrl/lavray-android/core/internal/model"
	"github.com/anatolijlaptev1991-ctrl/lavray-android/core/internal/version"
)

type (
	checkKind   = lavrayv1.RunSelfCheckRequest_CheckKind
	checkResult = lavrayv1.SelfCheckResult
)

const (
	ckOK   = lavrayv1.SelfCheckResult_STATUS_OK
	ckWarn = lavrayv1.SelfCheckResult_STATUS_WARN
	ckFail = lavrayv1.SelfCheckResult_STATUS_FAIL
	ckSkip = lavrayv1.SelfCheckResult_STATUS_UNSPECIFIED
)

func verdict(st lavrayv1.SelfCheckResult_Status, detail, fix string) *checkResult {
	return &checkResult{Status: st, Detail: strpIf(detail), FixCode: fix}
}

// notChecked — проверка не выполнена; reason — код причины для UI.
func notChecked(reason string) *checkResult { return verdict(ckSkip, reason, "") }

// RunSelfCheck выполняет запрошенные проверки (пусто = все 8) параллельно;
// порядок результатов — порядок enum CheckKind.
func (s *Server) RunSelfCheck(ctx context.Context, req *connect.Request[lavrayv1.RunSelfCheckRequest]) (*connect.Response[lavrayv1.RunSelfCheckResponse], error) {
	probe := s.probeTunnel(ctx)
	checks := []struct {
		kind checkKind
		run  func() *checkResult
	}{
		{lavrayv1.RunSelfCheckRequest_CHECK_KIND_CORE_ALIVE, func() *checkResult { return s.checkCoreAlive() }},
		{lavrayv1.RunSelfCheckRequest_CHECK_KIND_SERVER_REACHABLE, func() *checkResult { return s.checkServer(ctx) }},
		{lavrayv1.RunSelfCheckRequest_CHECK_KIND_DNS_RESOLVES, func() *checkResult { return s.checkDNS(probe) }},
		{lavrayv1.RunSelfCheckRequest_CHECK_KIND_PROXY_ACTIVE, func() *checkResult { return s.checkProxy(probe) }},
		{lavrayv1.RunSelfCheckRequest_CHECK_KIND_CLOCK_SKEW, func() *checkResult { return s.checkClock(ctx) }},
		{lavrayv1.RunSelfCheckRequest_CHECK_KIND_SUBSCRIPTION_OK, func() *checkResult { return s.checkSubscription() }},
		{lavrayv1.RunSelfCheckRequest_CHECK_KIND_PORT_FREE, func() *checkResult { return s.checkPort(ctx) }},
		{lavrayv1.RunSelfCheckRequest_CHECK_KIND_IPV6, func() *checkResult { return s.checkIPv6(ctx) }},
	}
	want := map[checkKind]bool{}
	for _, k := range req.Msg.Kinds {
		want[k] = true
	}
	results := make([]*checkResult, len(checks))
	var wg sync.WaitGroup
	for i, c := range checks {
		if len(want) > 0 && !want[c.kind] {
			continue
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			results[i] = c.run()
			results[i].Kind = c.kind
		}()
	}
	wg.Wait()
	out := &lavrayv1.RunSelfCheckResponse{}
	for _, r := range results {
		if r != nil {
			out.Results = append(out.Results, r)
		}
	}
	return connect.NewResponse(out), nil
}

// tunnelProbe — два замера через mixed-in: без DNS (адрес-литерал) и с DNS (домен).
type tunnelProbe struct {
	up             bool
	trafficErr     error
	dnsErr         error
	listenHostPort string
}

func (s *Server) probeTunnel(ctx context.Context) tunnelProbe {
	c, ok := s.tunnelClient()
	if !ok || c == nil {
		return tunnelProbe{}
	}
	p := tunnelProbe{up: true}
	var wg sync.WaitGroup
	wg.Add(2)
	go func() { defer wg.Done(); p.trafficErr = s.headOK(ctx, c, s.ipProbeURL) }()
	go func() { defer wg.Done(); p.dnsErr = s.headOK(ctx, c, s.testURL()) }()
	wg.Wait()
	return p
}

// testURL — адрес проверки связи (настройка test.url).
func (s *Server) testURL() string { return s.settingStr("test.url") }

// headOK — запрос HEAD: успех = ответ получен и это не ошибка прокси (5xx).
func (s *Server) headOK(ctx context.Context, c *http.Client, url string) error {
	_, err := s.head(ctx, c, url)
	return err
}

func (s *Server) head(ctx context.Context, c *http.Client, url string) (*http.Response, error) {
	cctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(cctx, http.MethodHead, url, nil)
	if err != nil {
		return nil, err
	}
	resp, err := c.Do(req)
	if err != nil {
		return nil, err
	}
	resp.Body.Close()
	if resp.StatusCode >= 500 {
		return nil, fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	return resp, nil
}

func (s *Server) checkCoreAlive() *checkResult {
	st := s.inst.Status()
	if st.State == inst.StateCrashed {
		return verdict(ckFail, st.LastError, "")
	}
	return verdict(ckOK, version.Version, "")
}

func (s *Server) checkServer(ctx context.Context) *checkResult {
	st := s.inst.Status()
	if st.Run == nil || st.Run.ProfileID == 0 {
		return notChecked("no_connection")
	}
	p, err := s.st.GetProfile(st.Run.ProfileID)
	if err != nil || p.Address == "" || p.Port <= 0 {
		return notChecked("not_applicable")
	}
	if p.Type == model.TypeHysteria2 || p.Type == model.TypeTUIC {
		return notChecked("not_applicable") // UDP-протокол: TCP-проверка неприменима
	}
	r, _ := s.tcpPing(ctx, p, 3*time.Second)
	if r.LatencyMs == nil {
		return verdict(ckFail, r.LastError, "")
	}
	return verdict(ckOK, strconv.Itoa(int(*r.LatencyMs))+" ms", "")
}

func (s *Server) checkDNS(p tunnelProbe) *checkResult {
	switch {
	case !p.up:
		return notChecked("no_connection")
	case p.trafficErr != nil:
		return notChecked("blocked") // трафик не идёт вообще — вина не DNS
	case p.dnsErr != nil:
		return verdict(ckFail, p.dnsErr.Error(), "open_dns")
	}
	return verdict(ckOK, "", "")
}

func (s *Server) checkProxy(p tunnelProbe) *checkResult {
	switch {
	case s.inst.Status().State != inst.StateReady:
		return verdict(ckFail, "no_connection", "")
	case !p.up:
		return notChecked("not_applicable") // у запущенного инстанса нет mixed-in
	case p.trafficErr != nil:
		return verdict(ckFail, p.trafficErr.Error(), "")
	}
	return verdict(ckOK, p.listenHostPort, "")
}

// checkClock сравнивает часы с заголовком Date HTTPS-ответа test.url (через
// туннель, если он поднят). Недоступный сервер — «не проверено», а не «часы
// неверны»: сбой сети не доказывает сбой часов.
func (s *Server) checkClock(ctx context.Context) *checkResult {
	c := s.http
	if tc, ok := s.tunnelClient(); ok {
		c = tc
	}
	before := time.Now()
	resp, err := s.head(ctx, c, s.testURL())
	if err != nil {
		return notChecked("time_unreachable")
	}
	server, err := http.ParseTime(resp.Header.Get("Date"))
	if err != nil {
		return notChecked("time_unreachable")
	}
	local := before.Add(time.Since(before) / 2)
	skew := local.Sub(server)
	detail := fmt.Sprintf("%+.1f s", skew.Seconds())
	switch abs := skew.Abs(); {
	case abs < 5*time.Second:
		return verdict(ckOK, detail, "")
	case abs < 90*time.Second:
		return verdict(ckWarn, detail, "")
	}
	return verdict(ckFail, detail, "") // VMess и REALITY при таком сдвиге не подключаются
}

func (s *Server) checkSubscription() *checkResult {
	groups, err := s.st.ListGroups()
	if err != nil {
		return notChecked("not_applicable")
	}
	now := time.Now()
	status, detail, found := ckOK, "", false
	for _, g := range groups {
		if g.Kind != model.GroupSubscription || g.Archive {
			continue
		}
		found = true
		st, d := ckOK, g.Name
		if info := g.SubscriptionInfo; info != nil {
			if info.ExpireMs != nil && *info.ExpireMs > 0 {
				left := time.UnixMilli(*info.ExpireMs).Sub(now)
				if left <= 0 {
					st, d = ckFail, g.Name+" · "+time.UnixMilli(*info.ExpireMs).Format("2006-01-02")
				} else if left < 72*time.Hour {
					st, d = ckWarn, g.Name+" · "+time.UnixMilli(*info.ExpireMs).Format("2006-01-02")
				}
			}
			if info.TotalBytes != nil && *info.TotalBytes > 0 && st == ckOK {
				used := derefInt64(info.UploadBytes) + derefInt64(info.DownloadBytes)
				if used >= *info.TotalBytes {
					st, d = ckFail, g.Name+" · 100%"
				}
			}
		}
		if st > status || detail == "" {
			status, detail = st, d
		}
	}
	if !found {
		return notChecked("no_subscription")
	}
	fix := ""
	if status != ckOK {
		fix = "open_subscriptions"
	}
	return verdict(status, detail, fix)
}

func derefInt64(p *int64) int64 {
	if p == nil {
		return 0
	}
	return *p
}

// checkPort: запущенный инстанс должен слушать свой порт, остановленный —
// порт обязан быть свободен для следующего запуска.
func (s *Server) checkPort(ctx context.Context) *checkResult {
	if !s.hasLocalListener() {
		return notChecked("not_applicable") // LAVRay Android: локального порта нет
	}
	hostPort := ""
	if u, ok := s.tunnelProxyURL(); ok {
		hostPort = u.Host
	} else if in := s.buildSettings(false).Inbound; in.Port > 0 && in.Port <= 65535 {
		host := in.Address
		if host == "" {
			host = "127.0.0.1"
		}
		hostPort = net.JoinHostPort(host, strconv.Itoa(in.Port))
	} else {
		return verdict(ckFail, "port "+strconv.Itoa(in.Port), "change_port")
	}
	if s.inst.Status().State == inst.StateReady {
		conn, err := s.dial(ctx, "tcp", hostPort)
		if err != nil {
			return verdict(ckFail, err.Error(), "")
		}
		_ = conn.Close()
		return verdict(ckOK, hostPort, "")
	}
	ln, err := net.Listen("tcp", hostPort)
	if err != nil {
		return verdict(ckFail, err.Error(), "change_port")
	}
	_ = ln.Close()
	return verdict(ckOK, hostPort, "")
}

func (s *Server) checkIPv6(ctx context.Context) *checkResult {
	switch {
	case s.inst.Status().State != inst.StateReady:
		return notChecked("no_connection")
	case s.tunnelIsTUN():
		return notChecked("not_applicable") // в TUN сокеты ядра идут в туннель
	case s.ipv6DirectAvailable(ctx):
		return verdict(ckWarn, "v6_direct", "")
	}
	return verdict(ckOK, "", "")
}
