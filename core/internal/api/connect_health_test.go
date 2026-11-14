// SPDX-License-Identifier: GPL-3.0-only

package api

// connect_health_test.go — проверка связи после подключения (0.2.1): неработающий
// апстрим даёт предупреждение в статусе, журнале и потоке событий; рабочий — нет;
// успешный повтор и отключение снимают предупреждение.

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"connectrpc.com/connect"

	"github.com/anatolijlaptev1991-ctrl/lavray-android/core/internal/gen/lavrayv1"
	"github.com/anatolijlaptev1991-ctrl/lavray-android/core/internal/gen/lavrayv1connect"
	"github.com/anatolijlaptev1991-ctrl/lavray-android/core/internal/inst"
)

// healthEnv — сервер на FakeRunner с подставным соединением «через исходящее».
type healthEnv struct {
	srv    *Server
	runner *inst.FakeRunner
	pid    int64
	calls  atomic.Int32 // сколько раз проверка открыла соединение
}

func newHealthEnv(t *testing.T) *healthEnv {
	t.Helper()
	runner := &inst.FakeRunner{}
	s, pid := t55Server(t, runner)
	s.healthRetryDelay = 10 * time.Millisecond
	s.healthTimeout = time.Second
	// Адрес проверки — по http: подставное соединение не умеет TLS.
	if _, err := s.SetSettings(context.Background(), connect.NewRequest(&lavrayv1.SetSettingsRequest{
		Values: map[string]*lavrayv1.SettingValue{"test.url": {Value: &lavrayv1.SettingValue_StringValue{StringValue: "http://health.example.test/generate_204"}}},
	})); err != nil {
		t.Fatal(err)
	}
	return &healthEnv{srv: s, runner: runner, pid: pid}
}

// working — апстрим отвечает 204: соединение уходит на httptest-сервер.
func (e *healthEnv) working(t *testing.T) {
	t.Helper()
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	t.Cleanup(upstream.Close)
	e.runner.ProxyDial = func(ctx context.Context, _, _ string) (net.Conn, error) {
		e.calls.Add(1)
		return (&net.Dialer{}).DialContext(ctx, "tcp", upstream.Listener.Addr().String())
	}
}

// broken — апстрим рвёт соединения: ошибка в формате sing-box.
func (e *healthEnv) broken() {
	e.runner.ProxyDial = func(context.Context, string, string) (net.Conn, error) {
		e.calls.Add(1)
		return nil, errors.New("open connection to www.gstatic.com:443 using outbound/vmess[proxy]: EOF")
	}
}

func (e *healthEnv) connect(t *testing.T) {
	t.Helper()
	if _, err := e.srv.Connect(context.Background(), connect.NewRequest(&lavrayv1.ConnectRequest{ProfileId: int64p(e.pid)})); err != nil {
		t.Fatalf("Connect: %v", err)
	}
}

func (e *healthEnv) healthError() string { return e.srv.coreStatus().GetHealthError() }

func (e *healthEnv) journalHas(substr string) bool {
	for _, l := range e.srv.logs.Tail(200) {
		if strings.Contains(l.Message, substr) {
			return true
		}
	}
	return false
}

func waitUntil(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("не дождались: %s", what)
}

// Неработающий апстрим: статус несёт причину, в журнале строка, запрос повторён
// один раз (быстрый сбой).
func TestHealthCheckBrokenUpstreamRaisesWarning(t *testing.T) {
	e := newHealthEnv(t)
	e.broken()
	e.connect(t)
	waitUntil(t, "health_error в статусе", func() bool { return e.healthError() != "" })
	if got := e.healthError(); got != "reset" {
		t.Errorf("health_error = %q, ожидался код причины «reset» без адресов и тегов sing-box", got)
	}
	waitUntil(t, "строка в журнале", func() bool {
		return e.journalHas("проверка связи после подключения не прошла: сервер оборвал соединение")
	})
	if e.journalHas("gstatic") || e.journalHas("vmess[proxy]") {
		t.Error("в журнале адрес или тег исходящего")
	}
	if n := e.calls.Load(); n != 2 {
		t.Errorf("соединений открыто %d, ожидалось 2 (первая попытка и один повтор)", n)
	}
}

// Рабочий апстрим: предупреждения нет, в журнале — успех.
func TestHealthCheckWorkingUpstreamNoWarning(t *testing.T) {
	e := newHealthEnv(t)
	e.working(t)
	e.connect(t)
	waitUntil(t, "успех в журнале", func() bool {
		return e.journalHas("проверка связи после подключения прошла")
	})
	if got := e.healthError(); got != "" {
		t.Errorf("health_error = %q, ожидалось пусто", got)
	}
	if e.journalHas("не прошла") {
		t.Error("в журнале есть сообщение о неудаче при рабочем апстриме")
	}
	if n := e.calls.Load(); n != 1 {
		t.Errorf("соединений открыто %d, ожидалось 1", n)
	}
}

// Успешная проверка после перезапуска снимает предупреждение; отключение — тоже.
func TestHealthCheckWarningClearedByRestartAndDisconnect(t *testing.T) {
	e := newHealthEnv(t)
	e.broken()
	e.connect(t)
	waitUntil(t, "health_error", func() bool { return e.healthError() != "" })

	e.working(t)
	if _, err := e.srv.RestartProfile(context.Background(), connect.NewRequest(&lavrayv1.RestartProfileRequest{})); err != nil {
		t.Fatalf("RestartProfile: %v", err)
	}
	if got := e.healthError(); got != "" {
		t.Errorf("сразу после перезапуска health_error = %q: итог прежнего запуска не сброшен", got)
	}
	waitUntil(t, "успех после перезапуска", func() bool {
		return e.journalHas("проверка связи после подключения прошла")
	})
	if got := e.healthError(); got != "" {
		t.Errorf("после успешной проверки health_error = %q", got)
	}

	e.broken()
	if _, err := e.srv.RestartProfile(context.Background(), connect.NewRequest(&lavrayv1.RestartProfileRequest{})); err != nil {
		t.Fatal(err)
	}
	waitUntil(t, "health_error после неудачного перезапуска", func() bool { return e.healthError() != "" })
	if _, err := e.srv.Disconnect(context.Background(), connect.NewRequest(&lavrayv1.DisconnectRequest{})); err != nil {
		t.Fatal(err)
	}
	if got := e.healthError(); got != "" {
		t.Errorf("после отключения health_error = %q", got)
	}
}

// Таймаут: причина — код timeout, повтор не делается (ждать ещё столько же незачем).
func TestHealthCheckTimeoutReason(t *testing.T) {
	e := newHealthEnv(t)
	e.srv.healthTimeout = 100 * time.Millisecond
	e.runner.ProxyDial = func(ctx context.Context, _, _ string) (net.Conn, error) {
		e.calls.Add(1)
		<-ctx.Done()
		return nil, ctx.Err()
	}
	e.connect(t)
	waitUntil(t, "health_error", func() bool { return e.healthError() != "" })
	if got := e.healthError(); got != "timeout" {
		t.Errorf("health_error = %q, ожидался код «timeout»", got)
	}
	if n := e.calls.Load(); n != 1 {
		t.Errorf("соединений открыто %d, ожидалось 1 (по таймауту не повторяем)", n)
	}
}

// connect.health_check=false: проверка не выполняется.
func TestHealthCheckDisabledBySetting(t *testing.T) {
	e := newHealthEnv(t)
	if _, err := e.srv.SetSettings(context.Background(), connect.NewRequest(&lavrayv1.SetSettingsRequest{
		Values: map[string]*lavrayv1.SettingValue{"connect.health_check": {Value: &lavrayv1.SettingValue_BoolValue{BoolValue: false}}},
	})); err != nil {
		t.Fatal(err)
	}
	e.broken()
	e.connect(t)
	time.Sleep(150 * time.Millisecond)
	if n := e.calls.Load(); n != 0 {
		t.Errorf("проверка выполнена %d раз при выключенной настройке", n)
	}
	if got := e.healthError(); got != "" {
		t.Errorf("health_error = %q при выключенной проверке", got)
	}
}

// По умолчанию проверка включена (дескриптор настройки).
func TestHealthCheckEnabledByDefault(t *testing.T) {
	e := newHealthEnv(t)
	if !e.srv.settingBool(settingHealthCheck) {
		t.Fatal("connect.health_check должна быть включена по умолчанию")
	}
}

// Раннер без возможности открыть соединение (FakeRunner без ProxyDial, сборка
// без sing-box): проверка молча пропускается, ложного предупреждения нет.
func TestHealthCheckSkippedWhenRunnerCannotDial(t *testing.T) {
	e := newHealthEnv(t)
	e.runner.ProxyDial = nil
	e.connect(t)
	time.Sleep(150 * time.Millisecond)
	if got := e.healthError(); got != "" {
		t.Errorf("health_error = %q у раннера без возможности проверки", got)
	}
	if e.journalHas("проверка связи после подключения") {
		t.Error("в журнале есть запись о проверке, которой не было")
	}
}

// Поток событий: при неудаче приходят state_changed с health_error и
// предупреждение no_traffic; при успехе предупреждение не приходит.
func TestHealthCheckEvents(t *testing.T) {
	e := newHealthEnv(t)
	mux := http.NewServeMux()
	e.srv.Register(mux)
	hs := httptest.NewServer(mux)
	defer hs.Close()
	client := lavrayv1connect.NewCoreServiceClient(hs.Client(), hs.URL)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	stream, err := client.SubscribeEvents(ctx, connect.NewRequest(&lavrayv1.SubscribeEventsRequest{}))
	if err != nil {
		t.Fatal(err)
	}
	e.broken()
	e.connect(t)

	var gotWarning *lavrayv1.CoreWarning
	var gotStatusWithError bool
	for stream.Receive() {
		switch k := stream.Msg().Kind.(type) {
		case *lavrayv1.CoreEvent_StateChanged:
			if k.StateChanged.GetHealthError() != "" {
				gotStatusWithError = true
			}
		case *lavrayv1.CoreEvent_WarningRaised:
			gotWarning = k.WarningRaised
		}
		if gotWarning != nil {
			break
		}
	}
	if gotWarning == nil {
		t.Fatalf("предупреждение не пришло: %v", stream.Err())
	}
	if gotWarning.Code != "no_traffic" || !strings.Contains(gotWarning.Message, "сервер оборвал соединение") {
		t.Errorf("предупреждение: %+v", gotWarning)
	}
	if !gotStatusWithError {
		t.Error("перед предупреждением не пришёл state_changed с health_error")
	}
}

// healthReason: типовые сбои сведены к кодам, адреса и префиксы sing-box убраны,
// длинный текст обрезан.
func TestHealthReason(t *testing.T) {
	long := strings.Repeat("ж", maxHealthReason+40)
	cases := []struct {
		name string
		err  error
		want string
	}{
		{"EOF в обёртке sing-box", errors.New("open connection to a.example:443 using outbound/vless[proxy]: EOF"), "reset"},
		{"сброс соединения", errors.New("read tcp 10.0.0.2:5000->203.0.113.5:443: wsarecv: An existing connection was forcibly closed by the remote host."), "reset"},
		{"отказ в соединении", errors.New("dial tcp 203.0.113.5:443: connectex: No connection could be made because the target machine actively refused it."), "refused"},
		{"нет имени", errors.New("lookup nowhere.example: no such host"), "no_host"},
		{"url.Error разворачивается", &url.Error{Op: "Get", URL: "https://www.gstatic.com/generate_204", Err: errors.New("remote error: tls: bad certificate")}, "remote error: tls: bad certificate"},
		{"префикс dial убирается", errors.New("dial tcp 203.0.113.5:443: i/o weird"), "i/o weird"},
		{"таймаут", &probeTimeoutError{msg: "таймаут 8000 мс"}, "timeout"},
		{"таймаут в url.Error", &url.Error{Op: "Get", URL: "http://x/", Err: &probeTimeoutError{msg: "таймаут 8000 мс"}}, "timeout"},
		{"пустой текст", errors.New("   "), "timeout"},
		{"длинный текст обрезается", errors.New(long), strings.Repeat("ж", maxHealthReason) + "…"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := healthReason(c.err); got != c.want {
				t.Errorf("healthReason = %q, ожидалось %q", got, c.want)
			}
		})
	}
}
