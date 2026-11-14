// SPDX-License-Identifier: GPL-3.0-only

package api

import (
	"context"
	"net"
	"testing"

	"connectrpc.com/connect"
	lavrayv1 "github.com/anatolijlaptev1991-ctrl/lavray-android/core/internal/gen/lavrayv1"
	"github.com/anatolijlaptev1991-ctrl/lavray-android/core/internal/model"
)

// tcpProfile создаёт SOCKS-профиль на 127.0.0.1:port в группе 1.
func tcpProfile(t *testing.T, cs *clients, name string, port int) int64 {
	t.Helper()
	p := &model.Profile{
		GroupID: 1, Type: model.TypeSOCKS, Name: name, Address: "127.0.0.1", Port: port,
		Detail: &model.SocksHTTPDetail{Proto: "socks5"},
	}
	if err := cs.st.CreateProfile(p); err != nil {
		t.Fatal(err)
	}
	return p.ID
}

// T-22 (B1-09, C2-08): TestLatency сохраняет результат в профиле, а
// ClearTestResults действительно его очищает.
func TestTestLatencyPersistsResult(t *testing.T) {
	cs := newE2E(t)
	ctx := context.Background()

	live, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer live.Close()
	dead, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	deadPort := dead.Addr().(*net.TCPAddr).Port
	_ = dead.Close() // порт закрыт: соединение будет отвергнуто

	upID := tcpProfile(t, cs, "up", live.Addr().(*net.TCPAddr).Port)
	downID := tcpProfile(t, cs, "down", deadPort)

	timeout := int32(2000)
	stream, err := cs.profile.TestLatency(ctx, connect.NewRequest(&lavrayv1.TestLatencyRequest{
		ProfileIds: []int64{upID, downID},
		Mode:       lavrayv1.TestLatencyRequest_MODE_TCP_PING,
		TimeoutMs:  &timeout,
	}))
	if err != nil {
		t.Fatal(err)
	}
	frames := 0
	for stream.Receive() {
		frames++
	}
	if err := stream.Err(); err != nil || frames != 2 {
		t.Fatalf("стрим: frames=%d err=%v", frames, err)
	}

	up, err := cs.profile.GetProfile(ctx, connect.NewRequest(&lavrayv1.GetProfileRequest{Id: upID}))
	if err != nil {
		t.Fatal(err)
	}
	if r := up.Msg.TestResult; r == nil || r.LatencyMs == nil || *r.LatencyMs <= 0 || r.TestedAtMs == nil || r.GetLastError() != "" {
		t.Fatalf("живой сервер: результат не сохранён: %+v", r)
	}
	down, err := cs.profile.GetProfile(ctx, connect.NewRequest(&lavrayv1.GetProfileRequest{Id: downID}))
	if err != nil {
		t.Fatal(err)
	}
	if r := down.Msg.TestResult; r == nil || r.LatencyMs != nil || r.GetLastError() == "" {
		t.Fatalf("закрытый порт: ожидалась ошибка без задержки: %+v", r)
	}
	list, err := cs.profile.ListProfiles(ctx, connect.NewRequest(&lavrayv1.ListProfilesRequest{}))
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range list.Msg.Profiles {
		if p.Id == upID && (p.TestResult == nil || p.TestResult.LatencyMs == nil) {
			t.Fatalf("ListProfiles (его читает экран «Серверы») не видит задержку")
		}
	}

	if _, err := cs.profile.ClearTestResults(ctx, connect.NewRequest(&lavrayv1.ClearTestResultsRequest{
		Ids: []int64{upID, downID},
	})); err != nil {
		t.Fatal(err)
	}
	for _, id := range []int64{upID, downID} {
		p, err := cs.st.GetProfile(id)
		if err != nil {
			t.Fatal(err)
		}
		if tr := p.TestResult; tr.LatencyMs != nil || tr.TestedAtMs != nil || tr.LastError != "" {
			t.Fatalf("профиль %d: результат не очищен: %+v", id, tr)
		}
	}
}
