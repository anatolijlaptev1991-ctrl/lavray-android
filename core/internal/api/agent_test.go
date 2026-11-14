// SPDX-License-Identifier: GPL-3.0-only

package api

// agent_test.go — доступ агентов (MCP): белый список процедур по режиму,
// ограничения параметров, очистка ответов от адресов и секретов, смена режима
// без перезапуска. Сервер настоящий (Store во временном каталоге, FakeRunner),
// запросы идут по HTTP через ConnectRPC-клиентов с основным и агентским токеном.

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"connectrpc.com/connect"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"

	lavrayv1 "github.com/anatolijlaptev1991-ctrl/lavray-android/core/internal/gen/lavrayv1"
	"github.com/anatolijlaptev1991-ctrl/lavray-android/core/internal/gen/lavrayv1connect"
	"github.com/anatolijlaptev1991-ctrl/lavray-android/core/internal/model"
)

const (
	testMainToken  = "main-token-0123456789abcdef0123456789abcdef"
	testAgentToken = "agent-token-0123456789abcdef0123456789abcd"
	secretHost     = "secret-host.example.org"
	secretPassword = "p4ss-w0rd-SECRET"
	secretUUID     = "b831381d-6324-4d53-ad4f-8cda48b30811"
	secretSubURL   = "https://sub.example.com/api/v1/token-SECRET-abc"
)

// bearerRT подставляет токен в каждый запрос.
type bearerRT struct{ token string }

func (b bearerRT) RoundTrip(r *http.Request) (*http.Response, error) {
	r = r.Clone(r.Context())
	r.Header.Set("Authorization", "Bearer "+b.token)
	return http.DefaultTransport.RoundTrip(r)
}

// agentHandler повторяет проверку токенов cmd/lavray-core: основной даёт полный
// доступ, агентский — отметку агента в контексте.
func agentHandler(srv *Server) http.Handler {
	h := srv.NewHandler()
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
		if got == testMainToken {
			h.ServeHTTP(w, r)
			return
		}
		if r2, ok := srv.AuthorizeAgent(r, got); ok {
			h.ServeHTTP(w, r2)
			return
		}
		http.Error(w, `{"code":"unauthenticated"}`, http.StatusUnauthorized)
	})
}

// apiClients — клиенты всех нужных сервисов под одним токеном.
type apiClients struct {
	core      lavrayv1connect.CoreServiceClient
	profile   lavrayv1connect.ProfileServiceClient
	subs      lavrayv1connect.SubscriptionServiceClient
	settings  lavrayv1connect.SettingsServiceClient
	logs      lavrayv1connect.LogServiceClient
	stats     lavrayv1connect.StatsServiceClient
	conns     lavrayv1connect.ConnectionServiceClient
	diag      lavrayv1connect.DiagnosticsServiceClient
	routing   lavrayv1connect.RoutingServiceClient
	dns       lavrayv1connect.DnsServiceClient
	migration lavrayv1connect.MigrationServiceClient
}

func newAPIClients(url, token string) *apiClients {
	hc := &http.Client{Transport: bearerRT{token}}
	return &apiClients{
		core:      lavrayv1connect.NewCoreServiceClient(hc, url),
		profile:   lavrayv1connect.NewProfileServiceClient(hc, url),
		subs:      lavrayv1connect.NewSubscriptionServiceClient(hc, url),
		settings:  lavrayv1connect.NewSettingsServiceClient(hc, url),
		logs:      lavrayv1connect.NewLogServiceClient(hc, url),
		stats:     lavrayv1connect.NewStatsServiceClient(hc, url),
		conns:     lavrayv1connect.NewConnectionServiceClient(hc, url),
		diag:      lavrayv1connect.NewDiagnosticsServiceClient(hc, url),
		routing:   lavrayv1connect.NewRoutingServiceClient(hc, url),
		dns:       lavrayv1connect.NewDnsServiceClient(hc, url),
		migration: lavrayv1connect.NewMigrationServiceClient(hc, url),
	}
}

type agentFixture struct {
	*svcFixture
	url     string
	main    *apiClients
	agent   *apiClients
	profile *model.Profile
}

// newAgentFixture: сервер с профилем, подпиской и результатом теста; режим
// агента выключен (по умолчанию).
func newAgentFixture(t *testing.T) *agentFixture {
	t.Helper()
	f := newSvcFixture(t, nil)
	f.srv.SetAgentToken(testAgentToken)
	ts := httptest.NewServer(agentHandler(f.srv))
	t.Cleanup(ts.Close)

	p := &model.Profile{GroupID: 1, Type: model.TypeTrojan, Name: "Нидерланды 1", Address: secretHost, Port: 443,
		Detail: &model.TrojanVlessDetail{Password: secretPassword},
		Notes:  "заметка с " + secretUUID}
	if err := f.srv.st.CreateProfile(p); err != nil {
		t.Fatal(err)
	}
	failedAt := int64(1_700_000_000_000)
	if err := f.srv.st.SaveTestResult(p.ID, model.TestResult{TestedAtMs: &failedAt, LastError: "dial tcp 10.1.2.3:443: connection refused"}); err != nil {
		t.Fatal(err)
	}
	if err := f.srv.st.CreateGroup(&model.Group{ID: 2, Name: "Подписка", Kind: model.GroupSubscription,
		SubscriptionURL: secretSubURL, HTTPBasicUser: "basic-user-SECRET", HTTPHeaders: map[string]string{"X-Key": "hdr-SECRET"}}); err != nil {
		t.Fatal(err)
	}
	return &agentFixture{
		svcFixture: f,
		url:        ts.URL,
		main:       newAPIClients(ts.URL, testMainToken),
		agent:      newAPIClients(ts.URL, testAgentToken),
		profile:    p,
	}
}

// setMode меняет agent.mode настоящим вызовом SetSettings с основным токеном.
func (f *agentFixture) setMode(t *testing.T, mode string) {
	t.Helper()
	_, err := f.main.settings.SetSettings(context.Background(), connect.NewRequest(&lavrayv1.SetSettingsRequest{
		Values: map[string]*lavrayv1.SettingValue{
			SettingAgentMode: {Value: &lavrayv1.SettingValue_StringValue{StringValue: mode}},
		},
	}))
	if err != nil {
		t.Fatalf("SetSettings(agent.mode=%s): %v", mode, err)
	}
}

// denied — вызов отвергнут политикой агента (а не по существу запроса).
func denied(err error) bool {
	return err != nil && connect.CodeOf(err) == connect.CodePermissionDenied
}

// agentCall — вызов процедуры; стримы читаются до конца, чтобы увидеть отказ.
type agentCall struct {
	name string
	run  func(c *apiClients) error
}

func unary[Req, Res any](f func(context.Context, *connect.Request[Req]) (*connect.Response[Res], error), msg *Req) error {
	_, err := f(context.Background(), connect.NewRequest(msg))
	return err
}

func drain[T any](stream *connect.ServerStreamForClient[T], err error) error {
	if err != nil {
		return err
	}
	for stream.Receive() {
	}
	defer stream.Close()
	return stream.Err()
}

func readCalls(profileID int64) []agentCall {
	return []agentCall{
		{"GetStatus", func(c *apiClients) error { return unary(c.core.GetStatus, &lavrayv1.GetStatusRequest{}) }},
		{"ListProfiles", func(c *apiClients) error { return unary(c.profile.ListProfiles, &lavrayv1.ListProfilesRequest{}) }},
		{"ListGroups", func(c *apiClients) error { return unary(c.profile.ListGroups, &lavrayv1.ListGroupsRequest{}) }},
		{"ListSubscriptions", func(c *apiClients) error {
			return unary(c.subs.ListSubscriptions, &lavrayv1.ListSubscriptionsRequest{})
		}},
		{"GetTail", func(c *apiClients) error { return unary(c.logs.GetTail, &lavrayv1.GetTailRequest{Lines: 5}) }},
		{"GetTrafficTotals", func(c *apiClients) error {
			return unary(c.stats.GetTrafficTotals, &lavrayv1.GetTrafficTotalsRequest{ProfileIds: []int64{profileID}})
		}},
		{"GetTrafficSummary", func(c *apiClients) error {
			return unary(c.stats.GetTrafficSummary, &lavrayv1.GetTrafficSummaryRequest{})
		}},
		{"StatsSubscribe", func(c *apiClients) error {
			// Без инстанса поток ждёт (T-79), поэтому отказ политики виден сразу, а
			// разрешённый вызов заканчивается по таймауту.
			ctx, cancel := context.WithTimeout(context.Background(), 3e8)
			defer cancel()
			stream, err := c.stats.Subscribe(ctx, connect.NewRequest(&lavrayv1.SubscribeStatsRequest{}))
			return drain(stream, err)
		}},
	}
}

func controlCalls(profileID int64) []agentCall {
	return []agentCall{
		{"Connect", func(c *apiClients) error {
			return unary(c.core.Connect, &lavrayv1.ConnectRequest{ProfileId: &profileID})
		}},
		{"Disconnect", func(c *apiClients) error { return unary(c.core.Disconnect, &lavrayv1.DisconnectRequest{}) }},
		{"StopTests", func(c *apiClients) error { return unary(c.profile.StopTests, &lavrayv1.StopTestsRequest{}) }},
		{"TestLatency", func(c *apiClients) error {
			return drain(c.profile.TestLatency(context.Background(), connect.NewRequest(&lavrayv1.TestLatencyRequest{
				ProfileIds: []int64{profileID}, Mode: lavrayv1.TestLatencyRequest_MODE_TCP_PING, TimeoutMs: ptr(int32(300)),
			})))
		}},
		{"UpdateAllSubscriptions", func(c *apiClients) error {
			return drain(c.subs.UpdateAllSubscriptions(context.Background(), connect.NewRequest(&lavrayv1.UpdateAllSubscriptionsRequest{})))
		}},
	}
}

// neverCalls — процедуры, закрытые агенту в любом режиме.
func neverCalls(profileID int64) []agentCall {
	return []agentCall{
		{"DeleteProfile", func(c *apiClients) error {
			return unary(c.profile.DeleteProfile, &lavrayv1.DeleteProfileRequest{Ids: []int64{profileID}})
		}},
		{"CreateProfile", func(c *apiClients) error {
			return unary(c.profile.CreateProfile, &lavrayv1.CreateProfileRequest{Profile: &lavrayv1.Profile{GroupId: 1}})
		}},
		{"ImportContent", func(c *apiClients) error {
			return unary(c.profile.ImportContent, &lavrayv1.ImportContentRequest{Content: "trojan://x@example.org:443#a"})
		}},
		{"ExportLinks", func(c *apiClients) error {
			return unary(c.profile.ExportLinks, &lavrayv1.ExportLinksRequest{Ids: []int64{profileID}})
		}},
		{"ExportConfig", func(c *apiClients) error {
			return unary(c.profile.ExportConfig, &lavrayv1.ExportConfigRequest{ProfileId: profileID})
		}},
		{"GetRuntimeConfig", func(c *apiClients) error {
			return unary(c.core.GetRuntimeConfig, &lavrayv1.GetRuntimeConfigRequest{})
		}},
		{"RestartProfile", func(c *apiClients) error { return unary(c.core.RestartProfile, &lavrayv1.RestartProfileRequest{}) }},
		{"Shutdown", func(c *apiClients) error { return unary(c.core.Shutdown, &lavrayv1.ShutdownRequest{}) }},
		{"SubscribeEvents", func(c *apiClients) error {
			ctx, cancel := context.WithTimeout(context.Background(), 2e9)
			defer cancel()
			return drain(c.core.SubscribeEvents(ctx, connect.NewRequest(&lavrayv1.SubscribeEventsRequest{})))
		}},
		{"ListConnections", func(c *apiClients) error {
			return unary(c.conns.ListConnections, &lavrayv1.ListConnectionsRequest{})
		}},
		{"WatchConnections", func(c *apiClients) error {
			ctx, cancel := context.WithTimeout(context.Background(), 3e8)
			defer cancel()
			return drain(c.conns.WatchConnections(ctx, connect.NewRequest(&lavrayv1.WatchConnectionsRequest{})))
		}},
		{"CloseConnection", func(c *apiClients) error {
			return unary(c.conns.CloseConnection, &lavrayv1.CloseConnectionRequest{Id: "00000000-0000-0000-0000-000000000000"})
		}},
		{"GetSettings", func(c *apiClients) error { return unary(c.settings.GetSettings, &lavrayv1.GetSettingsRequest{}) }},
		{"SetSettings", func(c *apiClients) error {
			return unary(c.settings.SetSettings, &lavrayv1.SetSettingsRequest{Values: map[string]*lavrayv1.SettingValue{
				SettingAgentMode: {Value: &lavrayv1.SettingValue_StringValue{StringValue: AgentModeControl}},
			}})
		}},
		{"GetDns", func(c *apiClients) error { return unary(c.dns.GetDns, &lavrayv1.GetDnsRequest{}) }},
		{"SetDns", func(c *apiClients) error { return unary(c.dns.SetDns, &lavrayv1.SetDnsRequest{}) }},
		{"ListPresets", func(c *apiClients) error { return unary(c.routing.ListPresets, &lavrayv1.ListPresetsRequest{}) }},
		{"CreateSubscription", func(c *apiClients) error {
			return unary(c.subs.CreateSubscription, &lavrayv1.CreateSubscriptionRequest{Name: "x", Url: "https://example.com/s"})
		}},
		{"DeleteSubscription", func(c *apiClients) error {
			return unary(c.subs.DeleteSubscription, &lavrayv1.DeleteSubscriptionRequest{GroupId: 2})
		}},
		{"UpdateSubscriptionSettings", func(c *apiClients) error {
			return unary(c.subs.UpdateSubscriptionSettings, &lavrayv1.UpdateSubscriptionSettingsRequest{GroupId: 2})
		}},
	}
}

func ptr[T any](v T) *T { return &v }

// Без токена, с чужим токеном и с токеном до его выдачи ядро не отвечает.
func TestAgentTokenIsRequired(t *testing.T) {
	f := newAgentFixture(t)
	for _, token := range []string{"", "wrong", testAgentToken + "x"} {
		err := unary(newAPIClients(f.url, token).core.GetStatus, &lavrayv1.GetStatusRequest{})
		if connect.CodeOf(err) != connect.CodeUnauthenticated {
			t.Errorf("токен %q: %v, ожидался unauthenticated", token, err)
		}
	}
	f.srv.SetAgentToken("")
	err := unary(f.agent.core.GetStatus, &lavrayv1.GetStatusRequest{})
	if connect.CodeOf(err) != connect.CodeUnauthenticated {
		t.Errorf("пустой агентский токен ядра пропустил запрос: %v", err)
	}
}

// Режим по умолчанию — off: агентский токен не открывает ничего.
func TestAgentModeOffDeniesEverything(t *testing.T) {
	f := newAgentFixture(t)
	if got := f.srv.AgentMode(); got != AgentModeOff {
		t.Fatalf("режим по умолчанию = %q, ожидался off", got)
	}
	all := append(append(readCalls(f.profile.ID), controlCalls(f.profile.ID)...), neverCalls(f.profile.ID)...)
	for _, c := range all {
		if err := c.run(f.agent); !denied(err) {
			t.Errorf("off: %s не отклонён: %v", c.name, err)
		}
	}
	// основной токен от режима не зависит
	if err := unary(f.main.core.GetStatus, &lavrayv1.GetStatusRequest{}); err != nil {
		t.Errorf("основной токен при agent.mode=off: %v", err)
	}
}

func TestAgentModeReadAllowsOnlyReads(t *testing.T) {
	f := newAgentFixture(t)
	f.setMode(t, AgentModeRead)
	for _, c := range readCalls(f.profile.ID) {
		if err := c.run(f.agent); denied(err) {
			t.Errorf("read: %s отклонён: %v", c.name, err)
		}
	}
	for _, c := range append(controlCalls(f.profile.ID), neverCalls(f.profile.ID)...) {
		if err := c.run(f.agent); !denied(err) {
			t.Errorf("read: %s не отклонён: %v", c.name, err)
		}
	}
	if f.runner.StartedCounter != 0 {
		t.Errorf("в режиме read ядро запускало инстанс (%d раз)", f.runner.StartedCounter)
	}
}

func TestAgentModeControlAddsControlButNeverDestructive(t *testing.T) {
	f := newAgentFixture(t)
	f.setMode(t, AgentModeControl)
	for _, c := range append(readCalls(f.profile.ID), controlCalls(f.profile.ID)...) {
		if err := c.run(f.agent); denied(err) {
			t.Errorf("control: %s отклонён: %v", c.name, err)
		}
	}
	for _, c := range neverCalls(f.profile.ID) {
		if err := c.run(f.agent); !denied(err) {
			t.Errorf("control: %s не отклонён: %v", c.name, err)
		}
	}
	// профиль на месте: DeleteProfile не дошёл до хендлера
	if _, err := f.srv.st.GetProfile(f.profile.ID); err != nil {
		t.Errorf("профиль пропал: %v", err)
	}
}

// Смена режима действует сразу, без перезапуска и без нового токена.
func TestAgentModeChangeAppliesImmediately(t *testing.T) {
	f := newAgentFixture(t)
	status := func() error { return unary(f.agent.core.GetStatus, &lavrayv1.GetStatusRequest{}) }
	connectCall := func() error {
		return unary(f.agent.core.Connect, &lavrayv1.ConnectRequest{ProfileId: &f.profile.ID})
	}
	if !denied(status()) {
		t.Fatal("off: GetStatus должен быть отклонён")
	}
	f.setMode(t, AgentModeRead)
	if err := status(); err != nil {
		t.Fatalf("read: %v", err)
	}
	if !denied(connectCall()) {
		t.Fatal("read: Connect должен быть отклонён")
	}
	f.setMode(t, AgentModeControl)
	if err := connectCall(); err != nil {
		t.Fatalf("control: Connect: %v", err)
	}
	if f.runner.StartedCounter != 1 {
		t.Errorf("инстанс запущен %d раз, ожидался 1", f.runner.StartedCounter)
	}
	f.setMode(t, AgentModeOff)
	if !denied(status()) {
		t.Fatal("после возврата в off агент снова должен быть отклонён")
	}
}

// Агент не может поднять себе режим: настройки закрыты, значение проверяется.
func TestAgentCannotEscalateAndModeIsValidated(t *testing.T) {
	f := newAgentFixture(t)
	f.setMode(t, AgentModeControl)
	c := neverCalls(f.profile.ID)
	for _, call := range c {
		if call.name != "SetSettings" {
			continue
		}
		if err := call.run(f.agent); !denied(err) {
			t.Errorf("SetSettings агентом не отклонён: %v", err)
		}
	}
	_, err := f.main.settings.SetSettings(context.Background(), connect.NewRequest(&lavrayv1.SetSettingsRequest{
		Values: map[string]*lavrayv1.SettingValue{
			SettingAgentMode: {Value: &lavrayv1.SettingValue_StringValue{StringValue: "root"}},
		},
	}))
	if connect.CodeOf(err) != connect.CodeInvalidArgument {
		t.Errorf("agent.mode=root принят: %v", err)
	}
	f.srv.st.SetSetting(SettingAgentMode, `"nonsense"`)
	if got := f.srv.AgentMode(); got != AgentModeOff {
		t.Errorf("испорченное значение даёт режим %q, ожидался off", got)
	}
}

// Параметры разрешённых процедур ограничены.
func TestAgentRequestRestrictions(t *testing.T) {
	f := newAgentFixture(t)
	f.setMode(t, AgentModeControl)
	id := f.profile.ID
	url := "https://evil.example/collect"
	inline := &lavrayv1.Profile{Name: "x", Type: lavrayv1.ProfileType_PROFILE_TYPE_SOCKS, Address: "evil.example", Port: 1080}
	cases := []struct {
		name string
		run  func() error
	}{
		{"Connect с inline_profile", func() error {
			return unary(f.agent.core.Connect, &lavrayv1.ConnectRequest{InlineProfile: inline})
		}},
		{"Connect без server_id", func() error { return unary(f.agent.core.Connect, &lavrayv1.ConnectRequest{}) }},
		{"Connect validate_only", func() error {
			return unary(f.agent.core.Connect, &lavrayv1.ConnectRequest{ProfileId: &id, ValidateOnly: ptr(true)})
		}},
		{"Connect режим TUN", func() error {
			return unary(f.agent.core.Connect, &lavrayv1.ConnectRequest{ProfileId: &id, Mode: lavrayv1.ConnectMode_CONNECT_MODE_TUN})
		}},
		{"ListProfiles с поиском по адресу", func() error {
			return unary(f.agent.profile.ListProfiles, &lavrayv1.ListProfilesRequest{Search: ptr("secret-host")})
		}},
		{"TestLatency со своим url", func() error {
			return drain(f.agent.profile.TestLatency(context.Background(), connect.NewRequest(&lavrayv1.TestLatencyRequest{
				ProfileIds: []int64{id}, Mode: lavrayv1.TestLatencyRequest_MODE_URL_TEST, Url: &url,
			})))
		}},
		{"TestLatency полный тест", func() error {
			return drain(f.agent.profile.TestLatency(context.Background(), connect.NewRequest(&lavrayv1.TestLatencyRequest{
				ProfileIds: []int64{id}, Mode: lavrayv1.TestLatencyRequest_MODE_FULL_TEST, FullLatency: ptr(true),
			})))
		}},
		{"UpdateSubscription confirm_destructive", func() error {
			return drain(f.agent.subs.UpdateSubscription(context.Background(), connect.NewRequest(&lavrayv1.UpdateSubscriptionRequest{
				GroupId: 2, ConfirmDestructive: ptr(true),
			})))
		}},
		{"UpdateSubscription allow_private_urls", func() error {
			return drain(f.agent.subs.UpdateSubscription(context.Background(), connect.NewRequest(&lavrayv1.UpdateSubscriptionRequest{
				GroupId: 2, AllowPrivateUrls: ptr(true),
			})))
		}},
	}
	for _, c := range cases {
		if err := c.run(); !denied(err) {
			t.Errorf("%s не отклонён: %v", c.name, err)
		}
	}
	if f.runner.StartedCounter != 0 {
		t.Errorf("отклонённые запросы запустили инстанс (%d)", f.runner.StartedCounter)
	}
}

// Хвост журнала для агента не длиннее предела, как бы много ни просили.
func TestAgentLogTailIsCapped(t *testing.T) {
	f := newAgentFixture(t)
	f.setMode(t, AgentModeRead)
	for i := 0; i < MaxAgentLogLines+100; i++ {
		f.srv.logs.Logf("info", "core", "строка %d", i)
	}
	for _, lines := range []int32{0, 1000, -5} {
		res, err := f.agent.logs.GetTail(context.Background(), connect.NewRequest(&lavrayv1.GetTailRequest{Lines: lines}))
		if err != nil {
			t.Fatal(err)
		}
		if n := len(res.Msg.Lines); n != MaxAgentLogLines {
			t.Errorf("lines=%d: %d строк, ожидалось %d", lines, n, MaxAgentLogLines)
		}
	}
	res, _ := f.agent.logs.GetTail(context.Background(), connect.NewRequest(&lavrayv1.GetTailRequest{Lines: 3}))
	if len(res.Msg.Lines) != 3 {
		t.Errorf("lines=3: %d строк", len(res.Msg.Lines))
	}
}

// Журнал для агента без посещаемых сайтов и адресов серверов; основному
// клиенту те же строки отдаются как есть.
func TestAgentLogTailHidesAddresses(t *testing.T) {
	f := newAgentFixture(t)
	f.setMode(t, AgentModeRead)
	f.srv.logs.Logf("info", "core", "inbound connection to www.example-site.ru:443")
	f.srv.logs.Logf("warn", "core", "dial tcp 203.0.113.7:8443: i/o timeout via [2001:db8::1]:443")
	ctx := context.Background()
	res, err := f.agent.logs.GetTail(ctx, connect.NewRequest(&lavrayv1.GetTailRequest{Lines: 2}))
	if err != nil {
		t.Fatal(err)
	}
	for _, l := range res.Msg.Lines {
		for _, leak := range []string{"example-site", "203.0.113.7", "2001:db8"} {
			if strings.Contains(l.Message, leak) {
				t.Errorf("агенту ушёл адрес %q: %q", leak, l.Message)
			}
		}
		if !strings.Contains(l.Message, "[addr]") {
			t.Errorf("адрес не заменён меткой: %q", l.Message)
		}
	}
	full, err := f.main.logs.GetTail(ctx, connect.NewRequest(&lavrayv1.GetTailRequest{Lines: 2}))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(full.Msg.Lines[0].Message, "www.example-site.ru") {
		t.Errorf("основной клиент получил урезанный журнал: %q", full.Msg.Lines[0].Message)
	}
}

// В ответах агенту нет адресов, паролей, uuid, ссылок подписок и текстов ошибок
// с адресами; те же данные основному клиенту отдаются целиком.
func TestAgentResponsesCarryNoSecrets(t *testing.T) {
	f := newAgentFixture(t)
	f.setMode(t, AgentModeControl)
	ctx := context.Background()
	secrets := []string{secretHost, secretPassword, secretUUID, secretSubURL, "token-SECRET", "basic-user-SECRET", "hdr-SECRET", "10.1.2.3", "443"}

	dump := func(label string, msgs ...proto.Message) {
		t.Helper()
		for _, m := range msgs {
			raw, err := protojson.Marshal(m)
			if err != nil {
				t.Fatal(err)
			}
			for _, s := range secrets {
				if strings.Contains(string(raw), s) {
					t.Errorf("%s: в ответе %T есть %q:\n%s", label, m, s, raw)
				}
			}
		}
	}
	profiles, err := f.agent.profile.ListProfiles(ctx, connect.NewRequest(&lavrayv1.ListProfilesRequest{}))
	if err != nil {
		t.Fatal(err)
	}
	groups, err := f.agent.profile.ListGroups(ctx, connect.NewRequest(&lavrayv1.ListGroupsRequest{}))
	if err != nil {
		t.Fatal(err)
	}
	subsRes, err := f.agent.subs.ListSubscriptions(ctx, connect.NewRequest(&lavrayv1.ListSubscriptionsRequest{}))
	if err != nil {
		t.Fatal(err)
	}
	dump("агент", profiles.Msg, groups.Msg, subsRes.Msg)
	if len(profiles.Msg.Profiles) != 1 || profiles.Msg.Profiles[0].Name != "Нидерланды 1" || profiles.Msg.Profiles[0].Id != f.profile.ID {
		t.Errorf("полезные поля потеряны: %v", profiles.Msg.Profiles)
	}
	if tr := profiles.Msg.Profiles[0].TestResult; tr == nil || tr.GetLastError() != "failed" || tr.GetTestedAtMs() == 0 {
		t.Errorf("результат теста: %v", tr)
	}
	if len(subsRes.Msg.Groups) != 1 || subsRes.Msg.Groups[0].Name != "Подписка" {
		t.Errorf("подписка: %v", subsRes.Msg.Groups)
	}

	// те же данные у основного клиента на месте — очистка не трогает хранилище
	own, err := f.main.profile.ListProfiles(ctx, connect.NewRequest(&lavrayv1.ListProfilesRequest{}))
	if err != nil {
		t.Fatal(err)
	}
	if own.Msg.Profiles[0].Address != secretHost {
		t.Errorf("основной клиент не видит адрес: %q", own.Msg.Profiles[0].Address)
	}

	// статус и ответ Connect: без параметров входа, ошибка без адресов
	conn, err := f.agent.core.Connect(ctx, connect.NewRequest(&lavrayv1.ConnectRequest{ProfileId: &f.profile.ID}))
	if err != nil {
		t.Fatal(err)
	}
	status, err := f.agent.core.GetStatus(ctx, connect.NewRequest(&lavrayv1.GetStatusRequest{}))
	if err != nil {
		t.Fatal(err)
	}
	if conn.Msg.Status.Inbound != nil || status.Msg.Inbound != nil || len(conn.Msg.Warnings) != 0 {
		t.Errorf("параметры входящего подключения видны агенту: %v / %v / %v", conn.Msg.Status.Inbound, status.Msg.Inbound, conn.Msg.Warnings)
	}
	if status.Msg.State != lavrayv1.CoreState_CORE_STATE_CONNECTED || status.Msg.GetActiveProfileName() != "Нидерланды 1" {
		t.Errorf("состояние потеряно: %v", status.Msg)
	}
}

func TestAgentSanitizeTextMasksAddressesAndSecrets(t *testing.T) {
	cases := []struct{ in, banned string }{
		{"dial tcp 203.0.113.7:443: i/o timeout", "203.0.113.7"},
		{"lookup secret-host.example.org: no such host", "secret-host.example.org"},
		{"connect [2001:db8::1]:443 failed", "2001:db8::1"},
		{"GET https://sub.example.com/x?token=abcdef failed", "abcdef"},
		{"uuid b831381d-6324-4d53-ad4f-8cda48b30811 rejected", "b831381d"},
	}
	for _, c := range cases {
		if got := agentSanitizeText(c.in); strings.Contains(got, c.banned) {
			t.Errorf("%q → %q: остался %q", c.in, got, c.banned)
		}
	}
	long := agentSanitizeText(strings.Repeat("ошибка ", 200))
	if n := len([]rune(long)); n > maxAgentTextRunes+1 {
		t.Errorf("текст не обрезан: %d рун", n)
	}
}

// Прогресс подписки не выдаёт ссылку: в detail фазы REQUESTING лежит адрес.
func TestAgentSubscriptionProgressHidesURL(t *testing.T) {
	f := newAgentFixture(t)
	f.setMode(t, AgentModeControl)
	stream, err := f.agent.subs.UpdateSubscription(context.Background(), connect.NewRequest(&lavrayv1.UpdateSubscriptionRequest{GroupId: 2}))
	if err != nil {
		t.Fatal(err)
	}
	defer stream.Close()
	var frames []*lavrayv1.SubscriptionProgress
	for stream.Receive() {
		frames = append(frames, stream.Msg())
	}
	if len(frames) == 0 {
		t.Fatal("обновление не прислало ни одного кадра")
	}
	for _, m := range frames {
		raw, _ := protojson.Marshal(m)
		for _, s := range []string{secretSubURL, "sub.example.com", "token-SECRET"} {
			if strings.Contains(string(raw), s) {
				t.Errorf("в кадре прогресса есть %q: %s", s, raw)
			}
		}
	}
}

// Отказы и управляющие вызовы агента видны в журнале приложения.
func TestAgentCallsAreLogged(t *testing.T) {
	f := newAgentFixture(t)
	f.setMode(t, AgentModeRead)
	_ = unary(f.agent.core.Connect, &lavrayv1.ConnectRequest{ProfileId: &f.profile.ID})
	f.setMode(t, AgentModeControl)
	_ = unary(f.agent.core.Disconnect, &lavrayv1.DisconnectRequest{})
	var text strings.Builder
	for _, l := range f.srv.logs.Tail(100) {
		text.WriteString(l.Message + "\n")
	}
	for _, want := range []string{"агент (MCP): отказ /lavray.v1.CoreService/Connect в режиме read", "агент (MCP): /lavray.v1.CoreService/Disconnect"} {
		if !strings.Contains(text.String(), want) {
			t.Errorf("в журнале нет %q:\n%s", want, text.String())
		}
	}
}

// Проверка прав на TUN: понятная причина вместо сырой ошибки sing-box; режим
// системного прокси проверка не касается.
func TestConnectTunPrivilegeCheck(t *testing.T) {
	f := newAgentFixture(t)
	f.srv.tunCheck = func() error {
		return errString("режим TUN требует прав администратора, а ядро запущено без повышения")
	}
	f.setMode(t, AgentModeControl)

	tun := &lavrayv1.ConnectRequest{ProfileId: &f.profile.ID, Mode: lavrayv1.ConnectMode_CONNECT_MODE_TUN}
	err := unary(f.main.core.Connect, tun)
	if connect.CodeOf(err) != connect.CodeFailedPrecondition || !strings.Contains(err.Error(), "прав администратора") {
		t.Fatalf("TUN без прав: %v", err)
	}
	if f.runner.StartedCounter != 0 {
		t.Error("TUN без прав запустил инстанс")
	}
	// запомненный режим TUN: подключение из MCP (режим не задан) тоже объясняет причину
	f.srv.st.SetSetting("remember.modes", `["tun"]`)
	err = unary(f.agent.core.Connect, &lavrayv1.ConnectRequest{ProfileId: &f.profile.ID})
	if connect.CodeOf(err) != connect.CodeFailedPrecondition || !strings.Contains(err.Error(), "прав администратора") {
		t.Fatalf("запомненный TUN без прав: %v", err)
	}
	f.srv.st.SetSetting("remember.modes", `["system_proxy"]`)
	if err := unary(f.agent.core.Connect, &lavrayv1.ConnectRequest{ProfileId: &f.profile.ID}); err != nil {
		t.Fatalf("системный прокси: %v", err)
	}
}

type errString string

func (e errString) Error() string { return string(e) }

// Заголовок подтверждения деструктивного обновления подписки агент подставить не
// может: перехватчик убирает его до хендлера. Обычному клиенту он остаётся.
func TestAgentCannotSendTheConfirmHeader(t *testing.T) {
	i := newInterceptor(nil, func() string { return AgentModeControl })
	proc := lavrayv1connect.SubscriptionServiceUpdateSubscriptionProcedure

	agent := http.Header{}
	agent.Set(agentConfirmHeader, "1")
	if err := i.agentBefore(WithAgent(context.Background()), proc, agent); err != nil {
		t.Fatal(err)
	}
	if got := agent.Get(agentConfirmHeader); got != "" {
		t.Errorf("заголовок подтверждения дошёл бы до хендлера: %q", got)
	}

	own := http.Header{}
	own.Set(agentConfirmHeader, "1")
	if err := i.agentBefore(context.Background(), proc, own); err != nil {
		t.Fatal(err)
	}
	if own.Get(agentConfirmHeader) != "1" {
		t.Error("у основного клиента заголовок убран")
	}
}
