// SPDX-License-Identifier: GPL-3.0-only

package api

// rpc_ratchet_test.go — «трещотка» нереализованных RPC. Каждая процедура
// lavray.proto вызывается с пустым запросом `{}` по сырому протоколу Connect;
// UNIMPLEMENTED допустим только для процедур из allowUnimplemented. Список
// может лишь сокращаться: реализовали заглушку — тест требует вычеркнуть её.
// Сеть не затрагивается: клиент подписок, набор номера, резолвер и фабрика
// временных инстансов заменены на отказ, сервер слушает только loopback.

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/netip"
	"strings"
	"testing"
	"time"

	"google.golang.org/protobuf/reflect/protoreflect"

	lavrayv1 "github.com/anatolijlaptev1991-ctrl/lavray-android/core/internal/gen/lavrayv1"
	"github.com/anatolijlaptev1991-ctrl/lavray-android/core/internal/inst"
)

// allowUnimplemented — процедуры, которые сейчас отвечают UNIMPLEMENTED.
// Реализуя процедуру, вычеркните её отсюда: тест не пускает «молчаливо
// реализованную» процедуру остаться в списке.
var allowUnimplemented = map[string]bool{
	// unimplemented.go (заглушки W6).
	// WebSessionService: ПОСТОЯННАЯ запись (T-76). Сервис включается только при
	// --web-bind (EnableWeb); десктоп без этого флага отвечает UNIMPLEMENTED
	// намеренно, и ratchet-сервер флага не имеет. Работающие методы проверяют
	// TestWebSessionServiceFlow и остальные websession_*_test.go с EnableWeb.
	// GatewayService из списка убран: на десктопе он отвечает INVALID_ARGUMENT.
	"/lavray.v1.WebSessionService/GetWebModeStatus": true,
	"/lavray.v1.WebSessionService/SetupFirstAdmin":  true,
	"/lavray.v1.WebSessionService/Login":            true,
	"/lavray.v1.WebSessionService/Logout":           true,
	"/lavray.v1.WebSessionService/RenewSession":     true,
	"/lavray.v1.WebSessionService/WhoAmI":           true,
	"/lavray.v1.WebSessionService/ChangePassword":   true,
	"/lavray.v1.WebSessionService/ListSessions":     true,
	"/lavray.v1.WebSessionService/RevokeSession":    true,
}

// rpcCallTimeout — предел одного вызова: бесконечный стрим без ошибки не
// считается «нереализованным».
const rpcCallTimeout = 2 * time.Second

// errOffline — отказ вместо любого выхода в сеть.
var errOffline = errors.New("rpc_ratchet_test: сеть в тесте запрещена")

// offlineRT — RoundTripper, который всегда возвращает ошибку.
type offlineRT struct{}

func (offlineRT) RoundTrip(*http.Request) (*http.Response, error) { return nil, errOffline }

// newOfflineRPCServer — сервер без выхода наружу: подписки, зонды, резолвер и
// временные инстансы тестов задержки отказывают, а не ходят в сеть.
func newOfflineRPCServer(t *testing.T) string {
	t.Helper()
	d := Deps{Runner: &inst.FakeRunner{}, HTTP: &http.Client{Transport: offlineRT{}}}
	_, httpSrv := newE2EServer(t, d, func(s *Server) {
		s.dial = func(context.Context, string, string) (net.Conn, error) { return nil, errOffline }
		s.lookupIP = func(context.Context, string) (netip.Addr, error) { return netip.Addr{}, errOffline }
		s.newTestInst = func(context.Context, []byte) (inst.TestDialer, error) { return nil, errOffline }
	})
	return httpSrv.URL
}

// rpcOutcome — итог одного вызова: код Connect ("ok" — без ошибки) либо признак
// того, что вызов не закончился за rpcCallTimeout.
type rpcOutcome struct {
	code     string
	timedOut bool
}

// callRaw вызывает процедуру с `{}` по протоколу Connect (unary — JSON,
// server-stream — connect+json с конвертом) и возвращает итог.
func callRaw(base, procedure string, streaming bool) (rpcOutcome, error) {
	ctx, cancel := context.WithTimeout(context.Background(), rpcCallTimeout)
	defer cancel()
	ctype, body := "application/json", []byte(`{}`)
	if streaming {
		ctype, body = "application/connect+json", envelope(0, []byte(`{}`))
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, base+procedure, bytes.NewReader(body))
	if err != nil {
		return rpcOutcome{}, err
	}
	req.Header.Set("Content-Type", ctype)
	req.Header.Set("Connect-Protocol-Version", "1")
	req.Header.Set("Accept-Encoding", "identity")
	resp, err := (&http.Client{}).Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return rpcOutcome{timedOut: true}, nil
		}
		return rpcOutcome{}, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK || !streaming {
		return unaryOutcome(ctx, resp)
	}
	return streamOutcome(ctx, resp.Body)
}

// envelope — кадр Connect-стрима: флаги, длина (big-endian, 4 байта), данные.
func envelope(flags byte, payload []byte) []byte {
	out := make([]byte, 5, 5+len(payload))
	out[0] = flags
	binary.BigEndian.PutUint32(out[1:], uint32(len(payload)))
	return append(out, payload...)
}

// unaryOutcome разбирает ответ unary (и ошибки до начала стрима): 200 — "ok",
// иначе в теле JSON {"code": "..."}.
func unaryOutcome(ctx context.Context, resp *http.Response) (rpcOutcome, error) {
	data, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return timedOrErr(ctx, err)
	}
	if resp.StatusCode == http.StatusOK {
		return rpcOutcome{code: "ok"}, nil
	}
	var e struct {
		Code string `json:"code"`
	}
	if err := json.Unmarshal(data, &e); err != nil || e.Code == "" {
		return rpcOutcome{}, fmt.Errorf("HTTP %d без кода Connect: %q", resp.StatusCode, data)
	}
	return rpcOutcome{code: e.Code}, nil
}

// streamOutcome читает кадры до end-stream (флаг 0x02) и берёт код из его
// error; end-stream без error — "ok". Кадры данных пропускаются.
func streamOutcome(ctx context.Context, r io.Reader) (rpcOutcome, error) {
	for {
		var head [5]byte
		if _, err := io.ReadFull(r, head[:]); err != nil {
			return timedOrErr(ctx, fmt.Errorf("стрим закрыт без end-stream: %w", err))
		}
		payload := make([]byte, binary.BigEndian.Uint32(head[1:]))
		if _, err := io.ReadFull(r, payload); err != nil {
			return timedOrErr(ctx, err)
		}
		if head[0]&0x02 == 0 {
			continue
		}
		var end struct {
			Error *struct {
				Code string `json:"code"`
			} `json:"error"`
		}
		if err := json.Unmarshal(payload, &end); err != nil {
			return rpcOutcome{}, fmt.Errorf("end-stream не JSON: %w", err)
		}
		if end.Error == nil {
			return rpcOutcome{code: "ok"}, nil
		}
		return rpcOutcome{code: end.Error.Code}, nil
	}
}

// timedOrErr: ошибка чтения при истёкшем контексте — таймаут, иначе сбой теста.
func timedOrErr(ctx context.Context, err error) (rpcOutcome, error) {
	if ctx.Err() != nil {
		return rpcOutcome{timedOut: true}, nil
	}
	return rpcOutcome{}, err
}

func TestUnimplementedRatchet(t *testing.T) {
	base := newOfflineRPCServer(t)
	known := map[string]bool{}
	services := lavrayv1.File_lavray_proto.Services()
	for i := 0; i < services.Len(); i++ {
		svc := services.Get(i)
		methods := svc.Methods()
		for j := 0; j < methods.Len(); j++ {
			m := methods.Get(j)
			procedure := procedureName(svc, m)
			known[procedure] = true
			if m.IsStreamingClient() {
				t.Fatalf("%s: клиентские стримы трещотка не умеет вызывать", procedure)
			}
			name := strings.TrimPrefix(strings.ReplaceAll(procedure, "/", "."), ".")
			t.Run(name, func(t *testing.T) { checkRatchet(t, base, procedure, m) })
		}
	}
	for procedure := range allowUnimplemented {
		if !known[procedure] {
			t.Errorf("allowUnimplemented: %q нет в lavray.proto, уберите запись", procedure)
		}
	}
}

// checkRatchet проверяет обе стороны трещотки для одной процедуры. Стримы идут
// параллельно: медленный стрим упирается в rpcCallTimeout, а не складывается.
func checkRatchet(t *testing.T, base, procedure string, m protoreflect.MethodDescriptor) {
	streaming := m.IsStreamingServer()
	if streaming {
		t.Parallel()
	}
	out, err := callRaw(base, procedure, streaming)
	if err != nil {
		t.Fatalf("%s: %v", procedure, err)
	}
	t.Logf("%s: code=%q timeout=%v", procedure, out.code, out.timedOut)
	unimplemented := out.code == "unimplemented"
	switch {
	case allowUnimplemented[procedure] && !unimplemented:
		t.Errorf("%s больше не UNIMPLEMENTED (ответ %q, таймаут %v): уберите её из allowUnimplemented",
			procedure, out.code, out.timedOut)
	case !allowUnimplemented[procedure] && unimplemented:
		t.Errorf("%s отвечает UNIMPLEMENTED, но её нет в allowUnimplemented: список только сокращается — реализуйте процедуру", procedure)
	}
}
