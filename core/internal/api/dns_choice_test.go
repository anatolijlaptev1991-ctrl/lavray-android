// SPDX-License-Identifier: GPL-3.0-only

package api

import (
	"context"
	"encoding/json"
	"testing"

	"connectrpc.com/connect"

	lavrayv1 "github.com/anatolijlaptev1991-ctrl/lavray-android/core/internal/gen/lavrayv1"
	"github.com/anatolijlaptev1991-ctrl/lavray-android/core/internal/model"
)

func setDNS(t *testing.T, s *Server, choice string, remote *string) error {
	t.Helper()
	d := &lavrayv1.DnsSettings{Mode: lavrayv1.DnsSettings_MODE_SIMPLE, RemoteDns: remote}
	if choice != "" {
		d.Choice = &choice
	}
	_, err := s.SetDns(context.Background(), connect.NewRequest(&lavrayv1.SetDnsRequest{Dns: d}))
	return err
}

func getDNS(t *testing.T, s *Server) *lavrayv1.DnsSettings {
	t.Helper()
	r, err := s.GetDns(context.Background(), connect.NewRequest(&lavrayv1.GetDnsRequest{}))
	if err != nil {
		t.Fatal(err)
	}
	return r.Msg
}

func activate(t *testing.T, s *Server, id string) {
	t.Helper()
	if _, err := s.ActivatePreset(context.Background(), connect.NewRequest(&lavrayv1.ActivatePresetRequest{Id: id})); err != nil {
		t.Fatal(err)
	}
}

// dnsConfig — секция dns собранного конфига (JSON-круг: как её увидит sing-box).
func dnsConfig(t *testing.T, s *Server, p *model.Profile) (remote map[string]any, rules []any) {
	t.Helper()
	res, err := s.dryRunBuild(p, lavrayv1.ConnectMode_CONNECT_MODE_SYSTEM_PROXY)
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(res.Config["dns"])
	var dns struct {
		Servers []map[string]any `json:"servers"`
		Rules   []any            `json:"rules"`
	}
	if err := json.Unmarshal(raw, &dns); err != nil {
		t.Fatal(err)
	}
	for _, srv := range dns.Servers {
		if srv["tag"] == "dns-remote" {
			remote = srv
		}
	}
	return remote, dns.Rules
}

// T-31 (B2-06): «Авто», «Через туннель» и «DoH» дают разные конфиги.
func TestDnsCardsProduceDifferentConfigs(t *testing.T) {
	f := newSvcFixture(t, nil)
	s := f.srv
	p := f.trojan(t)
	activate(t, s, "lav-ru") // раздельный DNS: RU у провайдера, остальное — DoH

	autoRemote, autoRules := dnsConfig(t, s, p)
	if autoRemote["detour"] != "proxy" || len(autoRules) <= 2 {
		t.Fatalf("Авто на lav-ru = DNS пресета (DoH в туннеле + правила списков): %v, правил %d", autoRemote, len(autoRules))
	}

	if err := setDNS(t, s, "tunnel", nil); err != nil {
		t.Fatal(err)
	}
	tunRemote, tunRules := dnsConfig(t, s, p)
	if tunRemote["detour"] != "proxy" || tunRemote["type"] != "https" || len(tunRules) >= len(autoRules) {
		t.Errorf("«Через туннель»: DoH с detour=proxy и без раздельных правил: %v, правил %d (было %d)", tunRemote, len(tunRules), len(autoRules))
	}

	if err := setDNS(t, s, "doh", nil); err != nil {
		t.Fatal(err)
	}
	dohRemote, _ := dnsConfig(t, s, p)
	if _, has := dohRemote["detour"]; has || dohRemote["type"] != "https" || dohRemote["server"] != "dns.google" {
		t.Errorf("«DoH» напрямую: https без detour: %v", dohRemote)
	}

	if err := setDNS(t, s, "dot", nil); err != nil {
		t.Fatal(err)
	}
	if r, _ := dnsConfig(t, s, p); r["type"] != "tls" {
		t.Errorf("«DoT»: %v", r)
	}
	if err := setDNS(t, s, "local", nil); err != nil {
		t.Fatal(err)
	}
	if r, _ := dnsConfig(t, s, p); r["type"] != "local" {
		t.Errorf("«Локальный»: %v", r)
	}

	// «Авто» возвращает DNS пресета.
	if err := setDNS(t, s, "auto", nil); err != nil {
		t.Fatal(err)
	}
	if r, rules := dnsConfig(t, s, p); r["detour"] != "proxy" || len(rules) != len(autoRules) {
		t.Errorf("после «Авто» DNS пресета должен вернуться: %v, правил %d", r, len(rules))
	}
}

// T-31 (B2-07): выбор действует только на активный пресет, GetDns называет карточку.
func TestDnsChoiceIsPerPreset(t *testing.T) {
	f := newSvcFixture(t, nil)
	s := f.srv
	activate(t, s, "lav-ru")
	ru := getDNS(t, s)
	if ru.GetChoice() != "auto" || !ru.RoutingEnabled {
		t.Fatalf("lav-ru до выбора: choice=%q routing=%v", ru.GetChoice(), ru.RoutingEnabled)
	}

	activate(t, s, "global")
	if err := setDNS(t, s, "local", nil); err != nil {
		t.Fatal(err)
	}
	if g := getDNS(t, s); g.GetChoice() != "local" || g.GetRemoteDns() != "local" {
		t.Errorf("global после выбора: %+v", g)
	}

	activate(t, s, "lav-ru")
	if again := getDNS(t, s); again.GetChoice() != "auto" || again.GetRemoteDns() != ru.GetRemoteDns() || !again.RoutingEnabled {
		t.Errorf("раздельный DNS lav-ru сломан выбором на global: %+v", again)
	}
	activate(t, s, "global")
	if g := getDNS(t, s); g.GetChoice() != "local" {
		t.Errorf("выбор global потерялся: %+v", g)
	}
}

func TestSetDnsValidatesChoiceAndAddress(t *testing.T) {
	s := newSvcFixture(t, nil).srv
	bad := func(choice, remote string) {
		t.Helper()
		if err := setDNS(t, s, choice, &remote); connect.CodeOf(err) != connect.CodeInvalidArgument {
			t.Errorf("choice=%q remote=%q: ожидался INVALID_ARGUMENT, получено %v", choice, remote, err)
		}
	}
	bad("custom", "ftp://dns.example")
	bad("custom", "https://")
	bad("custom", "dns example.org")
	bad("custom", "")
	bad("nope", "")
	if err := setDNS(t, s, "custom", strp("https://dns.example/dns-query")); err != nil {
		t.Fatalf("корректный адрес отвергнут: %v", err)
	}
	if g := getDNS(t, s); g.GetChoice() != "custom" || g.GetRemoteDns() != "https://dns.example/dns-query" {
		t.Errorf("custom не сохранился: %+v", g)
	}
	// Старый клиент без choice: пустой remote_dns = «Авто».
	if err := setDNS(t, s, "", nil); err != nil || getDNS(t, s).GetChoice() != "auto" {
		t.Errorf("запрос без choice и remote_dns должен сбросить выбор: %v", err)
	}
}
