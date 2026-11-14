// SPDX-License-Identifier: GPL-3.0-only

// subscription_two_servers_test.go — живой отказ 0.2.0: подписка панели отдаёт
// base64-список из двух ссылок (vmess+ws и vless+grpc), а в базе оказывался
// только vmess. Тест гонит обе ссылки через настоящий ход ядра (CreateSubscription
// → UpdateSubscription → ListProfiles) и проверяет, что сохраняются оба сервера,
// хвост «?serverDescription=<base64>» не попадает в имя, а описание — в заметки.
// Ссылка, которая не разобралась, больше не пропадает молча.
package api

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"connectrpc.com/connect"
	lavrayv1 "github.com/anatolijlaptev1991-ctrl/lavray-android/core/internal/gen/lavrayv1"
	"github.com/anatolijlaptev1991-ctrl/lavray-android/core/internal/gen/lavrayv1connect"
	"github.com/anatolijlaptev1991-ctrl/lavray-android/core/internal/inst"
	"github.com/anatolijlaptev1991-ctrl/lavray-android/core/internal/logs"
	"github.com/anatolijlaptev1991-ctrl/lavray-android/core/internal/model"
	"github.com/anatolijlaptev1991-ctrl/lavray-android/core/internal/store"
)

const (
	twoServersUUID = "11111111-2222-4333-8444-555555555555"
	twoServersHost = "de.example.test"
)

func b64(s string) string { return base64.StdEncoding.EncodeToString([]byte(s)) }

// twoServersLinks — две ссылки панели той же структуры, что у провайдера.
// Имена оканчиваются на «?serverDescription=<base64 описания>» (так их помечает
// приложение-клиент провайдера); описание — кириллица, чтобы base64 содержал
// «+», «/» и «=».
func twoServersLinks(descVMess, descVless string, percentEncodedName bool) []string {
	vm, _ := json.Marshal(map[string]any{
		"v": "2", "ps": "🇩🇪 DE — Берлин (WS)?serverDescription=" + b64(descVMess),
		"add": twoServersHost, "port": "443", "id": twoServersUUID, "aid": "0", "scy": "auto",
		"net": "ws", "type": "none", "host": twoServersHost, "path": "/api/v1/gpu-stream/socket",
		"tls": "tls", "sni": twoServersHost, "alpn": "h2", "fp": "firefox",
	})
	name := "🇩🇪 DE — Берлин (only WiFi)?serverDescription=" + b64(descVless)
	if percentEncodedName {
		name = strings.NewReplacer(" ", "%20", "—", "%E2%80%94").Replace(name)
	}
	vless := "vless://" + twoServersUUID + "@" + twoServersHost + ":443?type=grpc&security=tls&alpn=h2&fp=firefox" +
		"&serviceName=api.v1.stream&sni=" + twoServersHost + "&encryption=none#" + name
	return []string{"vmess://" + b64(string(vm)), vless}
}

// panelBody — тело ответа панели: base64 от списка ссылок.
func panelBody(links ...string) string { return b64(strings.Join(links, "\n") + "\n") }

type twoServersEnv struct {
	profile lavrayv1connect.ProfileServiceClient
	subs    lavrayv1connect.SubscriptionServiceClient
	st      *store.Store
	logs    *logs.Mux
	groupID int64
}

func newTwoServersEnv(t *testing.T) *twoServersEnv {
	t.Helper()
	dir, err := os.MkdirTemp("", "lavray-two-*")
	if err != nil {
		t.Fatal(err)
	}
	st, err := store.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	lg := logs.NewMux(1000)
	NewServer(Deps{Store: st, Logs: lg, Runner: &inst.FakeRunner{}}).Register(mux)
	hs := httptest.NewServer(mux)
	t.Cleanup(func() {
		hs.Close()
		_ = st.Close()
		for i := 0; i < 50 && os.RemoveAll(dir) != nil; i++ {
			time.Sleep(10 * time.Millisecond)
		}
	})
	return &twoServersEnv{
		profile: lavrayv1connect.NewProfileServiceClient(hs.Client(), hs.URL),
		subs:    lavrayv1connect.NewSubscriptionServiceClient(hs.Client(), hs.URL),
		st:      st,
		logs:    lg,
	}
}

// create создаёт подписку (адрес панели публичный по виду — CreateSubscription
// не принимает loopback, T-51), затем подменяет его адресом httptest.
func (e *twoServersEnv) create(t *testing.T, providerURL string) {
	t.Helper()
	ctx := context.Background()
	g, err := e.subs.CreateSubscription(ctx, connect.NewRequest(&lavrayv1.CreateSubscriptionRequest{
		Name: "панель", Url: "https://panel.example.test/sub/abc",
	}))
	if err != nil {
		t.Fatalf("CreateSubscription: %v", err)
	}
	e.groupID = g.Msg.Id
	if _, err := e.subs.UpdateSubscriptionSettings(ctx, connect.NewRequest(&lavrayv1.UpdateSubscriptionSettingsRequest{
		GroupId: e.groupID, Url: strp(providerURL),
	})); err != nil {
		t.Fatalf("UpdateSubscriptionSettings: %v", err)
	}
}

// update обновляет подписку и возвращает последний кадр потока (PHASE_DONE).
func (e *twoServersEnv) update(t *testing.T) *lavrayv1.SubscriptionProgress {
	t.Helper()
	stream, err := e.subs.UpdateSubscription(context.Background(), connect.NewRequest(&lavrayv1.UpdateSubscriptionRequest{
		GroupId: e.groupID, AllowPrivateUrls: boolp(true),
	}))
	if err != nil {
		t.Fatalf("UpdateSubscription: %v", err)
	}
	var last *lavrayv1.SubscriptionProgress
	for stream.Receive() {
		last = stream.Msg()
		if last.Phase == lavrayv1.SubscriptionProgress_PHASE_FAILED {
			t.Fatalf("обновление подписки: %s", last.GetDetail())
		}
	}
	if err := stream.Err(); err != nil {
		t.Fatalf("поток обновления: %v", err)
	}
	return last
}

func (e *twoServersEnv) profiles(t *testing.T) []*lavrayv1.Profile {
	t.Helper()
	list, err := e.profile.ListProfiles(context.Background(), connect.NewRequest(&lavrayv1.ListProfilesRequest{GroupId: int64p(e.groupID)}))
	if err != nil {
		t.Fatalf("ListProfiles: %v", err)
	}
	return list.Msg.Profiles
}

func serveBody(body *string) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, *body)
	}))
}

// Обе ссылки панели доходят до базы; имена без служебного хвоста, описание — в
// заметках; повторное обновление не меняет id.
func TestSubscriptionKeepsBothServersAndCleansDescription(t *testing.T) {
	const descVMess, descVless = "Германия, WS", "Германия, gRPC — только Wi-Fi"
	for _, encoded := range []bool{false, true} {
		body := panelBody(twoServersLinks(descVMess, descVless, encoded)...)
		provider := serveBody(&body)
		e := newTwoServersEnv(t)
		e.create(t, provider.URL)
		e.update(t)
		profiles := e.profiles(t)
		if len(profiles) != 2 {
			var names []string
			for _, p := range profiles {
				names = append(names, p.Name+" ("+p.Type.String()+")")
			}
			t.Fatalf("percentEncoded=%v: в базе %d серверов из двух: %v", encoded, len(profiles), names)
		}
		byType := map[lavrayv1.ProfileType]*lavrayv1.Profile{}
		for _, p := range profiles {
			byType[p.Type] = p
		}
		vm, vl := byType[lavrayv1.ProfileType_PROFILE_TYPE_VMESS], byType[lavrayv1.ProfileType_PROFILE_TYPE_VLESS]
		if vm == nil || vl == nil {
			t.Fatalf("ожидались vmess и vless, получено %v", byType)
		}
		if vm.Name != "🇩🇪 DE — Берлин (WS)" || vm.GetNotes() != descVMess {
			t.Errorf("vmess: имя %q, заметки %q", vm.Name, vm.GetNotes())
		}
		if vl.Name != "🇩🇪 DE — Берлин (only WiFi)" || vl.GetNotes() != descVless {
			t.Errorf("vless: имя %q, заметки %q", vl.Name, vl.GetNotes())
		}

		// Повторное обновление тем же ответом: те же два сервера с теми же id.
		e.update(t)
		again := e.profiles(t)
		if len(again) != 2 || again[0].Id != profiles[0].Id || again[1].Id != profiles[1].Id {
			t.Errorf("после повторного обновления: %d серверов, id изменились", len(again))
		}
		provider.Close()
	}
}

// Свои заметки пользователя обновление подписки не перетирает; описание
// провайдера занимает только пустые заметки.
func TestSubscriptionKeepsUserNotes(t *testing.T) {
	body := panelBody(twoServersLinks("Описание WS", "Описание gRPC", false)...)
	provider := serveBody(&body)
	defer provider.Close()
	e := newTwoServersEnv(t)
	e.create(t, provider.URL)
	e.update(t)
	list, err := e.st.ListProfiles(e.groupID)
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range list {
		if p.Type != model.TypeVMess {
			continue
		}
		p.Notes = "мои заметки"
		if err := e.st.UpdateProfile(p); err != nil {
			t.Fatalf("UpdateProfile: %v", err)
		}
	}
	e.update(t)
	for _, p := range e.profiles(t) {
		switch p.Type {
		case lavrayv1.ProfileType_PROFILE_TYPE_VMESS:
			if p.GetNotes() != "мои заметки" {
				t.Errorf("vmess: заметки %q, ожидались пользовательские", p.GetNotes())
			}
		case lavrayv1.ProfileType_PROFILE_TYPE_VLESS:
			if p.GetNotes() != "Описание gRPC" {
				t.Errorf("vless: заметки %q, ожидалось описание провайдера", p.GetNotes())
			}
		}
	}
}

// Ссылка, которую не удалось разобрать, не пропадает молча: остальные серверы
// сохраняются, кадр PHASE_DONE сообщает число пропущенных строк, причина — в журнале.
func TestSubscriptionReportsSkippedLines(t *testing.T) {
	links := twoServersLinks("a", "b", false)
	// Неизвестный отпечаток uTLS — ссылка отвергается парсером.
	bad := "vless://" + twoServersUUID + "@bad.example.test:443?type=tcp&security=tls&fp=bogus#Плохой"
	body := panelBody(append(links, bad)...)
	provider := serveBody(&body)
	defer provider.Close()
	e := newTwoServersEnv(t)
	e.create(t, provider.URL)
	done := e.update(t)
	if got := len(e.profiles(t)); got != 2 {
		t.Fatalf("сохранено %d серверов, ожидалось 2 (плохая ссылка пропускается)", got)
	}
	if done.Phase != lavrayv1.SubscriptionProgress_PHASE_DONE || !strings.Contains(done.GetDetail(), "не разобрано строк ответа: 1") {
		t.Errorf("последний кадр: фаза %v, detail %q; ожидалось DONE с числом пропущенных строк", done.Phase, done.GetDetail())
	}
	var found bool
	for _, rec := range e.logs.Tail(100) {
		if strings.Contains(rec.Message, "не разобрана и пропущена") && strings.Contains(rec.Message, "bogus") {
			found = true
		}
		if strings.Contains(rec.Message, twoServersUUID) {
			t.Errorf("в журнале секрет ссылки: %q", rec.Message)
		}
	}
	if !found {
		t.Error("в журнале нет записи о пропущенной строке с причиной")
	}
}

// Одиночная ссылка (ImportContent): тот же хвост отрезается, описание — в заметки.
func TestImportContentStripsServerDescription(t *testing.T) {
	e := newTwoServersEnv(t)
	link := twoServersLinks("Описание WS", "Описание gRPC", false)[1]
	res, err := e.profile.ImportContent(context.Background(), connect.NewRequest(&lavrayv1.ImportContentRequest{
		Content: link, Intent: lavrayv1.ImportContentRequest_INTENT_AS_LINKS,
	}))
	if err != nil {
		t.Fatalf("ImportContent: %v", err)
	}
	if len(res.Msg.AddedProfileIds) != 1 {
		t.Fatalf("добавлено %d серверов, ожидался 1", len(res.Msg.AddedProfileIds))
	}
	p, err := e.profile.GetProfile(context.Background(), connect.NewRequest(&lavrayv1.GetProfileRequest{Id: res.Msg.AddedProfileIds[0]}))
	if err != nil {
		t.Fatal(err)
	}
	if p.Msg.Name != "🇩🇪 DE — Берлин (only WiFi)" || p.Msg.GetNotes() != "Описание gRPC" {
		t.Errorf("имя %q, заметки %q", p.Msg.Name, p.Msg.GetNotes())
	}
}
