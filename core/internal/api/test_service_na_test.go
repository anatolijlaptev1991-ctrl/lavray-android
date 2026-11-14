// SPDX-License-Identifier: GPL-3.0-only

package api

import (
	"context"
	"net"
	"strings"
	"testing"

	"connectrpc.com/connect"
	lavrayv1 "github.com/anatolijlaptev1991-ctrl/lavray-android/core/internal/gen/lavrayv1"
)

// naPrefix — контракт с клиентом: LastError, начинающийся так, означает «тест
// не применим» (api.testNAPrefix; литерал нарочно, чтобы тест ловил и смену кода).
const naPrefix = "n/a: "

// T-23 (B1-10): TCP-пинг неприменим к Hysteria2 и TUIC. Живой UDP-сервер не
// должен получать «недоступен»: результат помечен кодом "n/a: ", без
// задержки, и сохраняется в профиле.

// naPingOne гоняет TestLatency(TCP_PING) по одному профилю и возвращает
// кадр результата.
func naPingOne(t *testing.T, cs *clients, id int64) *lavrayv1.TestResult {
	t.Helper()
	stream, err := cs.profile.TestLatency(context.Background(), connect.NewRequest(&lavrayv1.TestLatencyRequest{
		ProfileIds: []int64{id},
		Mode:       lavrayv1.TestLatencyRequest_MODE_TCP_PING,
	}))
	if err != nil {
		t.Fatal(err)
	}
	var res *lavrayv1.TestResult
	for stream.Receive() {
		res = stream.Msg().GetResult()
	}
	if err := stream.Err(); err != nil {
		t.Fatal(err)
	}
	if res == nil {
		t.Fatal("TestLatency не вернул ни одного кадра")
	}
	return res
}

func naCreateProfile(t *testing.T, cs *clients, p *lavrayv1.Profile) int64 {
	t.Helper()
	p.GroupId = 1
	out, err := cs.profile.CreateProfile(context.Background(), connect.NewRequest(&lavrayv1.CreateProfileRequest{Profile: p}))
	if err != nil {
		t.Fatal(err)
	}
	return out.Msg.Id
}

func TestTCPPingNotApplicableToQUICProtocols(t *testing.T) {
	// Живой UDP-порт: сервер «работает», но TCP на нём никто не слушает.
	udp, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer udp.Close()
	port := int32(udp.LocalAddr().(*net.UDPAddr).Port)

	cs := newE2E(t)
	pass := "secret"
	cases := []struct {
		name string
		p    *lavrayv1.Profile
	}{
		{"hysteria2", &lavrayv1.Profile{
			Type: lavrayv1.ProfileType_PROFILE_TYPE_HYSTERIA2, Name: "hy2", Address: "127.0.0.1", Port: port,
			Detail: &lavrayv1.Profile_Hysteria2{Hysteria2: &lavrayv1.Hysteria2Detail{Password: &pass}},
		}},
		{"tuic", &lavrayv1.Profile{
			Type: lavrayv1.ProfileType_PROFILE_TYPE_TUIC, Name: "tuic", Address: "127.0.0.1", Port: port,
			Detail: &lavrayv1.Profile_Tuic{Tuic: &lavrayv1.TuicDetail{Uuid: "b831381d-6324-4d53-ad4f-8cda48b30811", Password: &pass}},
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			id := naCreateProfile(t, cs, tc.p)
			res := naPingOne(t, cs, id)
			if res.LatencyMs != nil {
				t.Errorf("латентность %d у теста, который не применим", res.GetLatencyMs())
			}
			if !strings.HasPrefix(res.GetLastError(), naPrefix) {
				t.Fatalf("last_error = %q, ожидался код %q", res.GetLastError(), naPrefix)
			}
			if strings.Contains(res.GetLastError(), "connectex") || strings.Contains(res.GetLastError(), "refused") {
				t.Errorf("в результате сырая ошибка dial: %q", res.GetLastError())
			}
			if res.TestedAtMs == nil {
				t.Error("tested_at_ms не заполнен")
			}
			// Сохранённый профиль несёт тот же код (список серверов читает его).
			stored, err := cs.st.GetProfile(id)
			if err != nil {
				t.Fatal(err)
			}
			if !strings.HasPrefix(stored.TestResult.LastError, naPrefix) || stored.TestResult.LatencyMs != nil {
				t.Errorf("в профиле сохранено %+v", stored.TestResult)
			}
		})
	}
}

// TCP-протоколы по-прежнему измеряются настоящим connect: живой порт — задержка,
// закрытый — ошибка без кода n/a.
func TestTCPPingStillWorksForTCPProtocols(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	livePort := int32(ln.Addr().(*net.TCPAddr).Port)
	closed, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	deadPort := int32(closed.Addr().(*net.TCPAddr).Port)
	_ = closed.Close()

	cs := newE2E(t)
	mk := func(name string, port int32) int64 {
		pw := "p"
		return naCreateProfile(t, cs, &lavrayv1.Profile{
			Type: lavrayv1.ProfileType_PROFILE_TYPE_TROJAN, Name: name, Address: "127.0.0.1", Port: port,
			Detail: &lavrayv1.Profile_TrojanVless{TrojanVless: &lavrayv1.TrojanVlessDetail{Password: pw}},
		})
	}
	live := naPingOne(t, cs, mk("live", livePort))
	if live.LatencyMs == nil || live.GetLastError() != "" {
		t.Errorf("живой TCP-порт: %+v", live)
	}
	dead := naPingOne(t, cs, mk("dead", deadPort))
	if dead.GetLastError() == "" || strings.HasPrefix(dead.GetLastError(), naPrefix) {
		t.Errorf("закрытый TCP-порт: last_error = %q, ожидалась обычная ошибка", dead.GetLastError())
	}
}
