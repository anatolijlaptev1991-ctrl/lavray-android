// SPDX-License-Identifier: GPL-3.0-only

package cfgbuild

import (
	"encoding/json"
	"strings"
	"testing"

	"connectrpc.com/connect"

	"github.com/anatolijlaptev1991-ctrl/lavray-android/core/internal/model"
)

func socksProfile(id int64, addr string, port int) *model.Profile {
	return &model.Profile{
		ID:      id,
		Type:    model.TypeSOCKS,
		Address: addr,
		Port:    port,
		Detail:  &model.SocksHTTPDetail{Proto: "socks5"},
	}
}

// lookupPool — ProfileLookup из среза профилей.
func lookupPool(profiles ...*model.Profile) ProfileLookup {
	return func(id int64) (*model.Profile, error) {
		for _, p := range profiles {
			if p.ID == id {
				return p, nil
			}
		}
		return nil, nil
	}
}

func outboundByTag(t *testing.T, res *BuildResult, tag string) map[string]any {
	t.Helper()
	// Config может хранить outbounds как []map[string]any (прямой Build)
	// или []any (после JSON-круга) — хелпер принимает оба.
	switch outs := res.Config["outbounds"].(type) {
	case []map[string]any:
		for _, m := range outs {
			if m["tag"] == tag {
				return m
			}
		}
	case []any:
		for _, o := range outs {
			if m, ok := o.(map[string]any); ok && m["tag"] == tag {
				return m
			}
		}
	default:
		t.Fatalf("outbounds неожиданного типа %T", res.Config["outbounds"])
	}
	t.Fatalf("outbound с тегом %q не найден", tag)
	return nil
}

// ---- G16: цепочка из 3 + групповой front proxy -------------------------------

func TestChainTagsAndDetours(t *testing.T) {
	exit := socksProfile(11, "1.1.1.1", 1080)
	mid := socksProfile(22, "2.2.2.2", 1080)
	entry := socksProfile(33, "3.3.3.3", 1080)
	front := socksProfile(44, "4.4.4.4", 1080)
	chain := &model.Profile{
		ID:     99,
		Type:   model.TypeChain,
		Detail: &model.ChainDetail{ProfileIDs: []int64{33, 22, 11}}, // вход → выход
	}
	frontID := int64(44)
	res, err := Build(BuildRequest{
		Profile: chain,
		Group:   &model.Group{ID: 1, FrontProxyID: &frontID},
		Lookup:  lookupPool(chain, exit, mid, entry, front),
	})
	if err != nil {
		t.Fatal(err)
	}

	if res.OutboundTag != "proxy" {
		t.Fatalf("OutboundTag = %q, want proxy", res.OutboundTag)
	}
	// Порядок тегов: proxy (выход), c-0-22, c-0-33, g-44 (вход=front).
	wantTags := []string{"proxy", "c-0-22", "c-0-33", "g-44"}
	if len(res.OutboundTags) != len(wantTags) {
		t.Fatalf("OutboundTags = %v, want %v", res.OutboundTags, wantTags)
	}
	for i, want := range wantTags {
		if res.OutboundTags[i] != want {
			t.Fatalf("OutboundTags[%d] = %q, want %q (все: %v)", i, res.OutboundTags[i], want, res.OutboundTags)
		}
	}
	// detour: proxy → c-0-22 → c-0-33 → g-44.
	if got := outboundByTag(t, res, "proxy")["detour"]; got != "c-0-22" {
		t.Fatalf("proxy.detour = %v", got)
	}
	if got := outboundByTag(t, res, "c-0-22")["detour"]; got != "c-0-33" {
		t.Fatalf("c-0-22.detour = %v", got)
	}
	if got := outboundByTag(t, res, "c-0-33")["detour"]; got != "g-44" {
		t.Fatalf("c-0-33.detour = %v", got)
	}
	if _, ok := outboundByTag(t, res, "g-44")["detour"]; ok {
		t.Fatal("g-44 (вход) не должен иметь detour")
	}
	// ignoreConnTags — все, кроме proxy.
	wantIgnore := []string{"c-0-22", "c-0-33", "g-44"}
	if len(res.IgnoreConnTags) != len(wantIgnore) {
		t.Fatalf("IgnoreConnTags = %v", res.IgnoreConnTags)
	}
	// outbounds-минимум.
	for _, tag := range []string{"direct", "bypass", "block"} {
		outboundByTag(t, res, tag)
	}
	// Домены серверов для dns-direct (W5): адреса — IP, списка быть не должно.
	if len(res.ServerDomains) != 0 {
		t.Fatalf("ServerDomains = %v, want пусто (IP-адреса)", res.ServerDomains)
	}
}

func TestChainDomainServersCollected(t *testing.T) {
	p := socksProfile(1, "example.org", 1080)
	res, err := Build(BuildRequest{Profile: p})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.ServerDomains) != 1 || res.ServerDomains[0] != "full:example.org" {
		t.Fatalf("ServerDomains = %v", res.ServerDomains)
	}
}

// Одиночный профиль: тег proxy, без g-.
func TestSingleProfileTag(t *testing.T) {
	res, err := Build(BuildRequest{Profile: socksProfile(7, "1.2.3.4", 1080)})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.OutboundTags) != 1 || res.OutboundTags[0] != "proxy" {
		t.Fatalf("OutboundTags = %v", res.OutboundTags)
	}
	if len(res.IgnoreConnTags) != 0 {
		t.Fatalf("IgnoreConnTags = %v", res.IgnoreConnTags)
	}
}

// chain-in-chain и отсутствующий член — ошибки.
func TestChainErrors(t *testing.T) {
	inner := &model.Profile{ID: 2, Type: model.TypeChain, Detail: &model.ChainDetail{}}
	outer := &model.Profile{ID: 1, Type: model.TypeChain, Detail: &model.ChainDetail{ProfileIDs: []int64{2}}}
	_, err := Build(BuildRequest{Profile: outer, Lookup: lookupPool(outer, inner)})
	if err == nil || !wantMsg(err, "chain in chain") {
		t.Fatalf("chain-in-chain: %v", err)
	}

	missing := &model.Profile{ID: 1, Type: model.TypeChain, Detail: &model.ChainDetail{ProfileIDs: []int64{42}}}
	_, err = Build(BuildRequest{Profile: missing, Lookup: lookupPool(missing)})
	if err == nil || !wantMsg(err, "chain missing ent") {
		t.Fatalf("chain missing: %v", err)
	}
}

// Некорректный порт (LNB-15) и пустой адрес — INVALID_ARGUMENT.
func TestProfileValidation(t *testing.T) {
	p := socksProfile(1, "1.2.3.4", 0)
	_, err := Build(BuildRequest{Profile: p})
	e, ok := AsError(err)
	if !ok || e.Code != connect.CodeInvalidArgument {
		t.Fatalf("port=0: %v", err)
	}
	p = socksProfile(1, " ", 1080)
	_, err = Build(BuildRequest{Profile: p})
	if _, ok := AsError(err); !ok {
		t.Fatalf("пустой адрес: %v", err)
	}
}

// ---- custom internal / internal-full ----------------------------------------

func TestCustomInternalOutbound(t *testing.T) {
	p := &model.Profile{
		ID: 5, Type: model.TypeCustomOutbound, Address: "ignored", Port: 1,
		Detail: &model.CustomDetail{
			Core:         "internal",
			OutboundJSON: `{"type":"socks","server":"5.6.7.8","server_port":1080}`,
		},
	}
	res, err := Build(BuildRequest{Profile: p})
	if err != nil {
		t.Fatal(err)
	}
	ob := outboundByTag(t, res, "proxy")
	if ob["type"] != "socks" || ob["server"] != "5.6.7.8" {
		t.Fatalf("custom outbound = %v", ob)
	}
	// server из custom JSON попадает в dns-direct список.
	if len(res.ServerDomains) != 0 {
		t.Fatalf("ServerDomains = %v (сервер — IP, должно быть пусто)", res.ServerDomains)
	}
}

func TestCustomInternalFull(t *testing.T) {
	full := `{"log":{"level":"warn"},"outbounds":[{"type":"direct","tag":"x"}]}`
	p := &model.Profile{
		ID: 6, Type: model.TypeCustomConfig, Address: "n/a", Port: 1,
		Detail:                &model.CustomDetail{Core: "internal-full", FullConfigJSON: full},
		CustomFieldsConfirmed: true, // L2: произвольный конфиг требует подтверждения
	}
	res, err := Build(BuildRequest{Profile: p})
	if err != nil {
		t.Fatal(err)
	}
	if res.Config["log"].(map[string]any)["level"] != "warn" {
		t.Fatalf("internal-full конфиг не «как есть»: %v", res.Config)
	}
	if len(res.OutboundTags) != 0 {
		t.Fatalf("OutboundTags = %v, want пусто", res.OutboundTags)
	}
	// validateEnt не должен применяться к internal-full (адрес «n/a»).
}

// internal-full + custom_config через allowlist.
func TestCustomInternalFullPlusCustomConfig(t *testing.T) {
	full := `{"log":{"level":"warn"},"outbounds":[{"type":"direct","tag":"x"}]}`
	p := &model.Profile{
		ID: 6, Type: model.TypeCustomConfig, Address: "n/a", Port: 1,
		Detail:                &model.CustomDetail{Core: "internal-full", FullConfigJSON: full},
		CustomConfigJSON:      `{"log":{"level":"trace"}}`,
		CustomFieldsConfirmed: true,
	}
	res, err := Build(BuildRequest{Profile: p})
	if err != nil {
		t.Fatal(err)
	}
	if res.Config["log"].(map[string]any)["level"] != "trace" {
		t.Fatalf("custom_config не смержился: %v", res.Config)
	}
}

// Не подтверждённые custom-поля импортированного профиля не применяются
// (LNB-25/FR-UI-257, защита на уровне билдера).
func TestUnconfirmedCustomFieldsSkipped(t *testing.T) {
	p := socksProfile(8, "1.2.3.4", 1080)
	p.CustomOutboundJSON = `{"server_port": 9999}`
	p.CustomConfigJSON = `{"log":{"level":"trace"}}`
	p.CustomFieldsConfirmed = false
	res, err := Build(BuildRequest{Profile: p})
	if err != nil {
		t.Fatal(err)
	}
	ob := outboundByTag(t, res, "proxy")
	if ob["server_port"] != 1080 {
		t.Fatalf("custom_outbound применён без подтверждения: %v", ob)
	}
	if res.Config["log"].(map[string]any)["level"] != "info" {
		t.Fatalf("custom_config применён без подтверждения")
	}
}

// ---- mux (§3.1.10) -----------------------------------------------------------

func TestMuxRules(t *testing.T) {
	newVMess := func(network string, multiplex int) *model.Profile {
		return &model.Profile{
			ID: 1, Type: model.TypeVMess, Address: "vm.example", Port: 443,
			Detail: &model.VMessDetail{UUID: "b831381d-6324-4d53-ad4f-8cda48b30811", Security: "auto"},
			Stream: &model.StreamSettings{Network: network, Security: "tls", Multiplex: multiplex},
		}
	}
	st := DefaultSettings()
	st.Mux.Concurrency = 8

	// G07: vmess + grpc + mux on (multiplex=1) — мультиплекс применяется.
	res, err := Build(BuildRequest{Profile: newVMess("grpc", 1), Settings: st})
	if err != nil {
		t.Fatal(err)
	}
	ob := outboundByTag(t, res, "proxy")
	mp, ok := ob["multiplex"].(map[string]any)
	if !ok {
		t.Fatalf("grpc+on: multiplex отсутствует: %v", ob)
	}
	if mp["protocol"] != "h2mux" || mp["max_streams"] != 8 {
		t.Fatalf("multiplex = %v", mp)
	}

	// grpc + multiplex=default(0) при default_on=false → нет.
	res, err = Build(BuildRequest{Profile: newVMess("grpc", 0), Settings: st})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := outboundByTag(t, res, "proxy")["multiplex"]; ok {
		t.Fatal("grpc+default: multiplex не должен применяться")
	}

	// concurrency <= 0 — глобально выключен.
	st2 := DefaultSettings()
	res, err = Build(BuildRequest{Profile: newVMess("ws", 1), Settings: st2})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := outboundByTag(t, res, "proxy")["multiplex"]; ok {
		t.Fatal("concurrency=-1: multiplex не должен применяться")
	}

	// vless с flow — никогда.
	vless := &model.Profile{
		ID: 2, Type: model.TypeVLESS, Address: "vl.example", Port: 443,
		Detail: &model.TrojanVlessDetail{Password: "b831381d-6324-4d53-ad4f-8cda48b30811", Flow: "xtls-rprx-vision"},
		Stream: &model.StreamSettings{Network: "tcp", Multiplex: 1},
	}
	res, err = Build(BuildRequest{Profile: vless, Settings: st})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := outboundByTag(t, res, "proxy")["multiplex"]; ok {
		t.Fatal("vless+flow: multiplex не должен применяться")
	}
}

// mux применяется только один раз на цепочку.
func TestMuxOncePerChain(t *testing.T) {
	mk := func(id int64) *model.Profile {
		return &model.Profile{
			ID: id, Type: model.TypeVMess, Address: "a.example", Port: 443,
			Detail: &model.VMessDetail{UUID: "b831381d-6324-4d53-ad4f-8cda48b30811"},
			Stream: &model.StreamSettings{Network: "ws", Multiplex: 1},
		}
	}
	a, b, c := mk(1), mk(2), mk(3)
	chain := &model.Profile{ID: 9, Type: model.TypeChain, Detail: &model.ChainDetail{ProfileIDs: []int64{1, 2, 3}}}
	st := DefaultSettings()
	st.Mux.Concurrency = 4
	res, err := Build(BuildRequest{Profile: chain, Lookup: lookupPool(chain, a, b, c), Settings: st})
	if err != nil {
		t.Fatal(err)
	}
	count := 0
	switch outs := res.Config["outbounds"].(type) {
	case []map[string]any:
		for _, m := range outs {
			if _, ok := m["multiplex"]; ok {
				count++
			}
		}
	case []any:
		for _, o := range outs {
			if m, ok := o.(map[string]any); ok {
				if _, ok := m["multiplex"]; ok {
					count++
				}
			}
		}
	default:
		t.Fatalf("outbounds неожиданного типа %T", res.Config["outbounds"])
	}
	if count != 1 {
		t.Fatalf("multiplex применён %d раз, want 1", count)
	}
}

// ---- внешние ядра: за флагом, W4 отвергает -----------------------------------

func TestExternalCoreRejected(t *testing.T) {
	naive := &model.Profile{
		ID: 3, Type: model.TypeNaive, Address: "n.example", Port: 443,
		Detail: &model.NaiveDetail{Username: "u", Password: "p"},
	}
	_, err := Build(BuildRequest{Profile: naive})
	if err == nil {
		t.Fatal("naive без флага enable_external_cores должен отвергаться")
	}
	ext := &model.Profile{
		ID: 4, Type: model.TypeCustomExternal, Address: "n/a", Port: 1,
		Detail: &model.CustomDetail{Core: "external", Command: []string{"xray"}},
	}
	_, err = Build(BuildRequest{Profile: ext})
	if err == nil {
		t.Fatal("custom external без флага должен отвергаться")
	}
}

func wantMsg(err error, sub string) bool {
	return err != nil && strings.Contains(err.Error(), sub)
}

// Дополнительная проверка: конфиг маршалится детерминированно.
func TestMarshalDeterministic(t *testing.T) {
	res, err := Build(BuildRequest{Profile: socksProfile(1, "1.2.3.4", 1080)})
	if err != nil {
		t.Fatal(err)
	}
	a, _ := MarshalIndent(res.Config)
	b, _ := MarshalIndent(res.Config)
	if string(a) != string(b) {
		t.Fatal("недетерминированный маршал")
	}
	var check map[string]any
	if err := json.Unmarshal(a, &check); err != nil {
		t.Fatalf("невалидный JSON: %v", err)
	}
}

// L2 аудита №1: internal-full без подтверждения пользователем не применяется
// (иначе импортированный профиль поднимал произвольный inbound).
func TestCustomInternalFullRequiresConfirmation(t *testing.T) {
	full := `{"inbounds":[{"listen":"0.0.0.0","listen_port":9}],"outbounds":[{"type":"direct","tag":"x"}]}`
	p := &model.Profile{
		ID: 7, Type: model.TypeCustomConfig, Address: "n/a", Port: 1,
		Detail: &model.CustomDetail{Core: "internal-full", FullConfigJSON: full},
	}
	if _, err := Build(BuildRequest{Profile: p}); err == nil {
		t.Fatal("internal-full без custom_fields_confirmed должен быть отвергнут")
	}
}
