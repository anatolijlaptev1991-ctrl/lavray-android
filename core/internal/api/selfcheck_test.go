// SPDX-License-Identifier: GPL-3.0-only

package api

// Настольные проверки пути через локальный прокси mixed-in (via, утечки, порт, IPv6, use_proxy
// подписки) убраны при переносе ядра в LAVRay Android: локального слушателя здесь нет, запросы
// «через туннель» открывает работающий инстанс в процессе. Это поведение проверяет
// tunnel_inproc_test.go.

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"strings"
	"testing"
	"time"

	"connectrpc.com/connect"

	lavrayv1 "github.com/anatolijlaptev1991-ctrl/lavray-android/core/internal/gen/lavrayv1"
	"github.com/anatolijlaptev1991-ctrl/lavray-android/core/internal/model"
)

type (
	kind   = lavrayv1.RunSelfCheckRequest_CheckKind
	status = lavrayv1.SelfCheckResult_Status
)

const (
	kCore   = lavrayv1.RunSelfCheckRequest_CHECK_KIND_CORE_ALIVE
	kServer = lavrayv1.RunSelfCheckRequest_CHECK_KIND_SERVER_REACHABLE
	kDNS    = lavrayv1.RunSelfCheckRequest_CHECK_KIND_DNS_RESOLVES
	kProxy  = lavrayv1.RunSelfCheckRequest_CHECK_KIND_PROXY_ACTIVE
	kClock  = lavrayv1.RunSelfCheckRequest_CHECK_KIND_CLOCK_SKEW
	kSub    = lavrayv1.RunSelfCheckRequest_CHECK_KIND_SUBSCRIPTION_OK
	kPort   = lavrayv1.RunSelfCheckRequest_CHECK_KIND_PORT_FREE
	kIPv6   = lavrayv1.RunSelfCheckRequest_CHECK_KIND_IPV6

	sOK   = lavrayv1.SelfCheckResult_STATUS_OK
	sWarn = lavrayv1.SelfCheckResult_STATUS_WARN
	sFail = lavrayv1.SelfCheckResult_STATUS_FAIL
	sSkip = lavrayv1.SelfCheckResult_STATUS_UNSPECIFIED // «не проверено»
)

// dateRT отвечает 204 с заголовком Date = now+skew и запоминает адреса запросов.
func dateRT(skew time.Duration, hits *[]string) rtFunc {
	return func(r *http.Request) (*http.Response, error) {
		*hits = append(*hits, r.URL.String())
		h := http.Header{"Date": {time.Now().Add(skew).UTC().Format(http.TimeFormat)}}
		return &http.Response{StatusCode: 204, Header: h, Body: io.NopCloser(strings.NewReader("")), Request: r}, nil
	}
}

func freePort(t *testing.T) int {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	return ln.Addr().(*net.TCPAddr).Port
}

func runChecks(t *testing.T, s *Server, kinds ...kind) map[kind]*lavrayv1.SelfCheckResult {
	t.Helper()
	r, err := s.RunSelfCheck(context.Background(), connect.NewRequest(&lavrayv1.RunSelfCheckRequest{Kinds: kinds}))
	if err != nil {
		t.Fatal(err)
	}
	out := map[kind]*lavrayv1.SelfCheckResult{}
	for _, x := range r.Msg.Results {
		out[x.Kind] = x
	}
	return out
}

func want(t *testing.T, got map[kind]*lavrayv1.SelfCheckResult, k kind, st status, detail string) {
	t.Helper()
	r := got[k]
	if r == nil || r.Status != st || !strings.Contains(r.GetDetail(), detail) {
		t.Errorf("%v: получено %v, ожидалось статус %v с %q", k, r, st, detail)
	}
}

func TestSelfCheckClock(t *testing.T) {
	for _, c := range []struct {
		skew time.Duration
		want status
	}{{0, sOK}, {2 * time.Second, sOK}, {40 * time.Second, sWarn}, {-3 * time.Minute, sFail}} {
		var hits []string
		f := newSvcFixture(t, &http.Client{Transport: dateRT(c.skew, &hits)})
		want(t, runChecks(t, f.srv, kClock), kClock, c.want, "")
	}
	// Сервер времени недоступен: «не проверено», а не «часы неверны».
	f := newSvcFixture(t, &http.Client{Transport: rtFunc(func(*http.Request) (*http.Response, error) { return nil, errors.New("blocked") })})
	want(t, runChecks(t, f.srv, kClock), kClock, sSkip, "time_unreachable")
}

func TestSelfCheckSubscription(t *testing.T) {
	f := newSvcFixture(t, nil)
	now := time.Now()
	mk := func(name string, expire time.Time) {
		ms := expire.UnixMilli()
		g := &model.Group{Name: name, Kind: model.GroupSubscription, SubscriptionURL: "https://sub.example/" + name,
			SubscriptionInfo: &model.SubscriptionInfo{ExpireMs: &ms}}
		if err := f.srv.st.CreateGroup(g); err != nil {
			t.Fatal(err)
		}
	}
	mk("fresh", now.Add(30*24*time.Hour))
	want(t, runChecks(t, f.srv, kSub), kSub, sOK, "fresh")
	mk("soon", now.Add(24*time.Hour))
	want(t, runChecks(t, f.srv, kSub), kSub, sWarn, "soon")
	mk("old", now.Add(-time.Hour))
	got := runChecks(t, f.srv, kSub)
	want(t, got, kSub, sFail, "old")
	if r := got[kSub]; r != nil && r.FixCode != "open_subscriptions" {
		t.Errorf("fix_code = %q", r.FixCode)
	}
}
