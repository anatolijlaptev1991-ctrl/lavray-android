// SPDX-License-Identifier: GPL-3.0-only

package api

// preset_exchange_test.go — обмен наборами файлом, импорт маршрута NekoRay,
// чистая PlanInsert и связь правок набора с собранным конфигом (T-80).

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"

	"connectrpc.com/connect"
	"google.golang.org/protobuf/proto"

	lavrayv1 "github.com/anatolijlaptev1991-ctrl/lavray-android/core/internal/gen/lavrayv1"
	"github.com/anatolijlaptev1991-ctrl/lavray-android/core/internal/presets"
)

func exportPreset(t *testing.T, cs *clients, id string) *lavrayv1.ExportPresetResponse {
	t.Helper()
	res, err := cs.routing.ExportPreset(bg, connect.NewRequest(&lavrayv1.ExportPresetRequest{Id: id}))
	if err != nil {
		t.Fatalf("ExportPreset(%s): %v", id, err)
	}
	return res.Msg
}

func importPreset(cs *clients, text string) (*lavrayv1.ImportPresetResponse, error) {
	res, err := cs.routing.ImportPreset(bg, connect.NewRequest(&lavrayv1.ImportPresetRequest{Json: text}))
	if err != nil {
		return nil, err
	}
	return res.Msg, nil
}

// TestExportImportRoundTrip (D): экспорт набора, импорт файла и повторный
// экспорт дают тот же файл, кроме id; импорт того же файла дважды — два набора.
func TestExportImportRoundTrip(t *testing.T) {
	cs := newE2E(t)
	cp := copyOf(t, cs, "lav-ru")
	edit := proto.Clone(cp).(*lavrayv1.RoutingPreset)
	edit.DisplayName = "Экспорт RU"
	edit.CustomRulesJson = strPtr(`{"rules":[{"domain":["pin.example"],"outbound":"proxy"}]}`)
	upd, err := updatePreset(cs, edit, cp.Revision)
	if err != nil {
		t.Fatal(err)
	}
	first := exportPreset(t, cs, cp.Id)
	if first.SuggestedName != "eksport-ru" || !strings.HasPrefix(first.Json, "{\n  \"$schema\"") {
		t.Fatalf("экспорт: name=%q\n%.200s", first.SuggestedName, first.Json)
	}
	imp, err := importPreset(cs, first.Json)
	if err != nil || len(imp.Warnings) != 0 {
		t.Fatalf("импорт: %v %v", imp, err)
	}
	if imp.Preset.Id == cp.Id || !strings.HasPrefix(imp.Preset.Id, "custom:") || imp.Preset.Revision != 1 {
		t.Fatalf("импортированный: id=%q rev=%d", imp.Preset.Id, imp.Preset.Revision)
	}
	if imp.Preset.DisplayName != "Экспорт RU" || imp.Preset.CustomRulesJson == nil ||
		!strings.HasPrefix(*imp.Preset.CustomRulesJson, "[") {
		t.Fatalf("содержимое: %q rules=%v", imp.Preset.DisplayName, imp.Preset.CustomRulesJson)
	}
	second := exportPreset(t, cs, imp.Preset.Id)
	norm := func(s string) string {
		var m map[string]any
		if err := json.Unmarshal([]byte(s), &m); err != nil {
			t.Fatal(err)
		}
		for _, k := range []string{"id", "revision", "source_id", "source_revision"} {
			delete(m, k)
		}
		b, _ := json.Marshal(m)
		return string(b)
	}
	if norm(first.Json) != norm(second.Json) {
		t.Fatalf("повторный экспорт отличается:\n%s\n%s", norm(first.Json), norm(second.Json))
	}
	again, err := importPreset(cs, first.Json)
	if err != nil || again.Preset.Id == imp.Preset.Id {
		t.Fatalf("второй импорт: %v %v", again, err)
	}
	if upd.NewRevision != 2 {
		t.Fatalf("ревизия исходного: %d", upd.NewRevision)
	}
	// Встроенный экспортируется, и его файл импортируется как обычный набор.
	b := exportPreset(t, cs, "global")
	if b.SuggestedName != "global" {
		t.Fatalf("имя встроенного: %q", b.SuggestedName)
	}
	g, err := importPreset(cs, b.Json)
	if err != nil || g.Preset.Builtin {
		t.Fatalf("импорт файла встроенного: %v %v", g, err)
	}
	_, err = cs.routing.ExportPreset(bg, connect.NewRequest(&lavrayv1.ExportPresetRequest{Id: "custom:none"}))
	wantCode(t, err, connect.CodeNotFound, "Export несуществующего")
}

// Имя файла для «Сохранить как» не совпадает с именами устройств Windows.
func TestExportSuggestedNameAvoidsDeviceNames(t *testing.T) {
	cs := newE2E(t)
	for name, want := range map[string]string{"CON": "preset-con", "com1": "preset-com1", "Lpt9": "preset-lpt9", "console": "console", "com0": "com0"} {
		made, err := fromValues(t, cs, &lavrayv1.RoutingPreset{DisplayName: name})
		if err != nil {
			t.Fatal(err)
		}
		if got := exportPreset(t, cs, made.Id).SuggestedName; got != want {
			t.Errorf("%s: имя файла %q, want %q", name, got, want)
		}
	}
}

// TestImportNeverTakesBuiltinID (S): файл с id и флагом встроенного, с чужим
// источником — набор всё равно пользовательский с новым id; ни встроенный, ни
// существующий набор не затронуты.
func TestImportNeverTakesBuiltinID(t *testing.T) {
	cs := newE2E(t)
	orig := getPreset(t, cs, "lav-ru")
	file := exportPreset(t, cs, "lav-ru").Json // id "lav-ru", builtin=true
	for i := 0; i < 2; i++ {
		got, err := importPreset(cs, file)
		if err != nil {
			t.Fatal(err)
		}
		p := got.Preset
		if p.Builtin || p.Id == "lav-ru" || !strings.HasPrefix(p.Id, "custom:") || p.BuiltinSourceId != "" || p.BuiltinSourceRevision != 0 {
			t.Fatalf("импорт %d: id=%q builtin=%v source=%q/%d", i, p.Id, p.Builtin, p.BuiltinSourceId, p.BuiltinSourceRevision)
		}
		if p.DisplayName != "LAV Preset RU" {
			t.Fatalf("имя из файла: %q", p.DisplayName)
		}
	}
	forged := strings.Replace(file, `"id": "lav-ru"`, `"id": "global"`, 1)
	forged = strings.Replace(forged, `"revision": 1`, `"revision": 999`, 1)
	got, err := importPreset(cs, forged)
	if err != nil || got.Preset.Id == "global" || got.Preset.Revision != 1 {
		t.Fatalf("подделанный id/revision: %+v %v", got, err)
	}
	if now := getPreset(t, cs, "lav-ru"); !proto.Equal(now, orig) {
		t.Fatal("встроенный набор изменился")
	}
	if now := getPreset(t, cs, "global"); !now.Builtin {
		t.Fatal("global перестал быть встроенным")
	}
}

// TestImportOversize (B): файл ровно в 1 МиБ принимается, на байт больше —
// RESOURCE_EXHAUSTED; мусор и чужие форматы — INVALID_ARGUMENT.
func TestImportOversize(t *testing.T) {
	cs := newE2E(t)
	file := exportPreset(t, cs, "global").Json
	atLimit := file + strings.Repeat(" ", presets.MaxImportBytes-len(file))
	if _, err := importPreset(cs, atLimit); err != nil {
		t.Fatalf("ровно 1 МиБ: %v", err)
	}
	_, err := importPreset(cs, atLimit+" ")
	wantCode(t, err, connect.CodeResourceExhausted, "1 МиБ + 1 байт")
	for _, bad := range []string{"", "not json", "[]", `{"unknown":1}`, `{"routing":7}`} {
		_, err := importPreset(cs, bad)
		wantCode(t, err, connect.CodeInvalidArgument, fmt.Sprintf("формат %q", bad))
	}
	// Новее поддерживаемой схемы и битый список — INVALID_ARGUMENT с путём.
	future := strings.Replace(file, `"version": 1`, `"version": 7`, 1)
	_, err = importPreset(cs, future)
	if connect.CodeOf(err) != connect.CodeInvalidArgument || !strings.Contains(err.Error(), "version") {
		t.Fatalf("версия схемы: %v", err)
	}
	var doc map[string]any
	if err := json.Unmarshal([]byte(file), &doc); err != nil {
		t.Fatal(err)
	}
	doc["routing"].(map[string]any)["direct_domain"] = []string{"geoip:ru"}
	badList, _ := json.Marshal(doc)
	_, err = importPreset(cs, string(badList))
	if connect.CodeOf(err) != connect.CodeInvalidArgument || !strings.Contains(err.Error(), "direct_domain") {
		t.Fatalf("битый список: %v", err)
	}
}

// TestImportNekoRayRouteCreatesPreset: маршрут NekoRay → пользовательский набор
// с процессными правилами и предупреждениями конвертера; имя из запроса.
func TestImportNekoRayRouteCreatesPreset(t *testing.T) {
	cs := newE2E(t)
	src, err := os.ReadFile("../presets/testdata/lav-ru.source.json")
	if err != nil {
		t.Fatal(err)
	}
	name := "RU из NekoBox"
	res, err := cs.routing.ImportNekoRayRoute(bg, connect.NewRequest(&lavrayv1.ImportNekoRayRouteRequest{NekoboxJson: string(src), Name: &name}))
	if err != nil {
		t.Fatal(err)
	}
	p := res.Msg.Preset
	if p.Builtin || !strings.HasPrefix(p.Id, "custom:") || p.DisplayName != name || len(p.ProcessRules) == 0 || len(p.DirectDomain) == 0 {
		t.Fatalf("набор: %+v", p)
	}
	if len(res.Msg.Warnings) == 0 {
		t.Error("предупреждения конвертера не переданы")
	}
	if got := getPreset(t, cs, p.Id); got.Revision != 1 {
		t.Fatalf("не сохранён: %+v", got)
	}
	for _, bad := range []string{"", "null", "[]", "{oops"} {
		_, err := cs.routing.ImportNekoRayRoute(bg, connect.NewRequest(&lavrayv1.ImportNekoRayRouteRequest{NekoboxJson: bad}))
		wantCode(t, err, connect.CodeInvalidArgument, fmt.Sprintf("маршрут %q", bad))
	}
	huge := `{"direct_domain":"a"}` + strings.Repeat(" ", presets.MaxImportBytes)
	_, err = cs.routing.ImportNekoRayRoute(bg, connect.NewRequest(&lavrayv1.ImportNekoRayRouteRequest{NekoboxJson: huge}))
	wantCode(t, err, connect.CodeResourceExhausted, "маршрут больше 1 МиБ")
	// Без имени — запасное имя, набор создаётся.
	def, err := cs.routing.ImportNekoRayRoute(bg, connect.NewRequest(&lavrayv1.ImportNekoRayRouteRequest{NekoboxJson: `{"direct_domain":"example.org"}`}))
	if err != nil || def.Msg.Preset.DisplayName == "" {
		t.Fatalf("без имени: %+v %v", def, err)
	}
}

// TestProcessRulesAndDnsRoundTrip: процессные правила, DNS и custom-правила
// проходят Create → Get → Update → Get без потерь.
func TestProcessRulesAndDnsRoundTrip(t *testing.T) {
	cs := newE2E(t)
	in := &lavrayv1.RoutingPreset{
		DisplayName: "RT",
		ProcessRules: []*lavrayv1.ProcessRule{
			{ProcessNames: []string{"a.exe", "b.exe"}, Outbound: lavrayv1.Outbound_OUTBOUND_DIRECT, Platforms: []string{"windows"}},
			{ProcessNames: []string{"curl"}, Outbound: lavrayv1.Outbound_OUTBOUND_BLOCK, Platforms: []string{"linux", "macos", "openwrt"}},
		},
		Dns: &lavrayv1.DnsSettings{
			Mode: lavrayv1.DnsSettings_MODE_SIMPLE, RemoteDns: strPtr("https://dns.example/q"), DirectDns: strPtr("local"),
			RoutingEnabled: true, UnderlyingDns: strPtr("local"),
			RemoteQueryStrategy: func() *lavrayv1.DomainStrategy { v := lavrayv1.DomainStrategy_DOMAIN_STRATEGY_PREFER_IPV4; return &v }(),
		},
		CustomRulesJson: strPtr(`[{"port":[8080],"outbound":"direct"}]`),
		DefaultOutbound: lavrayv1.Outbound_OUTBOUND_PROXY,
		SniffingMode:    lavrayv1.SniffingMode_SNIFFING_FOR_ROUTING,
	}
	made, err := fromValues(t, cs, in)
	if err != nil {
		t.Fatal(err)
	}
	check := func(label string, got *lavrayv1.RoutingPreset) {
		t.Helper()
		if len(got.ProcessRules) != 2 || !proto.Equal(got.ProcessRules[0], in.ProcessRules[0]) || !proto.Equal(got.ProcessRules[1], in.ProcessRules[1]) {
			t.Errorf("%s: процессные правила: %v", label, got.ProcessRules)
		}
		if !proto.Equal(got.Dns, in.Dns) {
			t.Errorf("%s: DNS: %v ≠ %v", label, got.Dns, in.Dns)
		}
		if got.GetCustomRulesJson() != in.GetCustomRulesJson() || got.DefaultOutbound != in.DefaultOutbound || got.SniffingMode != in.SniffingMode {
			t.Errorf("%s: custom=%q def=%v sniff=%v", label, got.GetCustomRulesJson(), got.DefaultOutbound, got.SniffingMode)
		}
	}
	check("Create", made)
	check("Get", getPreset(t, cs, made.Id))
	edit := proto.Clone(made).(*lavrayv1.RoutingPreset)
	edit.BlockDomain = []string{"ads.example"}
	upd, err := updatePreset(cs, edit, made.Revision)
	if err != nil {
		t.Fatal(err)
	}
	check("Update", upd.Preset)
	check("Get после Update", getPreset(t, cs, made.Id))
	// custom_rules в виде {"rules":…} при записи приводится к массиву.
	edit = proto.Clone(upd.Preset).(*lavrayv1.RoutingPreset)
	edit.CustomRulesJson = strPtr(`{"rules":[{"port":[8080],"outbound":"direct"}]}`)
	upd, err = updatePreset(cs, edit, upd.NewRevision)
	if err != nil || upd.Preset.GetCustomRulesJson() != in.GetCustomRulesJson() {
		t.Fatalf("custom_rules не приведены к массиву: %v %v", upd, err)
	}
	// Правило процесса с путём отклоняется на записи.
	edit = proto.Clone(upd.Preset).(*lavrayv1.RoutingPreset)
	edit.ProcessRules[0].ProcessNames = []string{`C:\tools\a.exe`}
	_, err = updatePreset(cs, edit, upd.NewRevision)
	wantCode(t, err, connect.CodeInvalidArgument, "имя процесса с путём")
}

// builtConfigJSON — собранный конфиг профиля при активном наборе id.
func builtConfigJSON(t *testing.T, f *svcFixture) string {
	t.Helper()
	res, err := f.srv.dryRunBuild(f.trojan(t), lavrayv1.ConnectMode_CONNECT_MODE_SYSTEM_PROXY)
	if err != nil {
		t.Fatal(err)
	}
	b, err := json.Marshal(res.Config["route"])
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// TestPresetRulesReachBuiltConfig: правки пользовательского набора попадают в
// собранный конфиг — списки, процессные правила и custom-правила (оба вида записи).
func TestPresetRulesReachBuiltConfig(t *testing.T) {
	f := newSvcFixture(t, nil)
	s := f.srv
	made, err := s.CreatePreset(bg, connect.NewRequest(&lavrayv1.CreatePresetRequest{Preset: &lavrayv1.RoutingPreset{
		DisplayName:     "Cfg",
		DirectDomain:    []string{"full:direct-edit.example"},
		BlockDomain:     []string{"full:block-edit.example"},
		DefaultOutbound: lavrayv1.Outbound_OUTBOUND_PROXY,
		CustomRulesJson: strPtr(`[{"domain":["pin-array.example"],"outbound":"proxy"}]`),
		ProcessRules:    []*lavrayv1.ProcessRule{{ProcessNames: []string{"edit-proc.exe"}, Outbound: lavrayv1.Outbound_OUTBOUND_DIRECT}},
	}}))
	if err != nil {
		t.Fatal(err)
	}
	activate(t, s, made.Msg.Id)
	cfg := builtConfigJSON(t, f)
	for _, want := range []string{"direct-edit.example", "block-edit.example", "pin-array.example", "edit-proc.exe"} {
		if !strings.Contains(cfg, want) {
			t.Errorf("в route нет %q:\n%s", want, cfg)
		}
	}
	// Правка: новое direct-правило появляется, старое исчезает.
	edit := proto.Clone(made.Msg).(*lavrayv1.RoutingPreset)
	edit.DirectDomain = []string{"full:direct-two.example"}
	edit.CustomRulesJson = strPtr(`{"rules":[{"domain":["pin-object.example"],"outbound":"proxy"}]}`)
	if _, err := s.UpdatePreset(bg, connect.NewRequest(&lavrayv1.UpdatePresetRequest{Preset: edit, ExpectedRevision: made.Msg.Revision})); err != nil {
		t.Fatal(err)
	}
	cfg = builtConfigJSON(t, f)
	if !strings.Contains(cfg, "direct-two.example") || strings.Contains(cfg, "direct-edit.example") || !strings.Contains(cfg, "pin-object.example") || strings.Contains(cfg, "pin-array.example") {
		t.Errorf("правка не дошла до конфига:\n%s", cfg)
	}
}

// TestCheckRouteSeesEditedPreset: тестер маршрута отвечает по отредактированному набору.
func TestCheckRouteSeesEditedPreset(t *testing.T) {
	f := newSvcFixture(t, nil)
	s := f.srv
	made, err := s.CreatePreset(bg, connect.NewRequest(&lavrayv1.CreatePresetRequest{BuiltinSourceId: strPtr("all-proxy")}))
	if err != nil {
		t.Fatal(err)
	}
	activate(t, s, made.Msg.Id)
	route := func(addr string) lavrayv1.Outbound {
		t.Helper()
		out, err := checkRoute(t, s, &lavrayv1.CheckRouteRequest{Address: addr})
		if err != nil {
			t.Fatal(err)
		}
		return out
	}
	if got := route("edited.example"); got != lavrayv1.Outbound_OUTBOUND_PROXY {
		t.Fatalf("до правки: %v", got)
	}
	edit := proto.Clone(made.Msg).(*lavrayv1.RoutingPreset)
	edit.DirectDomain = append(edit.DirectDomain, "full:edited.example")
	edit.BlockDomain = append(edit.BlockDomain, "full:blocked.example")
	if _, err := s.UpdatePreset(bg, connect.NewRequest(&lavrayv1.UpdatePresetRequest{Preset: edit, ExpectedRevision: made.Msg.Revision})); err != nil {
		t.Fatal(err)
	}
	if got := route("edited.example"); got != lavrayv1.Outbound_OUTBOUND_DIRECT {
		t.Errorf("после правки edited.example = %v, ожидался DIRECT", got)
	}
	if got := route("blocked.example"); got != lavrayv1.Outbound_OUTBOUND_BLOCK {
		t.Errorf("после правки blocked.example = %v, ожидался BLOCK", got)
	}
	if got := route("other.example"); got != lavrayv1.Outbound_OUTBOUND_PROXY {
		t.Errorf("прочие адреса: %v", got)
	}
}
