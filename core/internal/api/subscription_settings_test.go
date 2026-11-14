// SPDX-License-Identifier: GPL-3.0-only

// subscription_settings_test.go — регрессия T-21: интервал и User-Agent
// подписки сохраняются через UpdateGroup и доходят до запроса к провайдеру;
// clear_before (и глобальный sub.clear) очищают группу перед применением
// ответа. Стенд t18Env — из subscription_confirm_test.go (T-18).
package api

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"connectrpc.com/connect"
	lavrayv1 "github.com/anatolijlaptev1991-ctrl/lavray-android/core/internal/gen/lavrayv1"
	"github.com/anatolijlaptev1991-ctrl/lavray-android/core/internal/gen/lavrayv1connect"
)

// t21Setup — подписка на «провайдера» из двух серверов (A, B); ua хранит User-Agent последнего запроса.
func t21Setup(t *testing.T) (e *t18Env, gid int64, ua *atomic.Value) {
	t.Helper()
	e, ua = newT18Env(t), &atomic.Value{}
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ua.Store(r.UserAgent())
		_, _ = io.WriteString(w, "trojan://p@a.example.net:443#A\ntrojan://p@b.example.net:443#B\n")
	}))
	t.Cleanup(provider.Close)
	// Группа в обход CreateSubscription: тот не принимает loopback (T-51).
	return e, e.subGroup(t, "prov", provider.URL), ua
}

func (e *t18Env) updateGroup(gid int64, g *lavrayv1.Group) (*lavrayv1.Group, error) {
	g.Id = gid
	res, err := e.profile().UpdateGroup(context.Background(), connect.NewRequest(&lavrayv1.UpdateGroupRequest{Group: g}))
	if err != nil {
		return nil, err
	}
	return res.Msg, nil
}

// update гоняет UpdateSubscription до конца; clear == nil — поле не задано.
func (e *t18Env) update(t *testing.T, gid int64, clear *bool) {
	t.Helper()
	s, err := e.subs().UpdateSubscription(context.Background(), connect.NewRequest(&lavrayv1.UpdateSubscriptionRequest{
		GroupId: gid, AllowPrivateUrls: boolp(true), ClearBefore: clear,
	}))
	if err != nil {
		t.Fatal(err)
	}
	for s.Receive() {
	}
	if err := s.Err(); err != nil {
		t.Fatalf("UpdateSubscription: %v", err)
	}
}

func (e *t18Env) ids(t *testing.T, gid int64) [2]int64 {
	t.Helper()
	res, err := e.profile().ListProfiles(context.Background(), connect.NewRequest(&lavrayv1.ListProfilesRequest{GroupId: int64p(gid)}))
	if err != nil || len(res.Msg.Profiles) != 2 {
		t.Fatalf("ожидались 2 профиля: %v %v", res, err)
	}
	return [2]int64{res.Msg.Profiles[0].Id, res.Msg.Profiles[1].Id}
}

func TestT21_GroupSettingsPersistAndReachProvider(t *testing.T) {
	e, gid, ua := t21Setup(t)

	got, err := e.updateGroup(gid, &lavrayv1.Group{UpdateIntervalMin: int32p(360), UserAgent: strp("clash.meta")})
	if err != nil || got.GetUpdateIntervalMin() != 360 || got.GetUserAgent() != "clash.meta" {
		t.Fatalf("UpdateGroup не сохранил настройки: %v %v", got, err)
	}
	// Поля, которых нет в запросе (переключатель «Автообновление»), не сбрасываются.
	if got, err = e.updateGroup(gid, &lavrayv1.Group{SkipAutoUpdate: true}); err != nil || got.GetUpdateIntervalMin() != 360 || got.GetUserAgent() != "clash.meta" {
		t.Fatalf("отсутствующие поля затёрты: %v %v", got, err)
	}
	e.update(t, gid, nil)
	if v, _ := ua.Load().(string); v != "clash.meta" {
		t.Fatalf("провайдер получил User-Agent %q, ожидался clash.meta", v)
	}
	// 0 и пустая строка — сброс к умолчанию.
	if got, err = e.updateGroup(gid, &lavrayv1.Group{UpdateIntervalMin: int32p(0), UserAgent: strp("")}); err != nil || got.UpdateIntervalMin != nil || got.UserAgent != nil {
		t.Fatalf("сброс не сработал: %v %v", got, err)
	}
	e.update(t, gid, nil)
	if v, _ := ua.Load().(string); !strings.HasPrefix(v, "LAVRay/") {
		t.Fatalf("после сброса ожидался LAVRay/<версия>, получено %q", v)
	}
	// Проверка входа.
	if _, err = e.updateGroup(gid, &lavrayv1.Group{UpdateIntervalMin: int32p(-5)}); connect.CodeOf(err) != connect.CodeInvalidArgument {
		t.Fatalf("отрицательный интервал: code=%v", connect.CodeOf(err))
	}
	if _, err = e.updateGroup(gid, &lavrayv1.Group{UserAgent: strp("x\r\nX-Evil: 1")}); connect.CodeOf(err) != connect.CodeInvalidArgument {
		t.Fatalf("перевод строки в User-Agent: code=%v", connect.CodeOf(err))
	}
}

func TestT21_ClearBefore(t *testing.T) {
	e, gid, _ := t21Setup(t)
	e.update(t, gid, nil)
	first := e.ids(t, gid)
	e.update(t, gid, nil) // обычное обновление совпавших профилей идёт на месте: id те же
	if got := e.ids(t, gid); got != first {
		t.Fatalf("обычное обновление поменяло id: %v -> %v", first, got)
	}
	e.update(t, gid, boolp(true)) // clear_before: группа очищается, профили создаются заново
	cleared := e.ids(t, gid)
	if cleared[0] == first[0] || cleared[1] == first[1] {
		t.Fatalf("clear_before проигнорирован: %v -> %v", first, cleared)
	}
	// Глобальная настройка sub.clear действует, если запрос поле не задаёт…
	_, err := lavrayv1connect.NewSettingsServiceClient(e.hc, e.url).SetSettings(context.Background(), connect.NewRequest(&lavrayv1.SetSettingsRequest{
		Values: map[string]*lavrayv1.SettingValue{"sub.clear": {Value: &lavrayv1.SettingValue_BoolValue{BoolValue: true}}},
	}))
	if err != nil {
		t.Fatal(err)
	}
	e.update(t, gid, nil)
	again := e.ids(t, gid)
	if again[0] == cleared[0] {
		t.Fatalf("sub.clear проигнорирована: %v -> %v", cleared, again)
	}
	// …а явный clear_before=false в запросе её перекрывает.
	e.update(t, gid, boolp(false))
	if got := e.ids(t, gid); got != again {
		t.Fatalf("clear_before=false не перекрыл sub.clear: %v -> %v", again, got)
	}
}
