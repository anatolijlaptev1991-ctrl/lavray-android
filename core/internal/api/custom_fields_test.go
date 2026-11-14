// SPDX-License-Identifier: GPL-3.0-only

package api

// custom_fields_test.go — T-08B: подтверждение custom-полей привязано к хэшу
// содержимого (PreviewCustomFields → ConfirmCustomFields).

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"testing"

	"connectrpc.com/connect"
	"google.golang.org/protobuf/proto"

	lavrayv1 "github.com/anatolijlaptev1991-ctrl/lavray-android/core/internal/gen/lavrayv1"
	"github.com/anatolijlaptev1991-ctrl/lavray-android/core/internal/gen/lavrayv1connect"
	"github.com/anatolijlaptev1991-ctrl/lavray-android/core/internal/inst"
	"github.com/anatolijlaptev1991-ctrl/lavray-android/core/internal/model"
)

const (
	cfgRemote = `{"dns":{"final":"remote-marker"}}`
	cfgLocal  = `{"dns":{"final":"local-marker"}}`
)

// trojanWithCustom — сохранённый trojan-профиль с паролем и custom-полями.
func trojanWithCustom(t *testing.T, cs *clients, out, cfg string) *lavrayv1.Profile {
	t.Helper()
	in := &lavrayv1.Profile{
		GroupId: 1, Type: lavrayv1.ProfileType_PROFILE_TYPE_TROJAN, Name: "tj",
		Address: "tr.example.com", Port: 443,
		Detail: &lavrayv1.Profile_TrojanVless{TrojanVless: &lavrayv1.TrojanVlessDetail{Password: "Sup3rS3cretPassw0rd"}},
	}
	if out != "" {
		in.CustomOutboundJson = strp(out)
	}
	if cfg != "" {
		in.CustomConfigJson = strp(cfg)
	}
	resp, err := cs.profile.CreateProfile(context.Background(), connect.NewRequest(&lavrayv1.CreateProfileRequest{Profile: in}))
	if err != nil {
		t.Fatalf("CreateProfile: %v", err)
	}
	return resp.Msg
}

func previewOf(t *testing.T, cs *clients, id int64) *lavrayv1.PreviewCustomFieldsResponse {
	t.Helper()
	resp, err := cs.profile.PreviewCustomFields(context.Background(), connect.NewRequest(&lavrayv1.PreviewCustomFieldsRequest{ProfileId: id}))
	if err != nil {
		t.Fatalf("PreviewCustomFields(%d): %v", id, err)
	}
	return resp.Msg
}

func confirmHash(cs *clients, id int64, hash string) (*lavrayv1.Profile, error) {
	resp, err := cs.profile.ConfirmCustomFields(context.Background(), connect.NewRequest(&lavrayv1.ConfirmCustomFieldsRequest{ProfileId: id, CustomHash: hash}))
	if err != nil {
		return nil, err
	}
	return resp.Msg, nil
}

// confirmByHash подтверждает текущее содержимое, считая хэш как клиент
// (без предпросмотра — например, когда сборка конфига невозможна).
func confirmByHash(t *testing.T, cs *clients, id int64) {
	t.Helper()
	p, err := cs.st.GetProfile(id)
	if err != nil {
		t.Fatal(err)
	}
	h, err := model.CustomFieldsHash(p)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := confirmHash(cs, id, h); err != nil {
		t.Fatalf("ConfirmCustomFields: %v", err)
	}
}

func updateProfile(t *testing.T, cs *clients, p *lavrayv1.Profile) *lavrayv1.Profile {
	t.Helper()
	resp, err := cs.profile.UpdateProfile(context.Background(), connect.NewRequest(&lavrayv1.UpdateProfileRequest{Profile: p}))
	if err != nil {
		t.Fatalf("UpdateProfile: %v", err)
	}
	return resp.Msg.GetProfile()
}

func confirmed(t *testing.T, cs *clients, id int64) bool {
	t.Helper()
	resp, err := cs.profile.GetProfile(context.Background(), connect.NewRequest(&lavrayv1.GetProfileRequest{Id: id}))
	if err != nil {
		t.Fatal(err)
	}
	return resp.Msg.GetCustomFieldsConfirmed()
}

func TestClientCannotSetCustomFieldsConfirmed(t *testing.T) {
	cs := newE2E(t)
	p := trojanWithCustom(t, cs, "", cfgRemote)
	if p.GetCustomFieldsConfirmed() {
		t.Fatal("CreateProfile принял флаг подтверждения от клиента")
	}
	p.CustomFieldsConfirmed = true
	if got := updateProfile(t, cs, p); got.GetCustomFieldsConfirmed() || confirmed(t, cs, p.GetId()) {
		t.Fatal("UpdateProfile принял флаг подтверждения от клиента")
	}
}

func TestConfirmBindsToHash(t *testing.T) {
	cs := newE2E(t)
	p := trojanWithCustom(t, cs, "", cfgRemote)
	pv := previewOf(t, cs, p.GetId())
	if pv.GetAlreadyConfirmed() || len(pv.GetCustomHash()) != 64 {
		t.Fatalf("предпросмотр: %+v", pv)
	}
	got, err := confirmHash(cs, p.GetId(), pv.GetCustomHash())
	if err != nil {
		t.Fatal(err)
	}
	if !got.GetCustomFieldsConfirmed() || !confirmed(t, cs, p.GetId()) {
		t.Fatal("подтверждение не записано")
	}
	if !previewOf(t, cs, p.GetId()).GetAlreadyConfirmed() {
		t.Fatal("предпросмотр после подтверждения: already_confirmed=false")
	}
	// Хэш другого содержимого (здесь — cfgLocal) не подтверждает текущее.
	q := trojanWithCustom(t, cs, "", cfgLocal)
	if _, err := confirmHash(cs, q.GetId(), pv.GetCustomHash()); connect.CodeOf(err) != connect.CodeFailedPrecondition {
		t.Fatalf("чужой хэш: %v", err)
	}
}

func TestConfirmRejectsMalformedHash(t *testing.T) {
	cs := newE2E(t)
	p := trojanWithCustom(t, cs, "", cfgRemote)
	h := previewOf(t, cs, p.GetId()).GetCustomHash()
	for _, bad := range []string{"", "abc", strings.ToUpper(h), h[:63] + "g", h + "0", strings.Repeat("z", 64)} {
		if _, err := confirmHash(cs, p.GetId(), bad); connect.CodeOf(err) != connect.CodeInvalidArgument {
			t.Errorf("хэш %q: code=%v", bad, connect.CodeOf(err))
		}
	}
}

func TestConfirmWithoutCustomFieldsFailsPrecondition(t *testing.T) {
	cs := newE2E(t)
	p := trojanWithCustom(t, cs, "", "{}") // пустой объект — не содержимое
	_, err := cs.profile.PreviewCustomFields(context.Background(), connect.NewRequest(&lavrayv1.PreviewCustomFieldsRequest{ProfileId: p.GetId()}))
	if connect.CodeOf(err) != connect.CodeFailedPrecondition {
		t.Fatalf("Preview без custom-полей: %v", err)
	}
	if _, err := confirmHash(cs, p.GetId(), strings.Repeat("a", 64)); connect.CodeOf(err) != connect.CodeFailedPrecondition {
		t.Fatalf("Confirm без custom-полей: %v", err)
	}
}

func TestConfirmUnknownProfileNotFound(t *testing.T) {
	cs := newE2E(t)
	if _, err := confirmHash(cs, 9999, strings.Repeat("a", 64)); connect.CodeOf(err) != connect.CodeNotFound {
		t.Fatalf("Confirm: %v", err)
	}
	_, err := cs.profile.PreviewCustomFields(context.Background(), connect.NewRequest(&lavrayv1.PreviewCustomFieldsRequest{ProfileId: 9999}))
	if connect.CodeOf(err) != connect.CodeNotFound {
		t.Fatalf("Preview: %v", err)
	}
}

func TestEditingCustomJSONRevokesConfirmation(t *testing.T) {
	cs := newE2E(t)
	p := trojanWithCustom(t, cs, "", cfgRemote)
	if _, err := confirmHash(cs, p.GetId(), previewOf(t, cs, p.GetId()).GetCustomHash()); err != nil {
		t.Fatal(err)
	}
	p.CustomConfigJson = strp(cfgLocal) // A → B
	if updateProfile(t, cs, p).GetCustomFieldsConfirmed() {
		t.Fatal("правка custom_config не сняла подтверждение")
	}
	p.CustomConfigJson = strp(cfgRemote) // B → A
	if updateProfile(t, cs, p).GetCustomFieldsConfirmed() || confirmed(t, cs, p.GetId()) {
		t.Fatal("возврат к подтверждённому содержимому вернул подтверждение")
	}
}

func TestRenameKeepsConfirmation(t *testing.T) {
	cs := newE2E(t)
	p := trojanWithCustom(t, cs, `{"domain_strategy":"ipv4_only"}`, cfgRemote)
	if _, err := confirmHash(cs, p.GetId(), previewOf(t, cs, p.GetId()).GetCustomHash()); err != nil {
		t.Fatal(err)
	}
	p.Name = "renamed"
	p.Port = 8443
	if !updateProfile(t, cs, p).GetCustomFieldsConfirmed() {
		t.Fatal("правка без custom-полей сняла подтверждение")
	}
}

func TestWhitespaceEditKeepsConfirmation(t *testing.T) {
	cs := newE2E(t)
	p := trojanWithCustom(t, cs, "", cfgRemote)
	if _, err := confirmHash(cs, p.GetId(), previewOf(t, cs, p.GetId()).GetCustomHash()); err != nil {
		t.Fatal(err)
	}
	p.CustomConfigJson = strp("{\n  \"dns\": { \"final\": \"remote-marker\" }\n}\n")
	if !updateProfile(t, cs, p).GetCustomFieldsConfirmed() {
		t.Fatal("переформатирование (тот же канон) сняло подтверждение")
	}
}

func TestConfirmRaceWithConcurrentEdit(t *testing.T) {
	cs := newE2E(t)
	p := trojanWithCustom(t, cs, "", cfgRemote)
	// Последовательно: правка между предпросмотром и подтверждением.
	stale := previewOf(t, cs, p.GetId()).GetCustomHash()
	p.CustomConfigJson = strp(cfgLocal)
	updateProfile(t, cs, p)
	if _, err := confirmHash(cs, p.GetId(), stale); connect.CodeOf(err) != connect.CodeFailedPrecondition {
		t.Fatalf("устаревший хэш: %v", err)
	}
	// Параллельно: при любом порядке итог не «подтверждено чужое содержимое».
	for i := range 20 {
		cur, next := cfgLocal, fmt.Sprintf(`{"dns":{"final":"m%d"}}`, i)
		p.CustomConfigJson = strp(cur)
		updateProfile(t, cs, p)
		h := previewOf(t, cs, p.GetId()).GetCustomHash()
		var wg sync.WaitGroup
		wg.Add(2)
		go func() {
			defer wg.Done()
			_, _ = confirmHash(cs, p.GetId(), h)
		}()
		go func() {
			defer wg.Done()
			q := proto.CloneOf(p) // своя копия запроса для горутины
			q.CustomConfigJson = strp(next)
			_, _ = cs.profile.UpdateProfile(context.Background(), connect.NewRequest(&lavrayv1.UpdateProfileRequest{Profile: q}))
		}()
		wg.Wait()
		if confirmed(t, cs, p.GetId()) {
			t.Fatalf("итерация %d: подтверждено содержимое, которого не было в предпросмотре", i)
		}
	}
}

func TestConnectInlineInternalFullRejected(t *testing.T) {
	cs := newE2E(t)
	_, err := cs.core.Connect(context.Background(), connect.NewRequest(&lavrayv1.ConnectRequest{
		InlineProfile: internalFullProto(`{"outbounds":[{"type":"direct","tag":"direct"}]}`),
		ValidateOnly:  boolp(true),
	}))
	if connect.CodeOf(err) != connect.CodeInvalidArgument {
		t.Fatalf("inline internal-full: %v", err)
	}
}

func internalFullProto(cfg string) *lavrayv1.Profile {
	return &lavrayv1.Profile{
		GroupId: 1, Type: lavrayv1.ProfileType_PROFILE_TYPE_CUSTOM_CONFIG, Name: "full",
		Detail:                &lavrayv1.Profile_Custom{Custom: &lavrayv1.CustomDetail{Core: lavrayv1.CustomDetail_CORE_INTERNAL_FULL, FullConfigJson: strp(cfg)}},
		CustomFieldsConfirmed: true,
	}
}

func TestUnconfirmedInternalFullRejected(t *testing.T) {
	cs := newE2E(t)
	ctx := context.Background()
	created, err := cs.profile.CreateProfile(ctx, connect.NewRequest(&lavrayv1.CreateProfileRequest{
		Profile: internalFullProto(`{"outbounds":[{"type":"direct","tag":"direct"}]}`),
	}))
	if err != nil {
		t.Fatal(err)
	}
	id := created.Msg.GetId()
	connectOnly := func() error {
		_, err := cs.core.Connect(ctx, connect.NewRequest(&lavrayv1.ConnectRequest{ProfileId: int64p(id), ValidateOnly: boolp(true)}))
		return err
	}
	if err := connectOnly(); connect.CodeOf(err) != connect.CodeInvalidArgument {
		t.Fatalf("неподтверждённый internal-full: %v", err)
	}
	pv := previewOf(t, cs, id)
	if !strings.Contains(strings.Join(pv.GetWarnings(), ","), warnInternalFull) || !strings.Contains(pv.GetDiff(), "+") {
		t.Fatalf("предпросмотр internal-full: warnings=%v", pv.GetWarnings())
	}
	if _, err := confirmHash(cs, id, pv.GetCustomHash()); err != nil {
		t.Fatal(err)
	}
	if err := connectOnly(); err != nil {
		t.Fatalf("подтверждённый internal-full: %v", err)
	}
}

func TestPreviewCustomFieldsRedactsSecrets(t *testing.T) {
	cs := newE2E(t)
	p := trojanWithCustom(t, cs, `{"domain_strategy":"ipv4_only"}`, cfgRemote)
	pv := previewOf(t, cs, p.GetId())
	if pv.GetDiff() == "" {
		t.Fatal("пустой diff")
	}
	if strings.Contains(pv.GetDiff(), "Sup3rS3cretPassw0rd") {
		t.Fatalf("пароль в diff:\n%s", pv.GetDiff())
	}
	if !strings.Contains(pv.GetDiff(), "+") || !strings.Contains(pv.GetDiff(), "remote-marker") {
		t.Fatalf("в diff нет custom-содержимого:\n%s", pv.GetDiff())
	}
}

func TestPreviewTouchedKeys(t *testing.T) {
	cs := newE2E(t)
	p := trojanWithCustom(t, cs, `{"domain_strategy":"ipv4_only"}`, cfgRemote)
	keys := strings.Join(previewOf(t, cs, p.GetId()).GetTouchedKeys(), " ")
	if !strings.Contains(keys, "dns.final") {
		t.Errorf("нет dns.final: %s", keys)
	}
	if !strings.Contains(keys, "].domain_strategy") || !strings.Contains(keys, "outbounds[") {
		t.Errorf("нет outbounds[i].domain_strategy: %s", keys)
	}
}

func TestPreviewHugeConfigTruncated(t *testing.T) {
	cs := newE2E(t)
	var rules []string
	for i := range 5000 {
		rules = append(rules, fmt.Sprintf(`{"domain":["d%d.example"],"outbound":"direct"}`, i))
	}
	p := trojanWithCustom(t, cs, "", `{"route":{"+rules":[`+strings.Join(rules, ",")+`]}}`)
	pv := previewOf(t, cs, p.GetId())
	if !pv.GetDiffTruncated() {
		t.Fatal("ждём diff_truncated")
	}
	if n := strings.Count(pv.GetDiff(), "\n"); n > maxDiffLines {
		t.Fatalf("строк diff %d > %d", n, maxDiffLines)
	}
}

func TestPreviewInvalidCustomJSON(t *testing.T) {
	cs := newE2E(t)
	// Профиль, сохранённый до проверок (импорт старой версии): JSON с дублями.
	m := &model.Profile{GroupID: 1, Type: model.TypeSOCKS, Name: "old", Address: "198.51.100.1", Port: 1080,
		Detail: &model.SocksHTTPDetail{Proto: "socks5"}, CustomConfigJSON: `{"dns":{},"dns":{}}`}
	if err := cs.st.CreateProfile(m); err != nil {
		t.Fatal(err)
	}
	_, err := cs.profile.PreviewCustomFields(context.Background(), connect.NewRequest(&lavrayv1.PreviewCustomFieldsRequest{ProfileId: m.ID}))
	if connect.CodeOf(err) != connect.CodeInvalidArgument {
		t.Fatalf("Preview: %v", err)
	}
	// Переименование такого профиля не требует чинить его custom-JSON.
	got, err := cs.profile.GetProfile(context.Background(), connect.NewRequest(&lavrayv1.GetProfileRequest{Id: m.ID}))
	if err != nil {
		t.Fatal(err)
	}
	got.Msg.Name = "renamed"
	updateProfile(t, cs, got.Msg)
}

func TestCreateProfileRejectsBadCustomJSON(t *testing.T) {
	cs := newE2E(t)
	for name, bad := range map[string]string{
		"не объект":      `[1,2]`,
		"дубль ключей":   `{"dns":{},"dns":{}}`,
		"мусор в конце":  `{"dns":{}} {}`,
		"больше 256 КиБ": `{"route":{"x":"` + strings.Repeat("a", maxCustomJSONBytes) + `"}}`,
	} {
		in := &lavrayv1.Profile{GroupId: 1, Type: lavrayv1.ProfileType_PROFILE_TYPE_SOCKS, Name: "x", Address: "198.51.100.2", Port: 1,
			Detail:           &lavrayv1.Profile_SocksHttp{SocksHttp: &lavrayv1.SocksHttpDetail{Proto: lavrayv1.SocksHttpDetail_PROTO_SOCKS5}},
			CustomConfigJson: strp(bad)}
		_, err := cs.profile.CreateProfile(context.Background(), connect.NewRequest(&lavrayv1.CreateProfileRequest{Profile: in}))
		if connect.CodeOf(err) != connect.CodeInvalidArgument {
			t.Errorf("%s: code=%v", name, connect.CodeOf(err))
		}
	}
}

// runtimeConfig подключает профиль через FakeRunner и возвращает конфиг.
func runtimeConfig(t *testing.T, cs *clients, id int64) string {
	t.Helper()
	ctx := context.Background()
	if _, err := cs.core.Connect(ctx, connect.NewRequest(&lavrayv1.ConnectRequest{ProfileId: int64p(id)})); err != nil {
		t.Fatalf("Connect: %v", err)
	}
	defer func() { _, _ = cs.core.Disconnect(ctx, connect.NewRequest(&lavrayv1.DisconnectRequest{})) }()
	rc, err := cs.core.GetRuntimeConfig(ctx, connect.NewRequest(&lavrayv1.GetRuntimeConfigRequest{}))
	if err != nil {
		t.Fatal(err)
	}
	return rc.Msg.GetSingBoxJson()
}

func TestUnconfirmedCustomConfigIsNotMerged(t *testing.T) {
	cs := newE2E(t)
	p := trojanWithCustom(t, cs, "", cfgRemote)
	if strings.Contains(runtimeConfig(t, cs, p.GetId()), "remote-marker") {
		t.Fatal("неподтверждённый custom_config применён")
	}
	if _, err := confirmHash(cs, p.GetId(), previewOf(t, cs, p.GetId()).GetCustomHash()); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(runtimeConfig(t, cs, p.GetId()), "remote-marker") {
		t.Fatal("подтверждённый custom_config не применён")
	}
}

func TestChainMemberConfirmationPerMember(t *testing.T) {
	cs := newE2E(t)
	a := trojanWithCustom(t, cs, `{"domain_strategy":"ipv4_only"}`, "")
	b := trojanWithCustom(t, cs, `{"domain_strategy":"ipv6_only"}`, "")
	if _, err := confirmHash(cs, a.GetId(), previewOf(t, cs, a.GetId()).GetCustomHash()); err != nil {
		t.Fatal(err)
	}
	chain, err := cs.profile.CreateProfile(context.Background(), connect.NewRequest(&lavrayv1.CreateProfileRequest{Profile: &lavrayv1.Profile{
		GroupId: 1, Type: lavrayv1.ProfileType_PROFILE_TYPE_CHAIN, Name: "chain",
		Detail: &lavrayv1.Profile_Chain{Chain: &lavrayv1.ChainDetail{ProfileIds: []int64{a.GetId(), b.GetId()}}},
	}}))
	if err != nil {
		t.Fatal(err)
	}
	cfg := runtimeConfig(t, cs, chain.Msg.GetId())
	if !strings.Contains(cfg, "ipv4_only") || strings.Contains(cfg, "ipv6_only") {
		t.Fatalf("в цепочке подтверждение действует на каждого участника отдельно:\n%s", cfg)
	}
}

func TestOldClientAllowCustomFieldsIgnored(t *testing.T) {
	// Клиент 0.2.x шлёт удалённое поле allow_custom_fields: запрос принимается,
	// поле ни на что не влияет (proto: reserved 6).
	_, httpSrv := newE2EServer(t, Deps{Runner: &inst.FakeRunner{}}, nil)
	body := `{"content":"trojan://p@tr.example.com:443#a","previewOnly":true,"allowCustomFields":true}`
	req, err := http.NewRequest(http.MethodPost, httpSrv.URL+lavrayv1connect.ProfileServiceImportContentProcedure, bytes.NewBufferString(body))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := httpSrv.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("старый клиент: HTTP %d", resp.StatusCode)
	}
}

func TestAgentDeniedCustomFieldRPCs(t *testing.T) {
	for _, proc := range []string{
		lavrayv1connect.ProfileServicePreviewCustomFieldsProcedure,
		lavrayv1connect.ProfileServiceConfirmCustomFieldsProcedure,
	} {
		for _, mode := range []string{AgentModeRead, AgentModeControl} {
			if err := agentAllowed(mode, proc); connect.CodeOf(err) != connect.CodePermissionDenied {
				t.Errorf("%s в режиме %s: %v", proc, mode, err)
			}
		}
	}
}
