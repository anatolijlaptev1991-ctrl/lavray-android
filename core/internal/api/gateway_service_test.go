// SPDX-License-Identifier: GPL-3.0-only

package api

// gateway_service_test.go — GatewayService и режимы GATEWAY_* (T-76/Э-4). Настоящий
// nft не запускается: правила уходят в подставной Runner.

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"connectrpc.com/connect"

	"github.com/anatolijlaptev1991-ctrl/lavray-android/core/internal/gateway"
	lavrayv1 "github.com/anatolijlaptev1991-ctrl/lavray-android/core/internal/gen/lavrayv1"
)

// removalScript — скрипт снятия правил (gateway.removeScript): apply-скрипт тоже
// содержит строку delete table, поэтому снятие сверяется целиком.
const removalScript = "table inet lavray\ndelete table inet lavray\n"

// nftSpy запоминает скрипты, которые получил бы `nft -f -`.
type nftSpy struct {
	mu      sync.Mutex
	scripts []string
	err     error
}

func (n *nftSpy) Run(_ context.Context, script string) error {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.scripts = append(n.scripts, script)
	return n.err
}

func (n *nftSpy) last() string {
	n.mu.Lock()
	defer n.mu.Unlock()
	if len(n.scripts) == 0 {
		return ""
	}
	return n.scripts[len(n.scripts)-1]
}

func (n *nftSpy) count() int {
	n.mu.Lock()
	defer n.mu.Unlock()
	return len(n.scripts)
}

// gwFixture — десктопный сервер, превращённый в «роутер» через EnableGateway.
type gwFixture struct {
	*svcFixture
	nft *nftSpy
	gw  *gatewayRPC
}

func newGwFixture(t *testing.T) *gwFixture {
	t.Helper()
	f := &gwFixture{svcFixture: newSvcFixture(t, nil), nft: &nftSpy{}}
	cfg := gateway.Config{TProxyPort: 7893, Enabled: true}
	svc, err := gateway.NewService(cfg, gateway.NewPolicyRepo(f.srv.st), &gateway.NftApplier{Runner: f.nft}, nil)
	if err != nil {
		t.Fatal(err)
	}
	f.srv.EnableGateway(svc, cfg)
	f.gw = f.srv.ext.gateway
	return f
}

func (f *gwFixture) setPolicy(mac string, action lavrayv1.DevicePolicy_Action, preset string) (*lavrayv1.DevicePolicy, error) {
	p := &lavrayv1.DevicePolicy{Mac: mac, Action: action}
	if preset != "" {
		p.PresetId = &preset
	}
	res, err := f.gw.SetDevicePolicy(context.Background(), connect.NewRequest(&lavrayv1.SetDevicePolicyRequest{Policy: p}))
	if err != nil {
		return nil, err
	}
	return res.Msg, nil
}

// TestGatewayStatusAndPolicies — статус, добавление, замена и снятие политик (GE).
func TestGatewayStatusAndPolicies(t *testing.T) {
	f := newGwFixture(t)
	ctx := context.Background()
	st, err := f.gw.GetGatewayStatus(ctx, connect.NewRequest(&lavrayv1.Empty{}))
	if err != nil || st.Msg.Mode != lavrayv1.GatewayStatus_MODE_OFF || st.Msg.NftRulesActive || st.Msg.Ipv6Enabled || st.Msg.WatchdogTripped {
		t.Fatalf("начальный статус: %+v err=%v", st.Msg, err)
	}

	got, err := f.setPolicy("AA:BB:CC:DD:EE:10", lavrayv1.DevicePolicy_ACTION_DIRECT, "")
	if err != nil || got.Mac != "aa:bb:cc:dd:ee:10" || got.Action != lavrayv1.DevicePolicy_ACTION_DIRECT {
		t.Fatalf("SetDevicePolicy: %+v err=%v", got, err)
	}
	if _, err := f.setPolicy("aa:bb:cc:dd:ee:20", lavrayv1.DevicePolicy_ACTION_PRESET, "ru-direct"); err != nil {
		t.Fatal(err)
	}
	list, _ := f.gw.ListDevicePolicies(ctx, connect.NewRequest(&lavrayv1.Empty{}))
	if n := len(list.Msg.Policies); n != 2 || list.Msg.Policies[0].Mac != "aa:bb:cc:dd:ee:10" || list.Msg.Policies[1].GetPresetId() != "ru-direct" {
		t.Fatalf("ListDevicePolicies: %+v", list.Msg.Policies)
	}
	// Замена того же MAC не плодит записей; UNSPECIFIED = наследовать общую.
	_, _ = f.setPolicy("aa:bb:cc:dd:ee:10", lavrayv1.DevicePolicy_ACTION_PROXY, "")
	if _, err := f.setPolicy("aa:bb:cc:dd:ee:20", lavrayv1.DevicePolicy_ACTION_UNSPECIFIED, ""); err != nil {
		t.Fatal(err)
	}
	list, _ = f.gw.ListDevicePolicies(ctx, connect.NewRequest(&lavrayv1.Empty{}))
	if n := len(list.Msg.Policies); n != 1 || list.Msg.Policies[0].Action != lavrayv1.DevicePolicy_ACTION_PROXY {
		t.Fatalf("после замены и снятия: %+v", list.Msg.Policies)
	}
	// Политики лежат в настройках и переживают пересоздание сервиса.
	raw, ok, _ := f.srv.st.GetSetting(gateway.SettingPolicies)
	if !ok || !strings.Contains(raw, "aa:bb:cc:dd:ee:10") {
		t.Fatalf("политики не в настройках: %q", raw)
	}
}

// TestGatewayPolicyErrors — коды ошибок SetDevicePolicy (I).
func TestGatewayPolicyErrors(t *testing.T) {
	f := newGwFixture(t)
	cases := []struct {
		name   string
		mac    string
		action lavrayv1.DevicePolicy_Action
		preset string
	}{
		{"плохой MAC", "not-a-mac", lavrayv1.DevicePolicy_ACTION_DIRECT, ""},
		{"инъекция", "aa:bb:cc:dd:ee:00\nflush ruleset", lavrayv1.DevicePolicy_ACTION_DIRECT, ""},
		{"групповой", "01:00:5e:00:00:01", lavrayv1.DevicePolicy_ACTION_DIRECT, ""},
		{"пресет без id", "aa:bb:cc:dd:ee:30", lavrayv1.DevicePolicy_ACTION_PRESET, ""},
		{"неизвестное действие", "aa:bb:cc:dd:ee:30", lavrayv1.DevicePolicy_Action(99), ""},
	}
	for _, c := range cases {
		_, err := f.setPolicy(c.mac, c.action, c.preset)
		if connect.CodeOf(err) != connect.CodeInvalidArgument {
			t.Errorf("%s: %v, ждали INVALID_ARGUMENT", c.name, err)
		}
	}
	if _, err := f.gw.SetDevicePolicy(context.Background(), connect.NewRequest(&lavrayv1.SetDevicePolicyRequest{})); connect.CodeOf(err) != connect.CodeInvalidArgument {
		t.Errorf("без policy: %v", err)
	}
	if f.nft.count() != 0 {
		t.Error("правила применены ради недопустимых политик")
	}
}

// TestDesktopGatewayInvalidArgument — без --platform openwrt все методы шлюза и
// режимы GATEWAY_* отвечают INVALID_ARGUMENT, а не UNIMPLEMENTED (P).
func TestDesktopGatewayInvalidArgument(t *testing.T) {
	base := newOfflineRPCServer(t)
	for _, m := range []string{"GetGatewayStatus", "ListDevicePolicies", "SetDevicePolicy"} {
		out, err := callRaw(base, "/lavray.v1.GatewayService/"+m, false)
		if err != nil || out.code != "invalid_argument" {
			t.Errorf("%s: code=%q err=%v, ждали invalid_argument", m, out.code, err)
		}
	}
	f := newSvcFixture(t, nil)
	p := f.trojan(t)
	for _, mode := range []lavrayv1.ConnectMode{lavrayv1.ConnectMode_CONNECT_MODE_GATEWAY_TPROXY, lavrayv1.ConnectMode_CONNECT_MODE_GATEWAY_TUN} {
		for _, validate := range []bool{false, true} {
			req := &lavrayv1.ConnectRequest{ProfileId: &p.ID, Mode: mode, ValidateOnly: &validate}
			_, err := f.srv.Connect(context.Background(), connect.NewRequest(req))
			if connect.CodeOf(err) != connect.CodeInvalidArgument {
				t.Errorf("режим %v validate=%v на десктопе: %v", mode, validate, err)
			}
		}
	}
	if f.runner.StartedCounter != 0 {
		t.Error("режим шлюза на десктопе запустил инстанс")
	}
}

// waitFor ждёт условие до секунды.
func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("не дождались: %s", what)
		}
		time.Sleep(2 * time.Millisecond)
	}
}

// TestGatewayConnectAppliesRulesAndDisconnectRemoves — Connect в режиме
// GATEWAY_TPROXY собирает tproxy-вход и ставит правила nft, Disconnect и выход
// процесса их снимают (R).
func TestGatewayConnectAppliesRulesAndDisconnectRemoves(t *testing.T) {
	f := newGwFixture(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go f.srv.RunGateway(ctx)
	waitFor(t, "снятие осиротевших правил при старте", func() bool { return f.nft.count() >= 1 })
	if f.nft.last() != removalScript {
		t.Fatalf("при старте ждали снятие правил: %q", f.nft.last())
	}

	p := f.trojan(t)
	_, err := f.srv.Connect(ctx, connect.NewRequest(&lavrayv1.ConnectRequest{ProfileId: &p.ID, Mode: lavrayv1.ConnectMode_CONNECT_MODE_GATEWAY_TPROXY}))
	if err != nil {
		t.Fatalf("Connect в режиме шлюза: %v", err)
	}
	if cfg := string(f.runner.LastConfig()); !strings.Contains(cfg, `"tproxy-in"`) || !strings.Contains(cfg, "7893") {
		t.Fatalf("в конфиге нет tproxy-in на порту шлюза:\n%s", cfg)
	}
	waitFor(t, "правила nft", func() bool { return strings.Contains(f.nft.last(), "tproxy ip to :7893") })
	st, _ := f.gw.GetGatewayStatus(ctx, connect.NewRequest(&lavrayv1.Empty{}))
	if st.Msg.Mode != lavrayv1.GatewayStatus_MODE_TPROXY || !st.Msg.NftRulesActive {
		t.Fatalf("статус в режиме шлюза: %+v", st.Msg)
	}

	// Политика применяется к уже работающему шлюзу.
	if _, err := f.setPolicy("aa:bb:cc:dd:ee:10", lavrayv1.DevicePolicy_ACTION_DIRECT, ""); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(f.nft.last(), "ether saddr aa:bb:cc:dd:ee:10 return") {
		t.Fatal("политика не попала в работающие правила")
	}

	if _, err := f.srv.Disconnect(ctx, connect.NewRequest(&lavrayv1.DisconnectRequest{})); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "снятие правил после отключения", func() bool { return f.nft.last() == removalScript })
	st, _ = f.gw.GetGatewayStatus(ctx, connect.NewRequest(&lavrayv1.Empty{}))
	if st.Msg.Mode != lavrayv1.GatewayStatus_MODE_OFF || st.Msg.NftRulesActive {
		t.Fatalf("статус после отключения: %+v", st.Msg)
	}
}

// TestGatewayRulesRemovedOnProcessExit — StopInstance снимает правила синхронно,
// не дожидаясь события.
func TestGatewayRulesRemovedOnProcessExit(t *testing.T) {
	f := newGwFixture(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go f.srv.RunGateway(ctx)
	p := f.trojan(t)
	if _, err := f.srv.Connect(ctx, connect.NewRequest(&lavrayv1.ConnectRequest{ProfileId: &p.ID, Mode: lavrayv1.ConnectMode_CONNECT_MODE_GATEWAY_TPROXY})); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "правила nft", func() bool { return f.gw.svc.Status().Active })
	if err := f.srv.StopInstance(ctx); err != nil {
		t.Fatal(err)
	}
	if f.gw.svc.Status().Active || f.nft.last() != removalScript {
		t.Fatal("после StopInstance правила остались")
	}
}
