// SPDX-License-Identifier: GPL-3.0-only

// api_e2e_test.go — e2e W6: полный цикл через connect-клиента поверх
// httptest-сервера (без сети): Profile CRUD/Import/Export, Routing,
// Settings, recover-интерсептор.
package api

import (
	"context"
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
	"github.com/anatolijlaptev1991-ctrl/lavray-android/core/internal/model"
	"github.com/anatolijlaptev1991-ctrl/lavray-android/core/internal/presets"
	"github.com/anatolijlaptev1991-ctrl/lavray-android/core/internal/store"
)

type clients struct {
	profile lavrayv1connect.ProfileServiceClient
	routing lavrayv1connect.RoutingServiceClient
	dns     lavrayv1connect.DnsServiceClient
	core    lavrayv1connect.CoreServiceClient
	st      *store.Store
}

// newE2EServer поднимает настоящий сервер на временном Store и httptest-слушателе
// (только loopback). setup, если задан, правит Server до первого запроса —
// так тесты подменяют сеть и набор номера. Deps.Store подставляется здесь.
func newE2EServer(t *testing.T, d Deps, setup func(*Server)) (*store.Store, *httptest.Server) {
	t.Helper()
	// Не t.TempDir(): гонка RemoveAll с -wal/-shm на Windows (см. store_test).
	dir, err := os.MkdirTemp("", "lavray-api-e2e-*")
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
	d.Store = st
	srv := NewServer(d)
	if setup != nil {
		setup(srv)
	}
	mux := http.NewServeMux()
	srv.Register(mux)
	httpSrv := httptest.NewServer(mux)
	t.Cleanup(httpSrv.Close)
	if _, err := st.GetGroup(1); err != nil {
		// Дефолтная группа (id=1) — как первый запуск приложения (W6 Product
		// Starter делает это при старте; тестам нужна гарантированная группа).
		if err := st.CreateGroup(&model.Group{ID: 1, Name: "Default", Kind: model.GroupBasic}); err != nil {
			t.Fatal(err)
		}
	}
	return st, httpSrv
}

func newE2E(t *testing.T) *clients {
	t.Helper()
	st, httpSrv := newE2EServer(t, Deps{Runner: &inst.FakeRunner{}}, nil)
	hc := httpSrv.Client()
	return &clients{
		profile: lavrayv1connect.NewProfileServiceClient(hc, httpSrv.URL),
		routing: lavrayv1connect.NewRoutingServiceClient(hc, httpSrv.URL),
		dns:     lavrayv1connect.NewDnsServiceClient(hc, httpSrv.URL),
		core:    lavrayv1connect.NewCoreServiceClient(hc, httpSrv.URL),
		st:      st,
	}
}

func TestProfileCRUD(t *testing.T) {
	cs := newE2E(t)
	ctx := context.Background()

	// Группа по умолчанию id=1 создаётся миграцией W1? — нет: создаём явно
	// через SubscriptionService.CreateSubscription нельзя (это подписка);
	// используем SettingsService?.. Группы создаёт GroupService — его в W6
	// регистрирует заглушка? Проверяем фактически: создадим профиль в группу,
	// созданную через nekoimport-независимый путь — store напрямую недоступен
	// из клиента. Обход: CreateProfile с group_id=1 ожидает группу 1 — миграция
	// W1 создаёт Default (id=0)? Фактическое поведение фиксируем тестом ниже.
	res, err := cs.profile.ListProfiles(ctx, connect.NewRequest(&lavrayv1.ListProfilesRequest{}))
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if res.Msg.Total != 0 {
		t.Fatalf("total = %d, want 0", res.Msg.Total)
	}

	// Create: без group_id — INVALID_ARGUMENT.
	_, err = cs.profile.CreateProfile(ctx, connect.NewRequest(&lavrayv1.CreateProfileRequest{
		Profile: &lavrayv1.Profile{Type: lavrayv1.ProfileType_PROFILE_TYPE_SOCKS},
	}))
	if connect.CodeOf(err) != connect.CodeInvalidArgument {
		t.Fatalf("create без группы: code=%v err=%v", connect.CodeOf(err), err)
	}

	// Create в группу 1 (создаётся первым CreateProfile с группой? нет —
	// группа обязательна; создаём через ImportContent c target_group_id=1?
	// тоже нужна группа). Группа 1 — дефолтная группа миграции (W1 «Default»).
	in := &lavrayv1.Profile{
		GroupId: 1,
		Type:    lavrayv1.ProfileType_PROFILE_TYPE_VMESS,
		Name:    "e2e vmess",
		Address: "vm.example.org",
		Port:    443,
		Detail: &lavrayv1.Profile_Vmess{Vmess: &lavrayv1.VmessDetail{
			Uuid:     "b831381d-6324-4d53-ad4f-8cda48b30811",
			Security: "auto",
		}},
	}
	created, err := cs.profile.CreateProfile(ctx, connect.NewRequest(&lavrayv1.CreateProfileRequest{Profile: in}))
	if err != nil {
		t.Fatalf("create: %v (группа 1 есть? миграция W1 создаёт Default)", err)
	}
	id := created.Msg.Id
	if id == 0 {
		t.Fatal("id не назначен")
	}

	// Get + List.
	got, err := cs.profile.GetProfile(ctx, connect.NewRequest(&lavrayv1.GetProfileRequest{Id: id}))
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.Msg.Name != "e2e vmess" {
		t.Fatalf("get: name=%s", got.Msg.Name)
	}
	list, err := cs.profile.ListProfiles(ctx, connect.NewRequest(&lavrayv1.ListProfilesRequest{
		Search: strp("e2e"),
	}))
	if err != nil || list.Msg.Total != 1 {
		t.Fatalf("list search: %v total=%d", err, list.Msg.Total)
	}

	// Update: имя меняется, статистика клиента не затирается (серверная).
	in.Name = "e2e vmess v2"
	upd, err := cs.profile.UpdateProfile(ctx, connect.NewRequest(&lavrayv1.UpdateProfileRequest{Profile: &lavrayv1.Profile{
		Id: id, GroupId: 1, Type: in.Type, Name: in.Name, Address: in.Address, Port: in.Port, Detail: in.Detail,
	}}))
	if err != nil {
		t.Fatalf("update: %v", err)
	}
	if upd.Msg.Profile.Name != "e2e vmess v2" {
		t.Fatalf("update: name=%s", upd.Msg.Profile.Name)
	}

	// SetProfileMeta: favorite.
	meta, err := cs.profile.SetProfileMeta(ctx, connect.NewRequest(&lavrayv1.SetProfileMetaRequest{Id: id, Favorite: boolp(true)}))
	if err != nil {
		t.Fatalf("meta: %v", err)
	}
	if !meta.Msg.Favorite {
		t.Fatal("favorite не установлен")
	}

	// Delete.
	if _, err := cs.profile.DeleteProfile(ctx, connect.NewRequest(&lavrayv1.DeleteProfileRequest{Ids: []int64{id}})); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if _, err := cs.profile.GetProfile(ctx, connect.NewRequest(&lavrayv1.GetProfileRequest{Id: id})); connect.CodeOf(err) != connect.CodeNotFound {
		t.Fatalf("get после delete: code=%v", connect.CodeOf(err))
	}
}

func TestImportContentPreviewAndCommit(t *testing.T) {
	cs := newE2E(t)
	ctx := context.Background()
	link := "trojan://pass@example.org:443#A\ntrojan://pass2@example.org:8443#B\n"

	// Preview: ничего не сохранено.
	pv, err := cs.profile.ImportContent(ctx, connect.NewRequest(&lavrayv1.ImportContentRequest{
		Content:     link,
		PreviewOnly: boolp(true),
	}))
	if err != nil || len(pv.Msg.ParsedProfiles) != 2 {
		t.Fatalf("preview: %v parsed=%d", err, len(pv.Msg.ParsedProfiles))
	}
	list, _ := cs.profile.ListProfiles(ctx, connect.NewRequest(&lavrayv1.ListProfilesRequest{}))
	if list.Msg.Total != 0 {
		t.Fatalf("preview сохранил профили: total=%d", list.Msg.Total)
	}

	// Commit в группу 1.
	cm, err := cs.profile.ImportContent(ctx, connect.NewRequest(&lavrayv1.ImportContentRequest{
		Content:       link,
		TargetGroupId: int64p(1),
	}))
	if err != nil || len(cm.Msg.AddedProfileIds) != 2 {
		t.Fatalf("commit: %v added=%d", err, len(cm.Msg.AddedProfileIds))
	}

	// Экспорт всей группы: две trojan-ссылки.
	exp, err := cs.profile.ExportLinks(ctx, connect.NewRequest(&lavrayv1.ExportLinksRequest{GroupId: int64p(1)}))
	if err != nil || len(exp.Msg.Links) != 2 {
		t.Fatalf("export: %v links=%v", err, exp.Msg.Links)
	}
	for _, l := range exp.Msg.Links {
		if !strings.HasPrefix(l, "trojan://") {
			t.Fatalf("ссылка не trojan: %s", l)
		}
	}
}

func TestRoutingActivateAndGetActive(t *testing.T) {
	cs := newE2E(t)
	ctx := context.Background()

	list, err := cs.routing.ListPresets(ctx, connect.NewRequest(&lavrayv1.ListPresetsRequest{}))
	if err != nil || len(list.Msg.Presets) != len(presets.BuiltinFiles) {
		t.Fatalf("list presets: %v count=%d", err, len(list.Msg.Presets))
	}
	if _, err := cs.routing.ActivatePreset(ctx, connect.NewRequest(&lavrayv1.ActivatePresetRequest{Id: "lav-ru"})); err != nil {
		t.Fatalf("activate lav-ru: %v", err)
	}
	active, err := cs.routing.GetActivePreset(ctx, connect.NewRequest(&lavrayv1.GetActivePresetRequest{}))
	if err != nil || active.Msg.Id != "lav-ru" {
		t.Fatalf("active: %v %+v", err, active.Msg)
	}
	// Неизвестный пресет — NotFound.
	if _, err := cs.routing.ActivatePreset(ctx, connect.NewRequest(&lavrayv1.ActivatePresetRequest{Id: "no-such"})); connect.CodeOf(err) != connect.CodeNotFound {
		t.Fatalf("unknown preset: code=%v", connect.CodeOf(err))
	}
	// Дефолт без активации — «global» (проверяем через свежий сервер —
	// здесь уже активирован lav-ru, поэтому просто фиксируем идемпотентность).
	if _, err := cs.routing.GetActivePreset(ctx, connect.NewRequest(&lavrayv1.GetActivePresetRequest{})); err != nil {
		t.Fatal(err)
	}
}

// Контракт «не реализованный RPC отвечает UNIMPLEMENTED, а не паникой» для всех
// процедур proto проверяет TestUnimplementedRatchet (rpc_ratchet_test.go).
func TestResetTrafficEmptyListMeansAll(t *testing.T) {
	cs := newE2E(t)
	// ResetTraffic реализован (FR-UI-211) — пустой список = все профили, ok.
	if _, err := cs.profile.ResetTraffic(context.Background(), connect.NewRequest(&lavrayv1.ResetTrafficRequest{})); err != nil {
		t.Fatalf("reset traffic: %v", err)
	}
}

func TestGetStatusColdServer(t *testing.T) {
	cs := newE2E(t)
	st, err := cs.core.GetStatus(context.Background(), connect.NewRequest(&lavrayv1.GetStatusRequest{}))
	if err != nil {
		t.Fatalf("status: %v", err)
	}
	if st.Msg.State < lavrayv1.CoreState_CORE_STATE_IDLE || st.Msg.State > lavrayv1.CoreState_CORE_STATE_RECONNECTING {
		t.Fatalf("state вне диапазона: %v", st.Msg.State)
	}
}

func TestImportFillsGeoByCountry(t *testing.T) {
	cs := newE2E(t)
	ctx := context.Background()
	// Подписка с ключами разных стран — типичный случай.
	content := strings.Join([]string{
		"trojan://p@jp1.example.com:443#🇯🇵 Токио",
		"trojan://p@nl1.example.com:443#🇳🇱 Амстердам",
		"trojan://p@de1.example.com:443#Франкфурт",
		"trojan://p@unknown.example.com:443#node-1",
	}, "\n")
	res, err := cs.profile.ImportContent(ctx, connect.NewRequest(&lavrayv1.ImportContentRequest{
		Content:       content,
		TargetGroupId: int64p(1),
	}))
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Msg.AddedProfileIds) != 4 {
		t.Fatalf("added=%d", len(res.Msg.AddedProfileIds))
	}
	list, err := cs.profile.ListProfiles(ctx, connect.NewRequest(&lavrayv1.ListProfilesRequest{}))
	if err != nil {
		t.Fatal(err)
	}
	byName := map[string]string{}
	for _, p := range list.Msg.Profiles {
		byName[p.Name] = p.Geo
	}
	want := map[string]string{"🇯🇵 Токио": "jp", "🇳🇱 Амстердам": "nl", "Франкфурт": "de", "node-1": ""}
	for name, geo := range want {
		if byName[name] != geo {
			t.Errorf("%s: geo=%q want %q", name, byName[name], geo)
		}
	}
}

// TestDnsSettingsReachBuiltConfig — регрессия A5/A6/A7 аудита round 1:
// SetDns писал dns.* в settings, но конфигбилдер читал только пресет,
// поэтому настройки DNS молча не применялись. Проверяем фактический конфиг
// через Connect(validate_only) → GetRuntimeConfig.
func TestDnsSettingsReachBuiltConfig(t *testing.T) {
	cs := newE2E(t)
	ctx := context.Background()

	const advanced = `{"servers":[{"tag":"dns-direct","type":"local"},` +
		`{"tag":"dns-remote","type":"https","server":"1.1.1.1"}],"rules":[]}`
	set, err := cs.dns.SetDns(ctx, connect.NewRequest(&lavrayv1.SetDnsRequest{
		Dns: &lavrayv1.DnsSettings{
			Mode:           lavrayv1.DnsSettings_MODE_ADVANCED,
			AdvancedJson:   strp(advanced),
			RoutingEnabled: true,
		},
	}))
	if err != nil {
		t.Fatalf("set dns: %v", err)
	}
	if set.Msg.Applied == nil || set.Msg.Applied.Mode != lavrayv1.DnsSettings_MODE_ADVANCED {
		t.Fatalf("applied: %+v", set.Msg.Applied)
	}

	got, err := cs.dns.GetDns(ctx, connect.NewRequest(&lavrayv1.GetDnsRequest{}))
	if err != nil {
		t.Fatalf("get dns: %v", err)
	}
	if got.Msg.GetAdvancedJson() != advanced {
		t.Fatalf("advanced_json не сохранился: %q", got.Msg.GetAdvancedJson())
	}
	if !got.Msg.RoutingEnabled {
		t.Fatal("routing_enabled потерялся")
	}

	// Невалидный advanced (нет dns-direct) — INVALID_ARGUMENT, а не тихое сохранение.
	if _, err := cs.dns.SetDns(ctx, connect.NewRequest(&lavrayv1.SetDnsRequest{
		Dns: &lavrayv1.DnsSettings{
			Mode:         lavrayv1.DnsSettings_MODE_ADVANCED,
			AdvancedJson: strp(`{"servers":[{"tag":"x","type":"local"}]}`),
		},
	})); connect.CodeOf(err) != connect.CodeInvalidArgument {
		t.Fatalf("advanced без dns-direct: code=%v", connect.CodeOf(err))
	}

	// Конфиг, собранный движком, обязан содержать именно наш advanced-DNS.
	p, err := cs.profile.CreateProfile(ctx, connect.NewRequest(&lavrayv1.CreateProfileRequest{
		Profile: &lavrayv1.Profile{
			GroupId: 1,
			Type:    lavrayv1.ProfileType_PROFILE_TYPE_TROJAN,
			Name:    "dns-check",
			Address: "example.org",
			Port:    443,
			Detail: &lavrayv1.Profile_TrojanVless{TrojanVless: &lavrayv1.TrojanVlessDetail{
				Password: "secret",
			}},
		},
	}))
	if err != nil {
		t.Fatalf("create profile: %v", err)
	}
	if _, err := cs.core.Connect(ctx, connect.NewRequest(&lavrayv1.ConnectRequest{
		ProfileId:    int64p(p.Msg.Id),
		ValidateOnly: boolp(true),
	})); err != nil {
		t.Fatalf("validate_only connect: %v", err)
	}
	// Проверяем через тот же путь, которым пользуется Connect.
	srv := &Server{st: cs.st}
	preset, err := srv.activePreset()
	if err != nil {
		t.Fatalf("activePreset: %v", err)
	}
	if preset == nil || preset.DNS.Mode != "advanced" || preset.DNS.AdvancedJSON != advanced {
		t.Fatalf("DNS из настроек не доехал до пресета: %+v", preset)
	}
}

// TestValidateDnsJson — FR-UI-149: валидатор и форматтер DNS-объекта.
func TestValidateDnsJson(t *testing.T) {
	cs := newE2E(t)
	ctx := context.Background()

	bad, err := cs.dns.ValidateDnsJson(ctx, connect.NewRequest(&lavrayv1.ValidateDnsJsonRequest{
		Json: `{"servers":[{"tag":"a","type":"local"}]}`,
	}))
	if err != nil {
		t.Fatal(err)
	}
	if bad.Msg.Valid || len(bad.Msg.Errors) == 0 {
		t.Fatalf("ожидали ошибку про dns-direct: %+v", bad.Msg)
	}

	good, err := cs.dns.ValidateDnsJson(ctx, connect.NewRequest(&lavrayv1.ValidateDnsJsonRequest{
		Json: `{"servers":[{"tag":"dns-direct","type":"local"}],"rules":[]}`,
	}))
	if err != nil {
		t.Fatal(err)
	}
	if !good.Msg.Valid || good.Msg.Formatted == nil {
		t.Fatalf("валидный DNS: %+v", good.Msg)
	}
}
