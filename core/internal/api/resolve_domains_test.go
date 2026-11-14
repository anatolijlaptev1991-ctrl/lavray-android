// SPDX-License-Identifier: GPL-3.0-only

package api

import (
	"context"
	"fmt"
	"testing"
	"time"

	"connectrpc.com/connect"

	lavrayv1 "github.com/anatolijlaptev1991-ctrl/lavray-android/core/internal/gen/lavrayv1"
	"github.com/anatolijlaptev1991-ctrl/lavray-android/core/internal/model"
)

// resolveFixture — сервер с подставным резолвером и короткими пределами.
func resolveFixture(t *testing.T) (*svcFixture, *fakeResolver) {
	t.Helper()
	f := newSvcFixture(t, nil)
	fr := newFakeResolver()
	f.srv.resolve = resolveConfig{resolver: fr, perHost: 2 * time.Second, parallel: 8}
	return f, fr
}

func resolveIDs(t *testing.T, f *svcFixture, ids ...int64) *lavrayv1.ResolveDomainsResponse {
	t.Helper()
	res, err := f.srv.ResolveDomains(context.Background(), newReq(&lavrayv1.ResolveDomainsRequest{Ids: ids}))
	if err != nil {
		t.Fatalf("ResolveDomains: %v", err)
	}
	return res.Msg
}

func (f *svcFixture) get(t *testing.T, id int64) *model.Profile {
	t.Helper()
	p, err := f.srv.st.GetProfile(id)
	if err != nil {
		t.Fatal(err)
	}
	return p
}

// addVless сохраняет vless-профиль с заданным транспортом.
func (f *svcFixture) addVless(t *testing.T, addr string, stream *model.StreamSettings) *model.Profile {
	t.Helper()
	p := &model.Profile{GroupID: 1, Type: model.TypeVLESS, Name: addr, Address: addr, Port: 443,
		Detail: &model.TrojanVlessDetail{Password: "11111111-1111-1111-1111-111111111111"}, Stream: stream}
	if err := f.srv.st.CreateProfile(p); err != nil {
		t.Fatal(err)
	}
	return p
}

// Адрес становится IP, домен переходит в SNI (TLS) и в host (ws), если они пусты.
func TestResolveSetsIPAndSNI(t *testing.T) {
	f, fr := resolveFixture(t)
	fr.answers["tls.example.org"] = addrs("203.0.113.5")
	fr.answers["ws.example.org"] = addrs("203.0.113.6")
	fr.answers["hy2.example.org"] = addrs("203.0.113.7")
	tlsP := f.addVless(t, "tls.example.org", &model.StreamSettings{Security: "tls"})
	wsP := f.addVless(t, "ws.example.org", &model.StreamSettings{Network: "ws", Security: "tls"})
	hy2 := &model.Profile{GroupID: 1, Type: model.TypeHysteria2, Name: "h", Address: "hy2.example.org", Port: 443,
		Detail: &model.Hysteria2Detail{Password: "pw"}}
	if err := f.srv.st.CreateProfile(hy2); err != nil {
		t.Fatal(err)
	}

	got := resolveIDs(t, f, tlsP.ID, wsP.ID, hy2.ID)
	if !sameIDs(got.ResolvedIds, []int64{tlsP.ID, wsP.ID, hy2.ID}) || len(got.Failed) != 0 || len(got.SkippedIds) != 0 {
		t.Fatalf("resolved=%v skipped=%v failed=%v", got.ResolvedIds, got.SkippedIds, got.Failed)
	}
	a := f.get(t, tlsP.ID)
	if a.Address != "203.0.113.5" || a.Stream.SNI != "tls.example.org" || a.Stream.Host != "" {
		t.Errorf("tls: address=%q sni=%q host=%q", a.Address, a.Stream.SNI, a.Stream.Host)
	}
	b := f.get(t, wsP.ID)
	if b.Address != "203.0.113.6" || b.Stream.SNI != "ws.example.org" || b.Stream.Host != "ws.example.org" {
		t.Errorf("ws: address=%q sni=%q host=%q", b.Address, b.Stream.SNI, b.Stream.Host)
	}
	if h := f.get(t, hy2.ID); h.Address != "203.0.113.7" || h.Detail.(*model.Hysteria2Detail).SNI != "hy2.example.org" {
		t.Errorf("hy2: address=%q detail=%+v", h.Address, h.Detail)
	}
}

// Профиль без TLS и без host-транспорта получает только IP: SNI там не нужен.
func TestResolvePlainProfileOnlyAddress(t *testing.T) {
	f, fr := resolveFixture(t)
	fr.answers["plain.example.org"] = addrs("203.0.113.9")
	p := f.addTrojan(t, "p", "plain.example.org", 8080, "pw")

	resolveIDs(t, f, p.ID)
	got := f.get(t, p.ID)
	if got.Address != "203.0.113.9" || got.Stream != nil {
		t.Fatalf("address=%q stream=%+v", got.Address, got.Stream)
	}
}

// Заданные SNI и host не перезаписываются.
func TestResolveKeepsExistingSNI(t *testing.T) {
	f, fr := resolveFixture(t)
	fr.answers["cdn.example.org"] = addrs("203.0.113.5")
	p := f.addVless(t, "cdn.example.org", &model.StreamSettings{
		Network: "ws", Security: "tls", SNI: "front.example.com", Host: "origin.example.net"})

	resolveIDs(t, f, p.ID)
	got := f.get(t, p.ID)
	if got.Address != "203.0.113.5" || got.Stream.SNI != "front.example.com" || got.Stream.Host != "origin.example.net" {
		t.Fatalf("address=%q sni=%q host=%q", got.Address, got.Stream.SNI, got.Stream.Host)
	}
}

// Один сбой не отменяет остальных: упавший попадает в failed, остальные разрешены.
func TestResolvePartialFailure(t *testing.T) {
	f, fr := resolveFixture(t)
	fr.answers["good.example.org"] = addrs("203.0.113.1")
	good := f.addTrojan(t, "good", "good.example.org", 443, "1")
	bad := f.addTrojan(t, "bad", "bad.example.org", 443, "2")

	got := resolveIDs(t, f, good.ID, bad.ID)
	if !sameIDs(got.ResolvedIds, []int64{good.ID}) {
		t.Fatalf("resolved = %v", got.ResolvedIds)
	}
	if len(got.Failed) != 1 || got.Failed[0].Id != bad.ID || got.Failed[0].Reason != "lookup_failed" {
		t.Fatalf("failed = %v", got.Failed)
	}
	if f.get(t, good.ID).Address != "203.0.113.1" || f.get(t, bad.ID).Address != "bad.example.org" {
		t.Fatal("записан не тот результат")
	}
}

// Ответ без пригодного адреса и неизвестный профиль — тоже в failed.
func TestResolveNoAddressAndNotFound(t *testing.T) {
	f, fr := resolveFixture(t)
	fr.answers["void.example.org"] = addrs("0.0.0.0")
	p := f.addTrojan(t, "v", "void.example.org", 443, "1")

	got := resolveIDs(t, f, p.ID, 4040)
	reasons := map[int64]string{}
	for _, fl := range got.Failed {
		reasons[fl.Id] = fl.Reason
	}
	if reasons[p.ID] != "no_address" || reasons[4040] != "not_found" {
		t.Fatalf("failed = %v", got.Failed)
	}
}

// Зависший резолвер обрывается по таймауту на хост; остальные хосты успевают.
func TestResolveTimeout(t *testing.T) {
	f, fr := resolveFixture(t)
	f.srv.resolve.perHost = 50 * time.Millisecond
	fr.hang["slow.example.org"] = true
	fr.ignoreCtx["deaf.example.org"] = 2 * time.Second // не смотрит на контекст
	fr.answers["fast.example.org"] = addrs("203.0.113.2")
	slow := f.addTrojan(t, "slow", "slow.example.org", 443, "1")
	deaf := f.addTrojan(t, "deaf", "deaf.example.org", 443, "2")
	fast := f.addTrojan(t, "fast", "fast.example.org", 443, "3")

	start := time.Now()
	got := resolveIDs(t, f, slow.ID, deaf.ID, fast.ID)
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Fatalf("запрос длился %v: таймаут на хост не сработал", elapsed)
	}
	if !sameIDs(got.ResolvedIds, []int64{fast.ID}) {
		t.Fatalf("resolved = %v", got.ResolvedIds)
	}
	if len(got.Failed) != 2 || got.Failed[0].Reason != "timeout" || got.Failed[1].Reason != "timeout" {
		t.Fatalf("failed = %v", got.Failed)
	}
}

// Только IPv6-адрес: профиль получает IPv6-литерал; при двух семействах берётся IPv4.
func TestResolveIPv6Only(t *testing.T) {
	f, fr := resolveFixture(t)
	fr.answers["v6.example.org"] = addrs("2001:db8::1")
	fr.answers["dual.example.org"] = addrs("2001:db8::2", "203.0.113.3")
	v6 := f.addTrojan(t, "v6", "v6.example.org", 443, "1")
	dual := f.addTrojan(t, "dual", "dual.example.org", 443, "2")

	got := resolveIDs(t, f, v6.ID, dual.ID)
	if len(got.Failed) != 0 {
		t.Fatalf("failed = %v", got.Failed)
	}
	if a := f.get(t, v6.ID).Address; a != "2001:db8::1" {
		t.Errorf("v6 address = %q", a)
	}
	if a := f.get(t, dual.ID).Address; a != "203.0.113.3" {
		t.Errorf("dual address = %q, want IPv4", a)
	}
}

// Цепочки, custom-профили, уже-IP и профили без адреса пропускаются без обращений к DNS.
func TestResolveSkips(t *testing.T) {
	f, fr := resolveFixture(t)
	ip := f.addTrojan(t, "ip", "198.51.100.7", 443, "1")
	ip6 := f.addTrojan(t, "ip6", "[2001:db8::7]", 443, "2")
	chain := f.addChain(t, "chain", ip.ID)
	custom := &model.Profile{GroupID: 1, Type: model.TypeCustomOutbound, Name: "c", Address: "custom.example.org", Port: 1,
		Detail: &model.CustomDetail{Core: "internal", OutboundJSON: `{}`}}
	if err := f.srv.st.CreateProfile(custom); err != nil {
		t.Fatal(err)
	}

	got := resolveIDs(t, f, ip.ID, ip6.ID, chain.ID, custom.ID)
	if len(got.ResolvedIds) != 0 || len(got.Failed) != 0 {
		t.Fatalf("resolved=%v failed=%v", got.ResolvedIds, got.Failed)
	}
	if !sameIDs(got.SkippedIds, []int64{ip.ID, ip6.ID, chain.ID, custom.ID}) {
		t.Fatalf("skipped = %v", got.SkippedIds)
	}
	if len(fr.calls) != 0 {
		t.Fatalf("резолвер вызван: %v", fr.calls)
	}
}

// До 8 разборов одновременно; одно имя разбирается один раз.
func TestResolveParallelLimit(t *testing.T) {
	f, fr := resolveFixture(t)
	fr.delay = 30 * time.Millisecond
	var ids []int64
	for i := 0; i < 20; i++ {
		host := fmt.Sprintf("h%d.example.org", i)
		fr.answers[host] = addrs(fmt.Sprintf("203.0.113.%d", i+1))
		ids = append(ids, f.addTrojan(t, host, host, 443, fmt.Sprint(i)).ID)
	}
	twin := f.addTrojan(t, "twin", "h0.example.org", 8443, "twin")
	ids = append(ids, twin.ID)

	got := resolveIDs(t, f, ids...)
	if len(got.ResolvedIds) != 21 {
		t.Fatalf("resolved %d из 21, failed=%v", len(got.ResolvedIds), got.Failed)
	}
	if fr.peak < 2 || fr.peak > 8 {
		t.Fatalf("одновременных разборов: %d, want 2..8", fr.peak)
	}
	if n := fr.callCount("h0.example.org"); n != 1 {
		t.Fatalf("h0 разобран %d раз, want 1", n)
	}
	if f.get(t, twin.ID).Address != "203.0.113.1" {
		t.Fatal("второй профиль того же хоста не получил адрес")
	}
}

// Пустой список и больше 1000 id — INVALID_ARGUMENT.
func TestResolveInvalidArgument(t *testing.T) {
	f, _ := resolveFixture(t)
	for name, ids := range map[string][]int64{"пустой": nil, "1001": make([]int64, 1001)} {
		_, err := f.srv.ResolveDomains(context.Background(), newReq(&lavrayv1.ResolveDomainsRequest{Ids: ids}))
		if connect.CodeOf(mapErr(err)) != connect.CodeInvalidArgument {
			t.Errorf("%s: code = %v, err = %v", name, connect.CodeOf(mapErr(err)), err)
		}
	}
}

// Отмена запроса прекращает разбор: профили не меняются, ответ — CANCELED.
func TestResolveCanceled(t *testing.T) {
	f, fr := resolveFixture(t)
	fr.hang["slow.example.org"] = true
	p := f.addTrojan(t, "slow", "slow.example.org", 443, "1")
	ctx, cancel := context.WithCancel(context.Background())
	time.AfterFunc(50*time.Millisecond, cancel)

	_, err := f.srv.ResolveDomains(ctx, newReq(&lavrayv1.ResolveDomainsRequest{Ids: []int64{p.ID}}))
	if connect.CodeOf(mapErr(err)) != connect.CodeCanceled {
		t.Fatalf("code = %v, err = %v", connect.CodeOf(mapErr(err)), err)
	}
	if f.get(t, p.ID).Address != "slow.example.org" {
		t.Fatal("профиль изменён после отмены")
	}
}

// Профиль, изменённый за время разбора, не затирается.
func TestResolveSkipsProfileChangedMeanwhile(t *testing.T) {
	f, fr := resolveFixture(t)
	fr.delay = 100 * time.Millisecond
	fr.answers["x.example.org"] = addrs("203.0.113.4")
	p := f.addTrojan(t, "x", "x.example.org", 443, "1")
	time.AfterFunc(30*time.Millisecond, func() {
		changed := f.get(t, p.ID)
		changed.Address = "moved.example.org"
		_ = f.srv.st.UpdateProfile(changed)
	})

	got := resolveIDs(t, f, p.ID)
	if len(got.ResolvedIds) != 0 || len(got.Failed) != 1 || got.Failed[0].Reason != "changed" {
		t.Fatalf("resolved=%v failed=%v", got.ResolvedIds, got.Failed)
	}
	if a := f.get(t, p.ID).Address; a != "moved.example.org" {
		t.Fatalf("address = %q: правка пользователя затёрта", a)
	}
}
