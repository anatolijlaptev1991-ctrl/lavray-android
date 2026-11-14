// SPDX-License-Identifier: GPL-3.0-only

package subs

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/anatolijlaptev1991-ctrl/lavray-android/core/internal/model"
	"github.com/anatolijlaptev1991-ctrl/lavray-android/core/internal/store"
)

const testSubBody = "trojan://pass@example.org:443#A\ntrojan://pass2@example.org:8443#B\n"

func newTestStore(t *testing.T) *store.Store {
	t.Helper()
	// Не t.TempDir(): на Windows его RemoveAll гоняется с освобождением
	// хэндлов -wal/-shm — флейк «directory is not empty» (см. store_test.go).
	dir, err := os.MkdirTemp("", "lavray-subs-test-*")
	if err != nil {
		t.Fatal(err)
	}
	st, err := store.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = st.Close()
		for i := 0; i < 50; i++ {
			if err := os.RemoveAll(dir); err == nil {
				return
			}
			time.Sleep(10 * time.Millisecond)
		}
	})
	return st
}

func newSubGroup(t *testing.T, st *store.Store, url string) *model.Group {
	t.Helper()
	g := &model.Group{Name: "sub", Kind: model.GroupSubscription, SubscriptionURL: url}
	if err := st.CreateGroup(g); err != nil {
		t.Fatal(err)
	}
	return g
}

func addProfile(t *testing.T, st *store.Store, groupID int64, name, addr string, port int, latency int32) *model.Profile {
	t.Helper()
	p := &model.Profile{GroupID: groupID, Type: model.TypeTrojan, Name: name, Address: addr, Port: port,
		Detail: &model.TrojanVlessDetail{Password: "x"}}
	if latency != 0 {
		p.TestResult.LatencyMs = &latency
	}
	p.Traffic = model.TrafficCounters{UplinkBytes: 111, DownlinkBytes: 222}
	if err := st.CreateProfile(p); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestURLGuardSchemeAndPrivate(t *testing.T) {
	g := &URLGuard{}
	for _, u := range []string{
		"file:///etc/passwd",
		"ftp://example.com/x",
		"data:text/plain,hi",
		"happ://crypt5/x",
		"http://127.0.0.1:19810/sub",
		"http://localhost/sub",
		"http://169.254.169.254/latest/meta-data",
		"http://10.0.0.1/",
		"http://[::1]/x",
		"http://[fe80::1]/x",
		"http://[fc00::5]/x",
		"notaurl",
	} {
		if err := g.Validate(u); err == nil {
			t.Errorf("Validate(%q) должен отклонять", u)
		}
	}
	// loopback-имя через фейковый резолвер
	fake := &URLGuard{Resolver: fakeResolver{"localhost": []net.IP{net.ParseIP("127.0.0.1")}}}
	if err := fake.Validate("http://localhost/x"); err == nil {
		t.Error("localhost → 127.0.0.1 должен отклоняться")
	}
	pub := &URLGuard{Resolver: fakeResolver{"sub.example.com": []net.IP{net.ParseIP("93.184.216.34")}}}
	if err := pub.Validate("https://sub.example.com/list"); err != nil {
		t.Errorf("публичный хост отклонён: %v", err)
	}
	allow := &URLGuard{AllowPrivate: true}
	if err := allow.Validate("http://127.0.0.1:9/x"); err != nil {
		t.Errorf("AllowPrivate должен разрешать loopback: %v", err)
	}
}

type fakeResolver map[string][]net.IP

func (f fakeResolver) LookupIP(host string) ([]net.IP, error) {
	if ips, ok := f[host]; ok {
		return ips, nil
	}
	return nil, fmt.Errorf("no such host %s", host)
}

func TestURLGuardIsPrivateIP(t *testing.T) {
	for _, s := range []string{"127.0.0.1", "10.1.2.3", "172.16.0.1", "192.168.1.1", "169.254.1.1",
		"100.64.0.1", "0.0.0.0", "224.0.0.1", "255.255.255.255", "::1", "fe80::1", "fc00::1", "ff02::1", "::"} {
		if !IsPrivateIP(net.ParseIP(s)) {
			t.Errorf("IsPrivateIP(%s) = false", s)
		}
	}
	for _, s := range []string{"8.8.8.8", "93.184.216.34", "2001:4860:4860::8888"} {
		if IsPrivateIP(net.ParseIP(s)) {
			t.Errorf("IsPrivateIP(%s) = true", s)
		}
	}
}

func TestFetchHeadersAndBody(t *testing.T) {
	var gotUA, gotHWID, gotCustom string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotUA = r.Header.Get("User-Agent")
		gotHWID = r.Header.Get("x-hwid")
		gotCustom = r.Header.Get("X-Sub-Token")
		w.Header().Set("Subscription-Userinfo", "upload=1; download=2; total=3; expire=60")
		w.Header().Set("Profile-Title", "base64:0JrRg9C/0LvQtdC90L4=")
		w.Header().Set("Profile-Update-Interval", "12")
		w.Header().Set("Content-Disposition", `attachment; filename="fallback.txt"`)
		w.Header().Set("Support-Url", "https://help.example.com")
		w.Header().Set("Announce", "maint note")
		w.Header().Set("Routing", "https://rules.example.com/route")
		w.Header().Set("Profile-Web-Page-Url", "https://panel.example.com")
		fmt.Fprint(w, testSubBody)
	}))
	defer srv.Close()

	var logs []string
	sub := NewSubscriber(nil, Options{
		AllowPrivateURL: true, // httptest слушает на 127.0.0.1
		HWIDEnabled:     true,
		Log:             func(f string, a ...any) { logs = append(logs, fmt.Sprintf(f, a...)) },
	})
	g := &model.Group{Kind: model.GroupSubscription, SubscriptionURL: srv.URL,
		HTTPHeaders: map[string]string{"X-Sub-Token": "tok"}}
	resp, err := sub.Fetch(context.Background(), g)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(gotUA, "LAVRay/") {
		t.Errorf("UA = %q", gotUA)
	}
	if gotHWID == "" {
		t.Error("HWID opt-in: x-hwid не отправлен")
	}
	if gotCustom != "tok" {
		t.Errorf("per-sub заголовок = %q", gotCustom)
	}
	info := resp.Info
	if info == nil {
		t.Fatal("info is nil")
	}
	if info.Title != "Куплено" {
		t.Errorf("title = %q", info.Title)
	}
	if *info.UpdateIntervalHours != 12 || *info.TotalBytes != 3 || *info.ExpireMs != 60000 {
		t.Errorf("info = %+v", info)
	}
	if info.Title == "" || info.SupportURL != "https://help.example.com" || info.Announce != "maint note" ||
		info.Routing != "https://rules.example.com/route" || info.WebPageURL != "https://panel.example.com" {
		t.Errorf("info urls = %+v", info)
	}
	if len(logs) != 0 {
		t.Errorf("unexpected logs: %v", logs)
	}
}

func TestFetchPerSubUserAgent(t *testing.T) {
	var gotUA string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotUA = r.Header.Get("User-Agent")
		fmt.Fprint(w, testSubBody)
	}))
	defer srv.Close()
	sub := NewSubscriber(nil, Options{AllowPrivateURL: true})
	g := &model.Group{Kind: model.GroupSubscription, SubscriptionURL: srv.URL, UserAgent: "MyPanel/2.1"}
	if _, err := sub.Fetch(context.Background(), g); err != nil {
		t.Fatal(err)
	}
	if gotUA != "MyPanel/2.1" {
		t.Errorf("per-sub UA = %q", gotUA)
	}
}

func TestFetchHWIDOptOut(t *testing.T) {
	var gotHWID string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotHWID = r.Header.Get("x-hwid")
		fmt.Fprint(w, testSubBody)
	}))
	defer srv.Close()
	sub := NewSubscriber(nil, Options{AllowPrivateURL: true}) // HWID off по умолчанию
	if _, err := sub.Fetch(context.Background(), &model.Group{Kind: model.GroupSubscription, SubscriptionURL: srv.URL}); err != nil {
		t.Fatal(err)
	}
	if gotHWID != "" {
		t.Error("x-hwid отправлен без opt-in (FR-UI-265)")
	}
}

func TestFetchHTMLError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprint(w, "<html><body>Please login</body></html>")
	}))
	defer srv.Close()
	sub := NewSubscriber(nil, Options{AllowPrivateURL: true})
	_, err := sub.Fetch(context.Background(), &model.Group{Kind: model.GroupSubscription, SubscriptionURL: srv.URL})
	if err != nil {
		t.Fatal("fetch сам по себе ок, HTML ловит парсер")
	}
	// Но Update отвергает: группа не меняется
	st := newTestStore(t)
	g := newSubGroup(t, st, srv.URL)
	old := addProfile(t, st, g.ID, "keep", "keep.example.com", 443, 50)
	if _, err := sub.Update(context.Background(), st, g, false); err == nil {
		t.Fatal("HTML-заглушка должна быть ошибкой Update")
	}
	after, _ := st.ListProfiles(g.ID)
	if len(after) != 1 || after[0].ID != old.ID {
		t.Fatalf("группа изменилась после заглушки: %+v", after)
	}
}

func TestFetchSizeLimit(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write(make([]byte, 1<<20)) // 1 МиБ при лимите 4 КиБ
	}))
	defer srv.Close()
	sub := NewSubscriber(nil, Options{AllowPrivateURL: true, MaxBodyBytes: 4 << 10})
	_, err := sub.Fetch(context.Background(), &model.Group{Kind: model.GroupSubscription, SubscriptionURL: srv.URL})
	if err == nil || !strings.Contains(err.Error(), "лимит") {
		t.Fatalf("err = %v", err)
	}
}

func TestFetchNon200(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "nope", http.StatusForbidden)
	}))
	defer srv.Close()
	sub := NewSubscriber(nil, Options{AllowPrivateURL: true})
	_, err := sub.Fetch(context.Background(), &model.Group{Kind: model.GroupSubscription, SubscriptionURL: srv.URL})
	if err == nil || !strings.Contains(err.Error(), "403") {
		t.Fatalf("err = %v", err)
	}
}

func TestFetchRedirectToPrivateRejected(t *testing.T) {
	// Редиректы — своим контролем (LNB-05): каждая цель через валидатор.
	// (httptest слушает на 127.0.0.1, поэтому проверяем саму политику
	// CheckRedirect напрямую.)
	priv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("редирект на приватный адрес не должен выполняться")
	}))
	defer priv.Close()
	sub := NewSubscriber(nil, Options{}) // AllowPrivateURL=false
	req, _ := http.NewRequest(http.MethodGet, priv.URL, nil)
	via := []*http.Request{{}}
	err := sub.client.CheckRedirect(req, via)
	if err == nil || !strings.Contains(err.Error(), "приватный") && !strings.Contains(err.Error(), "локальный") {
		t.Fatalf("редирект на приватный прошёл: %v", err)
	}
	// и лимит цепочки
	var via6 []*http.Request
	for i := 0; i < MaxRedirects+1; i++ {
		via6 = append(via6, &http.Request{})
	}
	public := "http://public.example.com/next"
	if err := sub.guardValidateForTest(t, public); err != nil {
		t.Fatalf("публичный URL не должен отклоняться: %v", err)
	}
	req2, _ := http.NewRequest(http.MethodGet, public, nil)
	if err := sub.client.CheckRedirect(req2, via6); err == nil || !strings.Contains(err.Error(), "редирект") {
		t.Fatalf("лимит редиректов не сработал: %v", err)
	}
	// При явном «разрешить локальные» редирект на приватный допустим
	subAllow := NewSubscriber(nil, Options{AllowPrivateURL: true})
	req3, _ := http.NewRequest(http.MethodGet, priv.URL, nil)
	if err := subAllow.client.CheckRedirect(req3, nil); err != nil {
		t.Fatalf("AllowPrivate должен пропускать локальные редиректы: %v", err)
	}
}

// guardValidateForTest — публика через фейковый резолвер.
func (s *Subscriber) guardValidateForTest(t *testing.T, url string) error {
	t.Helper()
	g := &URLGuard{Resolver: fakeResolver{"public.example.com": []net.IP{net.ParseIP("93.184.216.34")}}}
	return g.Validate(url)
}

func TestFetchPrivateURLRejectedWithoutFlag(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, testSubBody)
	}))
	defer srv.Close()
	sub := NewSubscriber(nil, Options{}) // AllowPrivateURL=false
	_, err := sub.Fetch(context.Background(), &model.Group{Kind: model.GroupSubscription, SubscriptionURL: srv.URL})
	if err == nil || !strings.Contains(err.Error(), "приватный") && !strings.Contains(err.Error(), "локальный") {
		t.Fatalf("127.0.0.1 не отклонён: %v", err)
	}
}

func TestFetchLastWorkingFallback(t *testing.T) {
	// основной URL — 404; «последний рабочий» отвечает
	good := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, testSubBody)
	}))
	defer good.Close()
	bad := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "moved", http.StatusNotFound)
	}))
	defer bad.Close()

	sub := NewSubscriber(nil, Options{AllowPrivateURL: true, Retries: 0})
	g := &model.Group{Kind: model.GroupSubscription, SubscriptionURL: bad.URL, LastWorkingURL: good.URL}
	resp, err := sub.Fetch(context.Background(), g)
	if err != nil {
		t.Fatal(err)
	}
	if !resp.FromLastWorking || resp.UsedURL != good.URL {
		t.Errorf("FromLastWorking=%v UsedURL=%s", resp.FromLastWorking, resp.UsedURL)
	}
}

func TestUpdateEndToEnd(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Subscription-Userinfo", "upload=1; total=100")
		fmt.Fprint(w, "trojan://pass@example.org:443#A2\ntrojan://newpass@new.example.net:443#C\n")
	}))
	defer srv.Close()

	st := newTestStore(t)
	g := newSubGroup(t, st, srv.URL)
	a := addProfile(t, st, g.ID, "A", "example.org", 443, 50) // совпадёт → обновится на месте
	addProfile(t, st, g.ID, "B", "b.example.com", 443, 0)     // удалится (<50%? 1 из 2 = 50% — не >50)

	sub := NewSubscriber(nil, Options{AllowPrivateURL: true})
	res, err := sub.Update(context.Background(), st, g, false)
	if err != nil {
		t.Fatal(err)
	}
	if res.Diff.Updated != 1 || len(res.Diff.Added) != 1 || len(res.Diff.Deleted) != 1 {
		t.Fatalf("diff = %+v", res.Diff)
	}
	after, _ := st.ListProfiles(g.ID)
	if len(after) != 2 {
		t.Fatalf("после обновления профилей %d, want 2", len(after))
	}
	var a2 *model.Profile
	for _, p := range after {
		if p.ID == a.ID {
			a2 = p
		}
	}
	if a2 == nil {
		t.Fatal("совпавший профиль потерял id (статистика/история должны сохраняться)")
	}
	if a2.Name != "A2" {
		t.Errorf("имя не обновилось: %q", a2.Name)
	}
	if a2.Traffic.UplinkBytes != 111 {
		t.Errorf("статистика потеряна: %+v", a2.Traffic)
	}
	if a2.TestResult.LatencyMs == nil || *a2.TestResult.LatencyMs != 50 {
		t.Errorf("результат теста потерян: %+v", a2.TestResult)
	}
	g2, _ := st.GetGroup(g.ID)
	if g2.LastWorkingURL != srv.URL || g2.LastUpdateMs == 0 {
		t.Errorf("last_working/last_update не записаны: %+v", g2)
	}
	if g2.SubscriptionInfo == nil || g2.SubscriptionInfo.TotalBytes == nil || *g2.SubscriptionInfo.TotalBytes != 100 {
		t.Errorf("subscription_info не записан: %+v", g2.SubscriptionInfo)
	}
	// порядок группы = порядок ответа
	ids := []int64{}
	for _, p := range after {
		ids = append(ids, p.ID)
	}
	g3, _ := st.GetGroup(g.ID)
	if len(g3.ProfileOrder) != 2 || g3.ProfileOrder[0] != a.ID {
		t.Errorf("order = %v", g3.ProfileOrder)
	}
}

func TestUpdateConfirmRequired(t *testing.T) {
	// 4 старых, 1 новый → удаление 3/4 = 75% > 50% → ConfirmRequired
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, "trojan://only@example.org:443#only")
	}))
	defer srv.Close()
	st := newTestStore(t)
	g := newSubGroup(t, st, srv.URL)
	for i := 0; i < 4; i++ {
		addProfile(t, st, g.ID, fmt.Sprintf("p%d", i), fmt.Sprintf("p%d.example.com", i), 443, 0)
	}
	sub := NewSubscriber(nil, Options{AllowPrivateURL: true})
	_, err := sub.Update(context.Background(), st, g, false)
	var cre *ConfirmRequiredError
	if !errors.As(err, &cre) {
		t.Fatalf("err = %v, want ConfirmRequiredError", err)
	}
	after, _ := st.ListProfiles(g.ID)
	if len(after) != 4 {
		t.Fatalf("без подтверждения группа изменилась: %d профилей", len(after))
	}
	// с подтверждением — применяется
	if _, err := sub.Update(context.Background(), st, g, true); err != nil {
		t.Fatal(err)
	}
	after, _ = st.ListProfiles(g.ID)
	if len(after) != 1 || after[0].Name != "only" {
		t.Fatalf("после подтверждения: %+v", after)
	}
}

func TestUpdateEmptyAnswerNotApplied(t *testing.T) {
	// пустой ответ → ошибка, группа сохранена (LNB-29)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	defer srv.Close()
	st := newTestStore(t)
	g := newSubGroup(t, st, srv.URL)
	addProfile(t, st, g.ID, "keep", "keep.example.com", 443, 0)
	sub := NewSubscriber(nil, Options{AllowPrivateURL: true})
	if _, err := sub.Update(context.Background(), st, g, true); err == nil {
		t.Fatal("пустой ответ должен быть ошибкой")
	}
	after, _ := st.ListProfiles(g.ID)
	if len(after) != 1 {
		t.Fatalf("пустой ответ применился: %+v", after)
	}
}

func TestRollback(t *testing.T) {
	st := newTestStore(t)
	g := newSubGroup(t, st, "https://unused.example.com/x")
	p1 := addProfile(t, st, g.ID, "p1", "p1.example.com", 443, 10)
	p2 := addProfile(t, st, g.ID, "p2", "p2.example.com", 8443, 20)

	snap, err := TakeSnapshot(st, g.ID)
	if err != nil {
		t.Fatal(err)
	}
	// применяем «враждебное» обновление напрямую через Apply: один новый
	rem := &model.Profile{Type: model.TypeTrojan, Name: "new", Address: "new.example.net", Port: 443,
		Detail: &model.TrojanVlessDetail{Password: "n"}}
	if _, err := Apply(st, snap, ApplyParams{Next: []*model.Profile{rem}, Confirmed: true, UsedURL: "https://x/y"}); err != nil {
		t.Fatal(err)
	}
	after, _ := st.ListProfiles(g.ID)
	if len(after) != 1 || after[0].Name != "new" {
		t.Fatalf("после применения: %+v", after)
	}

	// Откатить подписку (FR-UI-258)
	if err := Rollback(st, snap); err != nil {
		t.Fatal(err)
	}
	restored, _ := st.ListProfiles(g.ID)
	if len(restored) != 2 {
		t.Fatalf("после отката: %d профилей", len(restored))
	}
	byName := map[string]*model.Profile{}
	for _, p := range restored {
		byName[p.Name] = p
	}
	r1 := byName["p1"]
	if r1 == nil || r1.ID != p1.ID {
		t.Fatalf("id не восстановлены: %+v", byName)
	}
	if r1.TestResult.LatencyMs == nil || *r1.TestResult.LatencyMs != 10 {
		t.Errorf("статистика тестов потеряна: %+v", r1.TestResult)
	}
	if r1.Traffic.UplinkBytes != 111 {
		t.Errorf("трафик потерян: %+v", r1.Traffic)
	}
	_ = p2
	// порядок восстановлен
	g2, _ := st.GetGroup(g.ID)
	if len(g2.ProfileOrder) != 2 || g2.ProfileOrder[0] != p1.ID {
		t.Errorf("порядок не восстановлен: %v", g2.ProfileOrder)
	}
	// last_working_url из «враждебного» обновления откачен
	if g2.LastWorkingURL != "" {
		t.Errorf("last_working_url не откачен: %q", g2.LastWorkingURL)
	}
}

// failingStore — обёртка store, падающая на N-м вызове заданного метода
// (инъекция сбоя посередине применения → проверка автоотката).
type failingStore struct {
	StoreAPI
	failOn  string
	nthCall int
	calls   int
}

func (f *failingStore) CreateProfile(p *model.Profile) error {
	f.calls++
	if f.failOn == "CreateProfile" && f.calls == f.nthCall {
		return errors.New("injected failure")
	}
	return f.StoreAPI.CreateProfile(p)
}

func (f *failingStore) UpdateGroup(g *model.Group) error {
	f.calls++
	if f.failOn == "UpdateGroup" && f.calls == f.nthCall {
		return errors.New("injected failure")
	}
	return f.StoreAPI.UpdateGroup(g)
}

func TestApplyRollbackOnError(t *testing.T) {
	st := newTestStore(t)
	g := newSubGroup(t, st, "https://unused.example.com/x")
	addProfile(t, st, g.ID, "old", "old.example.com", 443, 5)

	snap, _ := TakeSnapshot(st, g.ID)
	fs := &failingStore{StoreAPI: st, failOn: "CreateProfile", nthCall: 1}
	fresh := &model.Profile{Type: model.TypeTrojan, Name: "fresh", Address: "fresh.example.net", Port: 443,
		Detail: &model.TrojanVlessDetail{Password: "x"}}
	_, err := Apply(fs, snap, ApplyParams{Next: []*model.Profile{fresh}, Confirmed: true, UsedURL: "https://u/x"})
	if err == nil || !strings.Contains(err.Error(), "injected failure") {
		t.Fatalf("err = %v", err)
	}
	// группа откачена к снапшоту (включая last_working_url)
	after, _ := st.ListProfiles(g.ID)
	if len(after) != 1 || after[0].Name != "old" {
		t.Fatalf("откат при ошибке не сработал: %+v", after)
	}
	g2, _ := st.GetGroup(g.ID)
	if g2.LastWorkingURL != "" {
		t.Errorf("last_working_url не откачен: %q", g2.LastWorkingURL)
	}
}

func TestComputeDiffFractions(t *testing.T) {
	mk := func(addr string) *model.Profile {
		return &model.Profile{Type: model.TypeTrojan, Address: addr, Port: 443}
	}
	existing := []*model.Profile{mk("a"), mk("b"), mk("c"), mk("d")}
	next := []*model.Profile{mk("a"), mk("b"), mk("e")}
	d := ComputeDiff(existing, next)
	if d.Updated != 2 || len(d.Added) != 1 || len(d.Deleted) != 2 {
		t.Errorf("diff = %+v", d)
	}
	if d.ConfirmRequired {
		t.Error("2/4 = 50% не должен требовать подтверждение (строго >50%)")
	}
	next2 := []*model.Profile{mk("a")}
	d2 := ComputeDiff(existing, next2)
	if !d2.ConfirmRequired {
		t.Error("3/4 = 75% требует подтверждения")
	}
	d3 := ComputeDiff(existing, nil)
	if !d3.ConfirmRequired || !strings.Contains(d3.Reason, "пуст") {
		t.Errorf("пустой список: %+v", d3)
	}
	d4 := ComputeDiff(nil, []*model.Profile{mk("x")})
	if d4.ConfirmRequired {
		t.Error("первое заполнение не требует подтверждения")
	}
}

func TestUpdateArchiveSkipped(t *testing.T) {
	st := newTestStore(t)
	g := newSubGroup(t, st, "https://x.example.com/")
	g.Archive = true
	sub := NewSubscriber(nil, Options{AllowPrivateURL: true})
	if _, err := sub.Update(context.Background(), st, g, true); err == nil || !strings.Contains(err.Error(), "архив") {
		t.Fatalf("err = %v", err)
	}
}

func TestApplyDuplicateKeys(t *testing.T) {
	// W3-3 (ревью): дедуп в ComputeDiff и Apply совпадает.
	st := newTestStore(t)
	g := newSubGroup(t, st, "https://x.example.com/sub")
	addProfile(t, st, g.ID, "dupA", "same.example.com", 443, 0)
	addProfile(t, st, g.ID, "dupB", "same.example.com", 443, 0)

	// 1) В группе два дубля, в ответе один → после Apply ровно один профиль.
	snap, err := TakeSnapshot(st, g.ID)
	if err != nil {
		t.Fatal(err)
	}
	// Пароль тот же, что у сохранённых профилей (addProfile): ключ
	// эквивалентности теперь включает креденшелы (L15 аудита №1).
	one := []*model.Profile{{Type: model.TypeTrojan, Name: "one", Address: "same.example.com", Port: 443,
		Detail: &model.TrojanVlessDetail{Password: "x"}}}
	res, err := Apply(st, snap, ApplyParams{Next: one, Confirmed: true})
	if err != nil {
		t.Fatal(err)
	}
	after, _ := st.ListProfiles(g.ID)
	if len(after) != 1 {
		t.Fatalf("дубль в группе не удалён: %d профилей", len(after))
	}
	if res.Diff.Deleted == nil || len(res.Diff.Deleted) != 1 {
		t.Errorf("diff.Deleted = %+v, ждали 1", res.Diff.Deleted)
	}

	// 2) В ответе два дубля → создаётся/остаётся один.
	snap2, _ := TakeSnapshot(st, g.ID)
	two := []*model.Profile{
		{Type: model.TypeTrojan, Name: "n1", Address: "same.example.com", Port: 443, Detail: &model.TrojanVlessDetail{Password: "x"}},
		{Type: model.TypeTrojan, Name: "n2", Address: "same.example.com", Port: 443, Detail: &model.TrojanVlessDetail{Password: "x"}},
	}
	if _, err := Apply(st, snap2, ApplyParams{Next: two, Confirmed: true}); err != nil {
		t.Fatal(err)
	}
	after2, _ := st.ListProfiles(g.ID)
	if len(after2) != 1 {
		t.Fatalf("дубль в ответе создал второй профиль: %d", len(after2))
	}
}

func TestApplyInfoSnapshotNotMutated(t *testing.T) {
	// W3-4 (ревью): MergeInfo не мутирует снапшот; сбой UpdateGroup →
	// Rollback восстанавливает исходный info.
	st := newTestStore(t)
	g := newSubGroup(t, st, "https://x.example.com/sub")
	totalOld := int64(100)
	g.SubscriptionInfo = &model.SubscriptionInfo{TotalBytes: &totalOld}
	if err := st.UpdateGroup(g); err != nil {
		t.Fatal(err)
	}
	snap, _ := TakeSnapshot(st, g.ID)
	totalNew := int64(999)
	info := &model.SubscriptionInfo{TotalBytes: &totalNew}
	fs := &failingStore{StoreAPI: st, failOn: "UpdateGroup", nthCall: 1}
	_, err := Apply(fs, snap, ApplyParams{Next: snap.Profiles, Confirmed: true, Info: info})
	if err == nil || !strings.Contains(err.Error(), "injected failure") {
		t.Fatalf("err = %v", err)
	}
	g2, _ := st.GetGroup(g.ID)
	if g2.SubscriptionInfo == nil || g2.SubscriptionInfo.TotalBytes == nil || *g2.SubscriptionInfo.TotalBytes != 100 {
		t.Fatalf("снапшот info мутирован: %+v", g2.SubscriptionInfo)
	}
}

func TestDialPinnedRejectsRebindToPrivate(t *testing.T) {
	// W3-2 (ревью): резолв в dial — через тот же guard; «rebinding» DNS на
	// приватный адрес между Validate и dial отклоняется.
	guard := &URLGuard{Resolver: fakeResolver{"sub.example.com": []net.IP{net.ParseIP("127.0.0.1")}}}
	_, err := dialPinned(context.Background(), "sub.example.com:443", guard, time.Second)
	if err == nil || !strings.Contains(err.Error(), "локальный/приватный") {
		t.Fatalf("err = %v, ждали отказ по приватному IP", err)
	}
}

// L15 аудита №1: два РАЗНЫХ аккаунта на одном host:port — не дубли.
// Раньше ключ (type,address,port) склеивал их, второй профиль терялся.
func TestApplyDifferentCredentialsAreNotDuplicates(t *testing.T) {
	st := newTestStore(t)
	g := newSubGroup(t, st, "https://x.example.com/sub")
	snap, err := TakeSnapshot(st, g.ID)
	if err != nil {
		t.Fatal(err)
	}
	next := []*model.Profile{
		{Type: model.TypeVMess, Name: "acct-A", Address: "same.example.com", Port: 443,
			Detail: &model.VMessDetail{UUID: "11111111-1111-1111-1111-111111111111", Security: "auto"}},
		{Type: model.TypeVMess, Name: "acct-B", Address: "same.example.com", Port: 443,
			Detail: &model.VMessDetail{UUID: "22222222-2222-2222-2222-222222222222", Security: "auto"}},
	}
	res, err := Apply(st, snap, ApplyParams{Next: next, Confirmed: true})
	if err != nil {
		t.Fatal(err)
	}
	after, _ := st.ListProfiles(g.ID)
	if len(after) != 2 {
		t.Fatalf("разные аккаунты склеены: %d профилей, ждали 2", len(after))
	}
	if len(res.Diff.Added) != 2 || len(res.Diff.Deleted) != 0 {
		t.Fatalf("diff = +%d/-%d, ждали +2/-0", len(res.Diff.Added), len(res.Diff.Deleted))
	}
}
