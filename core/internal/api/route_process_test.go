// SPDX-License-Identifier: GPL-3.0-only

package api

import (
	"context"
	"testing"

	"connectrpc.com/connect"

	lavrayv1 "github.com/anatolijlaptev1991-ctrl/lavray-android/core/internal/gen/lavrayv1"
)

func checkRoute(t *testing.T, s *Server, req *lavrayv1.CheckRouteRequest) (lavrayv1.Outbound, error) {
	t.Helper()
	r, err := s.CheckRoute(context.Background(), connect.NewRequest(req))
	if err != nil {
		return lavrayv1.Outbound_OUTBOUND_UNSPECIFIED, err
	}
	return r.Msg.Outbound, nil
}

// T-29 (B2-04): тестер принимает приложение без адреса. Правило процесса
// задано пользовательским custom-правилом, чтобы тест не зависел от ОС
// (процессные правила встроенных пресетов — только Windows).
func TestCheckRouteAcceptsProcessWithoutAddress(t *testing.T) {
	f := newSvcFixture(t, nil)
	s, ctx := f.srv, context.Background()
	custom := `{"rules":[{"process_name":["Zed.exe"],"outbound":"direct"}]}`
	if err := setSetting(t, s, "routing.custom_global", strSetting(custom)); err != nil {
		t.Fatal(err)
	}
	p := f.trojan(t)
	if _, err := s.Connect(ctx, connect.NewRequest(&lavrayv1.ConnectRequest{ProfileId: &p.ID})); err != nil {
		t.Fatal(err)
	}

	zed, other := "zed.exe", "other.exe"
	if got, err := checkRoute(t, s, &lavrayv1.CheckRouteRequest{ProcessName: &zed}); err != nil || got != lavrayv1.Outbound_OUTBOUND_DIRECT {
		t.Errorf("zed.exe без адреса = %v, %v; ожидался DIRECT", got, err)
	}
	if got, err := checkRoute(t, s, &lavrayv1.CheckRouteRequest{ProcessName: &other}); err != nil || got != lavrayv1.Outbound_OUTBOUND_PROXY {
		t.Errorf("other.exe без адреса = %v, %v; ожидался PROXY (route.final)", got, err)
	}
	// Ни адреса, ни процесса — по-прежнему ошибка пользователя.
	if _, err := checkRoute(t, s, &lavrayv1.CheckRouteRequest{}); connect.CodeOf(err) != connect.CodeInvalidArgument {
		t.Errorf("пустой запрос: ожидался INVALID_ARGUMENT, получено %v", err)
	}
	// Адрес без процесса работает как раньше.
	if _, err := checkRoute(t, s, &lavrayv1.CheckRouteRequest{Address: "example.org"}); err != nil {
		t.Errorf("адрес без процесса: %v", err)
	}
}
