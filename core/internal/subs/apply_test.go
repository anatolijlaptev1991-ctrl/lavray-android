// SPDX-License-Identifier: GPL-3.0-only

package subs

import (
	"testing"

	"github.com/anatolijlaptev1991-ctrl/lavray-android/core/internal/model"
)

// TestApplyFillsGeo — регрессия A17 аудита round 1: подписка с ключами по
// странам должна заполнять geo при применении, иначе UI-группировка
// «по странам» остаётся пустой для всех подписочных серверов.
func TestApplyFillsGeo(t *testing.T) {
	st := newTestStore(t)
	g := &model.Group{Name: "sub", Kind: model.GroupSubscription, SubscriptionURL: "https://example.com/sub"}
	if err := st.CreateGroup(g); err != nil {
		t.Fatal(err)
	}
	snap, err := TakeSnapshot(st, g.ID)
	if err != nil {
		t.Fatal(err)
	}
	next := []*model.Profile{
		{Type: model.TypeTrojan, Name: "🇯🇵 Токио", Address: "jp.example.com", Port: 443,
			Detail: &model.TrojanVlessDetail{Password: "p"}},
		{Type: model.TypeTrojan, Name: "Амстердам", Address: "nl.example.com", Port: 443,
			Detail: &model.TrojanVlessDetail{Password: "p"}},
		{Type: model.TypeTrojan, Name: "node-1", Address: "unknown.example.com", Port: 443,
			Detail: &model.TrojanVlessDetail{Password: "p"}},
	}
	if _, err := Apply(st, snap, ApplyParams{Next: next, Confirmed: true}); err != nil {
		t.Fatalf("apply: %v", err)
	}
	list, err := st.ListProfiles(g.ID)
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]string{}
	for _, p := range list {
		got[p.Name] = p.Geo
	}
	want := map[string]string{"🇯🇵 Токио": "jp", "Амстердам": "nl", "node-1": ""}
	for name, geo := range want {
		if got[name] != geo {
			t.Errorf("%s: geo=%q want %q", name, got[name], geo)
		}
	}
}

// T-09 (A1-02): автообновление подписки не должно стирать то, что пользователь
// настроил профилю сам. Подписка не доставляет custom-поля (парсер их
// вырезает), поэтому у совпавшего профиля они остаются прежними, вместе с
// подтверждением: подтверждение относится именно к этим полям.
func TestApplyKeepsUserCustomFields(t *testing.T) {
	const (
		customOut = `{"tls_fragment":{"enabled":true}}`
		customCfg = `{"route":{"final":"proxy"}}`
	)
	mk := func(groupID int64, name, pass string) *model.Profile {
		return &model.Profile{
			GroupID: groupID, Type: model.TypeTrojan, Name: name, Address: "tr.example.com", Port: 443,
			Detail: &model.TrojanVlessDetail{Password: pass},
		}
	}
	cases := []struct {
		name string
		next func(groupID int64) *model.Profile
	}{
		{"тот же сервер и пароль", func(g int64) *model.Profile { return mk(g, "провайдер: Токио", "p1") }},
		{"сервер тот же, пароль сменился (мягкое совпадение)", func(g int64) *model.Profile { return mk(g, "провайдер: Токио", "p2") }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			st := newTestStore(t)
			g := &model.Group{Name: "sub", Kind: model.GroupSubscription, SubscriptionURL: "https://example.com/sub"}
			if err := st.CreateGroup(g); err != nil {
				t.Fatal(err)
			}
			old := mk(g.ID, "провайдер: Токио", "p1")
			old.Favorite, old.Notes = true, "основной"
			old.CustomOutboundJSON = customOut
			old.CustomConfigJSON = customCfg
			if err := st.CreateProfile(old); err != nil {
				t.Fatal(err)
			}
			if _, err := st.ConfirmCustomFields(old.ID, mustCustomHash(t, old)); err != nil { // T-08B
				t.Fatal(err)
			}
			snap, err := TakeSnapshot(st, g.ID)
			if err != nil {
				t.Fatal(err)
			}
			next := tc.next(g.ID)
			if _, err := Apply(st, snap, ApplyParams{Next: []*model.Profile{next}, Confirmed: true}); err != nil {
				t.Fatalf("apply: %v", err)
			}
			got, err := st.GetProfile(old.ID)
			if err != nil {
				t.Fatalf("профиль %d пропал после обновления: %v", old.ID, err)
			}
			if got.CustomOutboundJSON != customOut || got.CustomConfigJSON != customCfg {
				t.Errorf("custom-поля стёрты: outbound=%q config=%q", got.CustomOutboundJSON, got.CustomConfigJSON)
			}
			if !got.CustomFieldsConfirmed {
				t.Error("подтверждение custom-полей сброшено")
			}
			if !got.Favorite || got.Notes != "основной" {
				t.Errorf("избранное/заметки потеряны: favorite=%v notes=%q", got.Favorite, got.Notes)
			}
		})
	}
}

// Новый профиль подписки (нет совпавшего) custom-полей не получает, а если
// провайдер всё же прислал их в разобранном профиле — они не наследуют
// подтверждение старого и не подменяют пользовательские.
func TestApplyProviderCustomFieldsDoNotOverrideUser(t *testing.T) {
	st := newTestStore(t)
	g := &model.Group{Name: "sub", Kind: model.GroupSubscription, SubscriptionURL: "https://example.com/sub"}
	if err := st.CreateGroup(g); err != nil {
		t.Fatal(err)
	}
	old := &model.Profile{
		GroupID: g.ID, Type: model.TypeTrojan, Name: "a", Address: "tr.example.com", Port: 443,
		Detail:           &model.TrojanVlessDetail{Password: "p"},
		CustomConfigJSON: `{"log":{"level":"debug"}}`,
	}
	if err := st.CreateProfile(old); err != nil {
		t.Fatal(err)
	}
	if _, err := st.ConfirmCustomFields(old.ID, mustCustomHash(t, old)); err != nil {
		t.Fatal(err)
	}
	snap, err := TakeSnapshot(st, g.ID)
	if err != nil {
		t.Fatal(err)
	}
	fromProvider := &model.Profile{
		Type: model.TypeTrojan, Name: "a", Address: "tr.example.com", Port: 443,
		Detail:           &model.TrojanVlessDetail{Password: "p"},
		CustomConfigJSON: `{"route":{"final":"direct"}}`, // чужое: не пользователь написал
	}
	if _, err := Apply(st, snap, ApplyParams{Next: []*model.Profile{fromProvider}, Confirmed: true}); err != nil {
		t.Fatal(err)
	}
	got, err := st.GetProfile(old.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.CustomConfigJSON != `{"log":{"level":"debug"}}` {
		t.Errorf("пользовательский custom_config подменён данными провайдера: %q", got.CustomConfigJSON)
	}
	if !got.CustomFieldsConfirmed {
		t.Error("подтверждение пользовательских полей потеряно")
	}
}

func mustCustomHash(t *testing.T, p *model.Profile) string {
	t.Helper()
	h, err := model.CustomFieldsHash(p)
	if err != nil {
		t.Fatal(err)
	}
	return h
}
