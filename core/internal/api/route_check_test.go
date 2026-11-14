// SPDX-License-Identifier: GPL-3.0-only

package api

import (
	"context"
	"strings"
	"testing"

	"connectrpc.com/connect"

	lavrayv1 "github.com/anatolijlaptev1991-ctrl/lavray-android/core/internal/gen/lavrayv1"
)

// T-30 (B2-05): тестер маршрута отвечает так, как поведёт себя конфиг, — по
// rule_set, ip_* и тегу bypass, с запущенным профилем и без него.
func TestCheckRouteMatchesRealConfig(t *testing.T) {
	const (
		direct = lavrayv1.Outbound_OUTBOUND_DIRECT
		proxy  = lavrayv1.Outbound_OUTBOUND_PROXY
		block  = lavrayv1.Outbound_OUTBOUND_BLOCK
	)
	cases := []struct {
		preset, input string
		want          lavrayv1.Outbound
		needsSets     bool // решает rule_set: нужен читатель .srs (сборка с тегом singbox_run)
	}{
		{"lav-ru", "lori.ru", direct, true},                // rule_set geosite-category-ru
		{"lav-ru", "store.steampowered.com", direct, true}, // rule_set geosite-steam
		{"lav-ru", "192.168.1.10", direct, false},          // ip_is_private
		{"lav-ru", "77.88.8.8", direct, true},              // rule_set geoip-ru
		{"lav-ru", "8.8.8.8", proxy, false},                // route.final
		{"lav-ru", "exitgames.com", direct, false},         // domain_suffix, тег bypass
		{"lav-ru", "doubleclick.net", block, true},         // rule_set geosite-category-ads-all
		{"lav-ru", "google.ru", proxy, false},              // proxy-список раньше category-ru
		{"lav-ru", "login.riotgames.com", proxy, false},    // custom-пин раньше geosite:riot
		{"lav-ru", "https://example.org/path", proxy, false},
		{"lav-ru", "bigmodel.cn", direct, false},
		{"lav-ru", "www.bigmodel.cn", direct, false},
		{"lav-ru", "zcode.bigmodel.cn", direct, false},
		{"lav-ru", "https://open.bigmodel.cn/api/anthropic", direct, false},
		{"lav-ru", "deepseek.com", direct, false},
		{"lav-ru", "chat.deepseek.com", direct, false},
		{"lav-ru", "https://api.deepseek.com/v1/models", direct, false},
		{"lav-ru", "kimi.com", direct, false},
		{"lav-ru", "https://api.kimi.ai/coding/v1", direct, false},
		{"lav-ru", "api.moonshot.cn", direct, false},
		{"lav-ru", "api.moonshot.ai", direct, false},
		{"lav-ru", "zcode.z.ai", proxy, false},
		{"global", "192.168.1.10", direct, false},
		{"global", "doubleclick.net", block, true},
		{"global", "example.org", proxy, false},
		{"all-direct", "example.org", direct, false},
	}
	for _, running := range []bool{false, true} {
		f := newSvcFixture(t, nil)
		s, ctx := f.srv, context.Background()
		if running {
			p := f.trojan(t)
			if _, err := s.Connect(ctx, connect.NewRequest(&lavrayv1.ConnectRequest{ProfileId: &p.ID})); err != nil {
				t.Fatal(err)
			}
		}
		active := ""
		for _, c := range cases {
			if c.needsSets && !ruleSetsSupported {
				continue
			}
			if c.preset != active {
				if _, err := s.ActivatePreset(ctx, connect.NewRequest(&lavrayv1.ActivatePresetRequest{Id: c.preset})); err != nil {
					t.Fatal(err)
				}
				active = c.preset
			}
			r, err := s.CheckRoute(ctx, connect.NewRequest(&lavrayv1.CheckRouteRequest{Address: c.input}))
			if err != nil {
				t.Errorf("running=%v %s/%s: %v", running, c.preset, c.input, err)
				continue
			}
			if r.Msg.Outbound != c.want {
				t.Errorf("running=%v %s %q → %v, в конфиге — %v", running, c.preset, c.input, r.Msg.Outbound, c.want)
			}
		}
	}
}

// Объяснение исхода: какой список и какое условие сработали.
func TestCheckRouteExplainsTheMatch(t *testing.T) {
	f := newSvcFixture(t, nil)
	s, ctx := f.srv, context.Background()
	if _, err := s.ActivatePreset(ctx, connect.NewRequest(&lavrayv1.ActivatePresetRequest{Id: "lav-ru"})); err != nil {
		t.Fatal(err)
	}
	check := func(in string) *lavrayv1.CheckRouteResponse {
		r, err := s.CheckRoute(ctx, connect.NewRequest(&lavrayv1.CheckRouteRequest{Address: in}))
		if err != nil {
			t.Fatal(err)
		}
		return r.Msg
	}
	if r := check("exitgames.com"); r.GetMatchedList() != "direct_domain" || !strings.Contains(r.GetMatchedRule(), "domain_suffix") {
		t.Errorf("exitgames.com: list=%q rule=%q", r.GetMatchedList(), r.GetMatchedRule())
	}
	if r := check("bigmodel.cn"); r.GetMatchedList() != "direct_domain" || !strings.Contains(r.GetMatchedRule(), "domain_suffix") {
		t.Errorf("bigmodel.cn: list=%q rule=%q", r.GetMatchedList(), r.GetMatchedRule())
	}
	if r := check("deepseek.com"); r.GetMatchedList() != "direct_domain" || !strings.Contains(r.GetMatchedRule(), "domain_suffix") {
		t.Errorf("deepseek.com: list=%q rule=%q", r.GetMatchedList(), r.GetMatchedRule())
	}
	if r := check("api.kimi.ai"); r.GetMatchedList() != "direct_domain" || !strings.Contains(r.GetMatchedRule(), "domain_suffix") {
		t.Errorf("api.kimi.ai: list=%q rule=%q", r.GetMatchedList(), r.GetMatchedRule())
	}
	if r := check("lori.ru"); ruleSetsSupported && !strings.Contains(r.GetMatchedRule(), "geosite-category-ru") {
		t.Errorf("lori.ru: list=%q rule=%q", r.GetMatchedList(), r.GetMatchedRule())
	}
	if r := check("example.org"); r.GetMatchedList() != "default" || r.MatchedRule != nil {
		t.Errorf("example.org: list=%q rule=%v", r.GetMatchedList(), r.MatchedRule)
	}
}
