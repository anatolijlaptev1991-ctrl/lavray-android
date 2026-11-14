// SPDX-License-Identifier: GPL-3.0-only

//go:build singbox_run

package api

// connect_realrunner_test.go — подключение на настоящем sing-box (0.2.1):
// проверка связи после подключения идёт через исходящее профиля, а занятый порт
// входа — это ошибка старта, а не «Подключено». Все порты — свободные порты
// loopback; порт 2080 (NekoBox) тесты не занимают и не используют.

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"connectrpc.com/connect"

	"github.com/anatolijlaptev1991-ctrl/lavray-android/core/internal/gen/lavrayv1"
	"github.com/anatolijlaptev1991-ctrl/lavray-android/core/internal/inst"
	"github.com/anatolijlaptev1991-ctrl/lavray-android/core/internal/model"
	"github.com/anatolijlaptev1991-ctrl/lavray-android/core/internal/testsocks"
)

// realRunnerEnv — сервер на настоящем раннере, свободный порт входа и SOCKS5-
// профиль на заданный порт сервера.
func realRunnerEnv(t *testing.T, socksPort int) (*Server, int64) {
	t.Helper()
	s, _ := t55Server(t, inst.NewSingBoxRunner())
	t.Cleanup(func() { _, _ = s.Disconnect(context.Background(), connect.NewRequest(&lavrayv1.DisconnectRequest{})) })
	t55SetInboundPort(t, s, int64(freeLoopbackPort(t)))
	// Встроенные пресеты ссылаются на наборы правил geosite (.srs), а каталога
	// geo в тестовой среде нет. Пустой пользовательский набор — без них.
	empty, err := s.CreatePreset(context.Background(), connect.NewRequest(&lavrayv1.CreatePresetRequest{
		Preset: &lavrayv1.RoutingPreset{DisplayName: "empty", DefaultOutbound: lavrayv1.Outbound_OUTBOUND_PROXY},
	}))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.ActivatePreset(context.Background(), connect.NewRequest(&lavrayv1.ActivatePresetRequest{Id: empty.Msg.Id})); err != nil {
		t.Fatal(err)
	}
	p := &model.Profile{
		GroupID: 1, Type: model.TypeSOCKS, Name: "socks", Address: "127.0.0.1", Port: socksPort,
		Detail: &model.SocksHTTPDetail{Proto: "socks5"},
	}
	if err := s.st.CreateProfile(p); err != nil {
		t.Fatal(err)
	}
	return s, p.ID
}

func freeLoopbackPort(t *testing.T) int {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	return ln.Addr().(*net.TCPAddr).Port
}

func journalContains(s *Server, substr string) bool {
	for _, l := range s.logs.Tail(300) {
		if strings.Contains(l.Message, substr) {
			return true
		}
	}
	return false
}

// Рабочий сервер профиля: проверка идёт через его исходящее (SOCKS-сервер видит
// цель), предупреждения нет.
func TestHealthCheckRealRunnerWorkingServer(t *testing.T) {
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	defer target.Close()
	socks := testsocks.Start(t)
	s, pid := realRunnerEnv(t, socks.Port())
	setStrSetting(t, s, "test.url", target.URL+"/generate_204")

	if _, err := s.Connect(context.Background(), connect.NewRequest(&lavrayv1.ConnectRequest{ProfileId: int64p(pid)})); err != nil {
		t.Fatalf("Connect: %v", err)
	}
	waitUntil(t, "успех проверки связи", func() bool {
		return journalContains(s, "проверка связи после подключения прошла")
	})
	if got := s.coreStatus().GetHealthError(); got != "" {
		t.Errorf("health_error = %q при рабочем сервере", got)
	}
	dests := socks.TCPDestinations()
	if len(dests) != 1 || dests[0] != target.Listener.Addr().String() {
		t.Errorf("SOCKS-сервер профиля увидел цели %v, ожидалась %s", dests, target.Listener.Addr())
	}
}

// Сервер профиля не отвечает (порт закрыт): инстанс стартует, статус
// «Подключено», но проверка связи сообщает причину.
func TestHealthCheckRealRunnerDeadServer(t *testing.T) {
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	defer target.Close()
	s, pid := realRunnerEnv(t, freeLoopbackPort(t)) // на этом порту никто не слушает
	s.healthRetryDelay = 0
	setStrSetting(t, s, "test.url", target.URL+"/generate_204")

	if _, err := s.Connect(context.Background(), connect.NewRequest(&lavrayv1.ConnectRequest{ProfileId: int64p(pid)})); err != nil {
		t.Fatalf("Connect: %v", err)
	}
	if st := s.coreStatus(); st.State != lavrayv1.CoreState_CORE_STATE_CONNECTED {
		t.Fatalf("состояние %v: ядро стартует и при мёртвом сервере", st.State)
	}
	waitUntil(t, "health_error", func() bool { return s.coreStatus().GetHealthError() != "" })
	if got := s.coreStatus().GetHealthError(); got != "refused" {
		t.Errorf("причина %q, ожидался код «refused» (без адреса сервера)", got)
	}
	if !journalContains(s, "проверка связи после подключения не прошла") {
		t.Error("в журнале нет строки о неудачной проверке")
	}
}

// Порт входа занят другой программой (NekoBox): Connect завершается ошибкой
// старта с понятным текстом, состояние — не «Подключено».
func TestConnectBusyInboundPortRealRunner(t *testing.T) {
	busy, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer busy.Close()
	busyPort := busy.Addr().(*net.TCPAddr).Port
	s, pid := realRunnerEnv(t, freeLoopbackPort(t))
	t55SetInboundPort(t, s, int64(busyPort))

	_, err = s.Connect(context.Background(), connect.NewRequest(&lavrayv1.ConnectRequest{ProfileId: int64p(pid)}))
	if connect.CodeOf(err) != connect.CodeFailedPrecondition {
		t.Fatalf("код %v (err=%v), ожидался FAILED_PRECONDITION", connect.CodeOf(err), err)
	}
	for _, want := range []string{"занят", "NekoBox", "Настройки"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("в сообщении нет %q: %v", want, err)
		}
	}
	if st := s.coreStatus(); st.State == lavrayv1.CoreState_CORE_STATE_CONNECTED {
		t.Errorf("состояние %v: при занятом порте «Подключено» быть не должно", st.State)
	}
	if s.inst.Status().Run != nil {
		t.Error("после ошибки старта остался активный запуск")
	}
}
