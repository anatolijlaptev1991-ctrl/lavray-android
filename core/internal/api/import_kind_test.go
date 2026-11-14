// SPDX-License-Identifier: GPL-3.0-only

package api

import (
	"context"
	"encoding/base64"
	"testing"

	"connectrpc.com/connect"
	lavrayv1 "github.com/anatolijlaptev1991-ctrl/lavray-android/core/internal/gen/lavrayv1"
	"github.com/anatolijlaptev1991-ctrl/lavray-android/core/internal/inst"
)

// Вектор из subs/happ/vectors_test.go: расшифровывается в ссылку подписки.
const (
	happSubLink = "happ://crypt/RvdpzSL6VpVzudhV8kd9NIE9F2wpRUH41uE6B7FyXmZITGiaM/cM4qLYVpFr1xf8TCz/r8HE/gQ1Yin7GKAAnJsFPdWAhfybNhdqc6Ip4WnJjTesvVZk2OqWzfGwdKLHFtJwO19QMB+iMEBZd8yx8IAPeMLE9LWWEvouT18TeEk="
	happSubURL  = "https://sub.example.com/NL/native-mode-0?token=test0"
)

func importKind(t *testing.T, cs *clients, req *lavrayv1.ImportContentRequest) *lavrayv1.ImportContentResponse {
	t.Helper()
	res, err := cs.profile.ImportContent(context.Background(), connect.NewRequest(req))
	if err != nil {
		t.Fatalf("ImportContent(%.40q): %v", req.Content, err)
	}
	return res.Msg
}

// T-19 (B1-04): ссылка на подписку и Happ, раскрывающийся в неё, — не профиль.
// Раньше ядро разбирало их как HTTP-прокси «хост:443» (и в превью, и при
// сохранении).
func TestImportContentSubscriptionURLIsNotAProfile(t *testing.T) {
	urls := []string{
		"https://panel.example.com/sub/TOKEN123",
		"https://panel.example.com:8443/sub/abc",
		"http://1.2.3.4:2096/sub/x?flag=clash",
		"https://panel.example.com/api/v1/client/subscribe?token=abc",
		"  https://panel.example.com/sub/TOKEN123\n",
		happSubLink,
	}
	for _, u := range urls {
		cs := newE2E(t)
		for _, preview := range []bool{true, false} {
			got := importKind(t, cs, &lavrayv1.ImportContentRequest{Content: u, PreviewOnly: boolp(preview)})
			if got.Kind != lavrayv1.ImportContentResponse_CONTENT_KIND_SUBSCRIPTION_URL {
				t.Errorf("%.40q preview=%v: kind=%v, ожидался SUBSCRIPTION_URL", u, preview, got.Kind)
			}
			if len(got.ParsedProfiles) != 0 || len(got.AddedProfileIds) != 0 {
				t.Errorf("%.40q preview=%v: профили parsed=%d added=%d, ожидалось 0", u, preview, len(got.ParsedProfiles), len(got.AddedProfileIds))
			}
		}
		if list, _ := cs.st.ListProfiles(-1); len(list) != 0 {
			t.Errorf("%.40q: сохранено профилей: %d", u, len(list))
		}
	}
}

// Ссылка HTTP-прокси (без пути, query только security/sni) остаётся профилем,
// а intent=AS_LINKS заставляет разобрать профилем любую http(s)-ссылку.
func TestImportContentHTTPProxyLinkStaysProfile(t *testing.T) {
	cs := newE2E(t)
	for _, link := range []string{
		"http://user:pass@1.2.3.4:8080#proxy",
		"https://1.2.3.4:8443",
		"http://1.2.3.4:3128/",
		"https://1.2.3.4:443?security=tls&sni=example.org#n",
	} {
		got := importKind(t, cs, &lavrayv1.ImportContentRequest{Content: link, PreviewOnly: boolp(true)})
		if got.Kind != lavrayv1.ImportContentResponse_CONTENT_KIND_SHARE_LINKS || len(got.ParsedProfiles) != 1 {
			t.Errorf("%q: kind=%v parsed=%d, ожидался SHARE_LINKS и 1 профиль", link, got.Kind, len(got.ParsedProfiles))
		} else if got.ParsedProfiles[0].Type != lavrayv1.ProfileType_PROFILE_TYPE_HTTP {
			t.Errorf("%q: type=%v, ожидался HTTP", link, got.ParsedProfiles[0].Type)
		}
	}
	got := importKind(t, cs, &lavrayv1.ImportContentRequest{
		Content:     "https://panel.example.com/sub/TOKEN123",
		Intent:      lavrayv1.ImportContentRequest_INTENT_AS_LINKS,
		PreviewOnly: boolp(true),
	})
	if got.Kind != lavrayv1.ImportContentResponse_CONTENT_KIND_SHARE_LINKS || len(got.ParsedProfiles) != 1 {
		t.Errorf("AS_LINKS: kind=%v parsed=%d, ожидался SHARE_LINKS и 1 профиль", got.Kind, len(got.ParsedProfiles))
	}
}

func TestImportContentKinds(t *testing.T) {
	cs := newE2E(t)
	const line = "trojan://pass@example.org:443#A"
	cases := []struct {
		name, content string
		want          lavrayv1.ImportContentResponse_ContentKind
	}{
		{"share-ссылки", line + "\n" + line, lavrayv1.ImportContentResponse_CONTENT_KIND_SHARE_LINKS},
		{"base64", base64.StdEncoding.EncodeToString([]byte(line)), lavrayv1.ImportContentResponse_CONTENT_KIND_BASE64},
		{"sing-box JSON", `{"outbounds":[{"type":"trojan","tag":"t","server":"example.org","server_port":443,"password":"p"}]}`, lavrayv1.ImportContentResponse_CONTENT_KIND_SINGBOX_JSON},
	}
	for _, tc := range cases {
		got := importKind(t, cs, &lavrayv1.ImportContentRequest{Content: tc.content, PreviewOnly: boolp(true)})
		if got.Kind != tc.want {
			t.Errorf("%s: kind=%v, ожидался %v", tc.name, got.Kind, tc.want)
		}
	}
}

// CreateSubscription раскрывает happ://crypt… до сохранения (как NekoBox):
// планировщик и обновление работают только с http(s)-ссылкой.
func TestCreateSubscriptionDecryptsHapp(t *testing.T) {
	cs := newE2E(t)
	srv := NewServer(Deps{Store: cs.st, Runner: &inst.FakeRunner{}})
	ctx := context.Background()

	g, err := srv.CreateSubscription(ctx, connect.NewRequest(&lavrayv1.CreateSubscriptionRequest{Url: happSubLink}))
	if err != nil {
		t.Fatalf("CreateSubscription(happ): %v", err)
	}
	if g.Msg.GetSubscriptionUrl() != happSubURL || g.Msg.Name != "sub.example.com" {
		t.Errorf("группа: url=%q name=%q", g.Msg.GetSubscriptionUrl(), g.Msg.Name)
	}

	plain, err := srv.CreateSubscription(ctx, connect.NewRequest(&lavrayv1.CreateSubscriptionRequest{
		Name: "prov", Url: "https://panel.example.com/sub/TOKEN123",
	}))
	if err != nil || plain.Msg.GetSubscriptionUrl() != "https://panel.example.com/sub/TOKEN123" {
		t.Errorf("обычная ссылка: err=%v url=%q", err, plain.Msg.GetSubscriptionUrl())
	}

	_, err = srv.CreateSubscription(ctx, connect.NewRequest(&lavrayv1.CreateSubscriptionRequest{Url: "happ://crypt/AAAA"}))
	if connect.CodeOf(err) != connect.CodeInvalidArgument {
		t.Errorf("битая Happ-ссылка: code=%v err=%v", connect.CodeOf(err), err)
	}
}
