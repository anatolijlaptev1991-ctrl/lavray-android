// SPDX-License-Identifier: GPL-3.0-only

package api

// subscription_hwid_test.go — D-02: HWID выключен по умолчанию (подписка не
// получает ни одного идентифицирующего заголовка), а при включении ID один
// и тот же во всех обновлениях и после перезапуска ядра.

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"connectrpc.com/connect"
	lavrayv1 "github.com/anatolijlaptev1991-ctrl/lavray-android/core/internal/gen/lavrayv1"
	"github.com/anatolijlaptev1991-ctrl/lavray-android/core/internal/inst"
	"github.com/anatolijlaptev1991-ctrl/lavray-android/core/internal/model"
)

// hwidRecorder — «провайдер подписки», записывающий заголовки каждого запроса.
type hwidRecorder struct {
	mu      sync.Mutex
	headers []http.Header
}

func newHWIDProvider(t *testing.T) (*httptest.Server, *hwidRecorder) {
	t.Helper()
	rec := &hwidRecorder{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rec.mu.Lock()
		rec.headers = append(rec.headers, r.Header.Clone())
		rec.mu.Unlock()
		// Состав неизменен: разрушительного обновления (с подтверждением) нет.
		fmt.Fprint(w, "trojan://x@hwid1.example.com:443#hwid1\ntrojan://x@hwid2.example.com:443#hwid2\n")
	}))
	t.Cleanup(srv.Close)
	return srv, rec
}

func (r *hwidRecorder) all() []http.Header {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]http.Header(nil), r.headers...)
}

// deviceHeaders — заголовки, по которым провайдер опознаёт устройство.
func deviceHeaders(h http.Header) []string {
	var found []string
	for name := range h {
		l := strings.ToLower(name)
		if l == "x-hwid" || l == "x-ver-os" || strings.HasPrefix(l, "x-device-") {
			found = append(found, name)
		}
	}
	return found
}

func TestSubscriptionDefaultSendsNoDeviceHeaders(t *testing.T) {
	provider, rec := newHWIDProvider(t)
	s, st := subAutoTestServer(t)
	g := subAutoGroup(t, st, provider.URL)

	if d := descriptorMap["sub.hwid_enabled"]; d.DefaultValue != "false" {
		t.Fatalf("sub.hwid_enabled по умолчанию должен быть false, а не %q", d.DefaultValue)
	}
	for i := 0; i < 2; i++ {
		if err := s.AutoUpdateSubscription(context.Background(), g.ID); err != nil {
			t.Fatalf("обновление %d: %v", i+1, err)
		}
	}
	got := rec.all()
	if len(got) != 2 {
		t.Fatalf("запросов к провайдеру %d, ожидалось 2", len(got))
	}
	for i, h := range got {
		if names := deviceHeaders(h); len(names) != 0 {
			t.Errorf("запрос %d при настройках по умолчанию несёт заголовки устройства: %v", i+1, names)
		}
	}
	if v, ok, _ := st.GetSetting(model.SettingHWIDID); ok {
		t.Errorf("HWID создан при выключенном sub.hwid_enabled: %s", v)
	}
}

func TestSubscriptionHWIDStableWhenEnabled(t *testing.T) {
	provider, rec := newHWIDProvider(t)
	s, st := subAutoTestServer(t)
	g := subAutoGroup(t, st, provider.URL)
	if err := st.SetSetting("sub.hwid_enabled", "true"); err != nil {
		t.Fatal(err)
	}

	for i := 0; i < 2; i++ {
		if err := s.AutoUpdateSubscription(context.Background(), g.ID); err != nil {
			t.Fatalf("обновление %d: %v", i+1, err)
		}
	}
	// «Перезапуск ядра»: новый Server на том же хранилище.
	s2 := NewServer(Deps{Store: st, Runner: &inst.FakeRunner{}})
	if err := s2.AutoUpdateSubscription(context.Background(), g.ID); err != nil {
		t.Fatalf("обновление после перезапуска: %v", err)
	}

	got := rec.all()
	if len(got) != 3 {
		t.Fatalf("запросов к провайдеру %d, ожидалось 3", len(got))
	}
	first := got[0].Get("x-hwid")
	if first == "" {
		t.Fatal("при включённом sub.hwid_enabled x-hwid не отправлен")
	}
	for i, h := range got {
		if id := h.Get("x-hwid"); id != first {
			t.Errorf("запрос %d: x-hwid = %q, ожидалось постоянное %q", i+1, id, first)
		}
		if h.Get("x-device-os") == "" {
			t.Errorf("запрос %d: нет x-device-os", i+1)
		}
	}
	if v, ok, _ := st.GetSetting(model.SettingHWIDID); !ok || !strings.Contains(v, first) {
		t.Errorf("ID не сохранён в %s: %q", model.SettingHWIDID, v)
	}

	// Выключили и снова включили: устройство то же самое.
	_ = st.SetSetting("sub.hwid_enabled", "false")
	if err := s.AutoUpdateSubscription(context.Background(), g.ID); err != nil {
		t.Fatal(err)
	}
	if names := deviceHeaders(rec.all()[3]); len(names) != 0 {
		t.Errorf("после выключения HWID заголовки всё ещё уходят: %v", names)
	}
	_ = st.SetSetting("sub.hwid_enabled", "true")
	if err := s.AutoUpdateSubscription(context.Background(), g.ID); err != nil {
		t.Fatal(err)
	}
	if id := rec.all()[4].Get("x-hwid"); id != first {
		t.Errorf("после повторного включения x-hwid = %q, ожидалось %q", id, first)
	}
}

func TestDeviceHWIDRecoversFromCorruptValue(t *testing.T) {
	s, st := subAutoTestServer(t)
	if err := st.SetSetting(model.SettingHWIDID, `{"broken":`); err != nil {
		t.Fatal(err)
	}
	h1, ok := s.deviceHWID()
	if !ok || h1.ID == "" {
		t.Fatal("повреждённая запись должна заменяться новым ID")
	}
	h2, ok := s.deviceHWID()
	if !ok || h2.ID != h1.ID {
		t.Fatalf("после починки ID должен быть постоянным: %q и %q", h1.ID, h2.ID)
	}
}

func TestDeviceHWIDConcurrentFirstUse(t *testing.T) {
	s, _ := subAutoTestServer(t)
	const n = 16
	ids := make([]string, n)
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			if h, ok := s.deviceHWID(); ok {
				ids[i] = h.ID
			}
		}(i)
	}
	wg.Wait()
	for i, id := range ids {
		if id == "" || id != ids[0] {
			t.Fatalf("гонка первого создания: ids[%d] = %q, ids[0] = %q", i, id, ids[0])
		}
	}
}

// Служебный ключ не виден UI и не меняется через SetSettings.
func TestHWIDIDKeyIsHiddenFromUI(t *testing.T) {
	s, st := subAutoTestServer(t)
	if _, ok := s.deviceHWID(); !ok {
		t.Fatal("HWID не создан")
	}
	ctx := context.Background()

	desc, err := s.DescribeSettings(ctx, connect.NewRequest(&lavrayv1.DescribeSettingsRequest{}))
	if err != nil {
		t.Fatal(err)
	}
	enabledListed := false
	for _, d := range desc.Msg.Descriptors {
		if d.Key == model.SettingHWIDID {
			t.Error("DescribeSettings отдаёт служебный ключ sub.hwid_id")
		}
		if d.Key == "sub.hwid_enabled" {
			enabledListed = true
		}
	}
	if !enabledListed {
		t.Error("sub.hwid_enabled должен оставаться видимым в UI")
	}

	for name, keys := range map[string][]string{"все ключи": nil, "по имени": {model.SettingHWIDID}} {
		got, err := s.GetSettings(ctx, connect.NewRequest(&lavrayv1.GetSettingsRequest{Keys: keys}))
		if err != nil {
			t.Fatal(err)
		}
		if _, leaked := got.Msg.Values[model.SettingHWIDID]; leaked {
			t.Errorf("GetSettings (%s) отдаёт служебный ключ sub.hwid_id", name)
		}
	}

	before, _, _ := st.GetSetting(model.SettingHWIDID)
	set, err := s.SetSettings(ctx, connect.NewRequest(&lavrayv1.SetSettingsRequest{
		Values: map[string]*lavrayv1.SettingValue{
			model.SettingHWIDID: {Value: &lavrayv1.SettingValue_StringValue{StringValue: "forged"}},
		},
	}))
	if err != nil {
		t.Fatal(err)
	}
	for _, k := range set.Msg.AcceptedKeys {
		if k == model.SettingHWIDID {
			t.Error("SetSettings принял служебный ключ sub.hwid_id")
		}
	}
	if after, _, _ := st.GetSetting(model.SettingHWIDID); after != before {
		t.Errorf("SetSettings изменил sub.hwid_id: %q -> %q", before, after)
	}
}
