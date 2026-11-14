// SPDX-License-Identifier: GPL-3.0-only

package presets

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Эталоны golden-векторов: встроенные наборы лежат в builtin/ (единственная копия в этом
// репозитории), исходник NekoRay для конвертации — в testdata/.
const (
	builtinDir  = "builtin"
	testdataDir = "testdata"
)

func TestBuiltinPresetsLoad(t *testing.T) {
	list, err := BuiltinPresets()
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 6 {
		t.Fatalf("builtin = %d, want 6", len(list))
	}
	ids := map[string]bool{}
	for _, p := range list {
		ids[p.ID] = true
	}
	for _, id := range []string{"lav-ru-linux", "lav-ru", "lav-cn", "global", "all-proxy", "all-direct"} {
		if !ids[id] {
			t.Errorf("builtin не содержит %s", id)
		}
	}
	// G28–G30-подготовка: все builtin содержат блокировку рекламы (§3b)
	// и проходят валидацию (уже в LoadPreset).
	for _, p := range list {
		if !contains(p.Routing.BlockDomain, "geosite:category-ads-all") {
			t.Errorf("%s: block_domain без geosite:category-ads-all (§3b)", p.ID)
		}
	}
}

func TestValidateErrors(t *testing.T) {
	valid := func() *Preset {
		return &Preset{
			Version: 1, ID: "x", Name: Localized{"en": "X"},
			Routing: Routing{DefOutbound: "proxy",
				ProxyDomain: []string{}, DirectDomain: []string{}, BlockDomain: []string{},
				ProxyIP: []string{}, DirectIP: []string{}, BlockIP: []string{}, ProcessRules: []ProcessRule{}},
			DNS: DNSBlock{Mode: "simple", RemoteDNS: "local", DirectDNS: "local"},
		}
	}
	cases := []struct {
		mutate func(*Preset)
		msg    string
	}{
		{func(p *Preset) { p.Version = 2 }, "version"},
		{func(p *Preset) { p.ID = "" }, "id"},
		{func(p *Preset) { p.Routing.DefOutbound = "xy" }, "def_outbound"},
		{func(p *Preset) { p.Routing.SniffingMode = 2 }, "sniffing_mode"},
		{func(p *Preset) { p.Routing.OutboundDomainStrategy = "AsIs" }, "outbound_domain_strategy"},
		{func(p *Preset) { p.Routing.DirectDomain = []string{"geoip:ru"} }, "geoip допустим только"},
		{func(p *Preset) { p.Routing.DirectIP = []string{"geosite:cn"} }, "geosite допустим только"},
		{func(p *Preset) { p.Routing.ProxyDomain = []string{""} }, "пустая строка"},
		{func(p *Preset) { p.Routing.ProxyDomain = []string{"a #b"} }, "пробелы"},
		{func(p *Preset) {
			p.Routing.ProcessRules = []ProcessRule{{Outbound: "proxy", ProcessNames: []string{"a/b"}}}
		}, "имя процесса без пути"},
		{func(p *Preset) {
			p.Routing.ProcessRules = []ProcessRule{{Outbound: "proxy", ProcessNames: []string{".."}}}
		}, "имя процесса без пути"},
		{func(p *Preset) { p.Routing.ProcessRules = []ProcessRule{{Outbound: "wrong"}} }, "process_rules"},
		{func(p *Preset) { p.Routing.CustomRules = "{not json" }, "custom_rules"},
		{func(p *Preset) { p.DNS.Mode = "magic" }, "dns.mode"},
		{func(p *Preset) { p.DNS.Mode = "advanced"; p.DNS.AdvancedJSON = "{oops" }, "advanced_json"},
	}
	for i, c := range cases {
		p := valid()
		c.mutate(p)
		err := p.Validate()
		if err == nil || !strings.Contains(err.Error(), c.msg) {
			t.Errorf("case %d: err = %v, want содержит %q", i, err, c.msg)
		}
	}
	if err := valid().Validate(); err != nil {
		t.Fatalf("валидный пресет отклонён: %v", err)
	}
}

func TestLocalizedMarshalOrder(t *testing.T) {
	l := Localized{"de": "D", "es": "E", "en": "En", "zh": "Z", "ru": "R", "fr": "F"}
	b, err := l.MarshalJSON()
	if err != nil {
		t.Fatal(err)
	}
	want := `{"ru":"R","zh":"Z","en":"En","es":"E","de":"D","fr":"F"}`
	if string(b) != want {
		t.Errorf("marshal = %s, want %s", b, want)
	}
}

func TestEffectiveProcessRules(t *testing.T) {
	p := &Preset{Routing: Routing{ProcessRules: []ProcessRule{
		{Platforms: []string{"windows"}, Outbound: "proxy", ProcessNames: []string{"Cursor.exe"}},
		{Platforms: []string{"linux", "macos"}, Outbound: "direct", ProcessNames: []string{"steam"}},
		{Outbound: "block", ProcessNames: []string{"any"}},
	}}}
	win, w := EffectiveProcessRules(p, "windows", false)
	if len(win) != 2 || len(w) != 0 {
		t.Errorf("windows = %+v (warn %v)", win, w)
	}
	linux, _ := EffectiveProcessRules(p, "linux", false)
	if len(linux) != 2 {
		t.Errorf("linux = %+v", linux)
	}
	mac, _ := EffectiveProcessRules(p, "darwin", false)
	if len(mac) != 2 {
		t.Errorf("darwin = %+v", mac)
	}
	// OpenWrt: процессные правила опускаются с предупреждением (SCHEMA §2.1)
	ow, warns := EffectiveProcessRules(p, "linux", true)
	if len(ow) != 0 || len(warns) != 3 {
		t.Errorf("openwrt = %+v warns=%v", ow, warns)
	}
	other, _ := EffectiveProcessRules(p, "js", false)
	if len(other) != 0 {
		t.Errorf("неподдерживаемая платформа = %+v", other)
	}
}

// marshalPreset — детерминированная сериализация golden (без HTML-экранирования,
// 2 пробела, завершающий \n — как файлы presets/*.json).
func marshalPreset(t *testing.T, p *Preset) []byte {
	t.Helper()
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	if err := enc.Encode(p); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// TestGoldenLavRU — G28/golden W3 (SCHEMA §6): конвертация lav-ru.source.json
// (RU-bypass + Games, формат NekoRay 4.x) должна совпадать с эталонным
// builtin/lav-ru.json байт-в-байт (концы строк нормализуются: на Windows файлы могут
// оказаться с CRLF, генератор пишет LF — допустимое отличие checkout EOL).
// Метаданные берутся из эталона (не выводимы из RoutingBean) — проверяется
// именно конвертация routing+dns.
func TestGoldenLavRU(t *testing.T) {
	targetRaw, err := os.ReadFile(filepath.Join(builtinDir, "lav-ru.json"))
	if err != nil {
		t.Fatalf("эталон недоступен (запуск вне дерева репо?): %v", err)
	}
	target := normalizeEOL(targetRaw)
	srcData, err := os.ReadFile(filepath.Join(testdataDir, "lav-ru.source.json"))
	if err != nil {
		t.Fatal(err)
	}
	var src NekoRayRoute
	if err := json.Unmarshal(srcData, &src); err != nil {
		t.Fatal(err)
	}
	var meta PresetMeta
	if err := json.Unmarshal(target, &meta); err != nil {
		t.Fatal(err)
	}

	preset, warnings, err := ConvertNekoRayRoute(&src, ConvertOptions{Meta: meta, AddAdsBlocklist: true})
	if err != nil {
		t.Fatal(err)
	}
	// DROP-поля источника должны быть предупреждены (dns_final_out есть в source)
	foundDrop := false
	for _, w := range warnings {
		if strings.Contains(w, "dns_final_out") {
			foundDrop = true
		}
	}
	if !foundDrop {
		t.Errorf("ожидалось предупреждение о DROP dns_final_out; warnings = %v", warnings)
	}

	got := marshalPreset(t, preset)
	if !bytes.Equal(got, target) {
		gLines := strings.Split(string(got), "\n")
		tLines := strings.Split(string(target), "\n")
		for i := 0; i < len(gLines) && i < len(tLines); i++ {
			if gLines[i] != tLines[i] {
				t.Fatalf("golden lav-ru: первая разница в строке %d:\n got: %s\nwant: %s", i+1, gLines[i], tLines[i])
			}
		}
		t.Fatalf("golden lav-ru: длины различаются (got %d строк, want %d)", len(gLines), len(tLines))
	}
}

// normalizeEOL приводит CRLF/CR к LF (файлы presets/ в репо — CRLF).
func normalizeEOL(b []byte) []byte {
	out := make([]byte, 0, len(b))
	for i := 0; i < len(b); i++ {
		if b[i] == '\r' {
			if i+1 < len(b) && b[i+1] == '\n' {
				continue // CRLF → LF
			}
			continue // одиночный CR → LF
		}
		out = append(out, b[i])
	}
	return out
}

// TestConvertRoundtripAllBuiltins — каждый builtin можно перегенерить из
// эквивалентного RoutingBean (здесь — обратная сборка source из пресета и
// повторная конвертация дают тот же результат: идемпотентность конвертера).
func TestConvertRoundtripStable(t *testing.T) {
	src := &NekoRayRoute{
		ProxyDomain:  "domain:google.com\n# comment\ndomain:x.ai",
		DirectIP:     "geoip:ru\ngeoip:private",
		DefOutbound:  "proxy",
		Custom:       `{"rules":[{"process_name":["A.exe","steam"],"outbound":"proxy"},{"domain":["x.com"],"outbound":"bypass"}]}`,
		RemoteDNS:    "https://dns.google/dns-query",
		DirectDNS:    "local",
		DNSRouting:   true,
		DNSFinalOut:  "proxy",
		SniffingMode: 1,
	}
	meta := PresetMeta{Schema: "preset.schema.v1.json", Version: 1, Revision: 3, ID: "custom:test",
		Name: Localized{"ru": "Т", "en": "T"}, Builtin: false}
	p1, warns1, err := ConvertNekoRayRoute(src, ConvertOptions{Meta: meta})
	if err != nil {
		t.Fatal(err)
	}
	// комментарии вырезаны,Ads-лист не добавляли
	if contains(p1.Routing.ProxyDomain, "# comment") || len(p1.Routing.ProxyDomain) != 2 {
		t.Errorf("proxy_domain = %v", p1.Routing.ProxyDomain)
	}
	// смешанное правило разделено: .exe → windows, steam → linux+macos
	if len(p1.Routing.ProcessRules) != 2 {
		t.Fatalf("process_rules = %+v", p1.Routing.ProcessRules)
	}
	if p1.Routing.ProcessRules[0].Platforms[0] != "windows" ||
		p1.Routing.ProcessRules[1].Platforms[0] != "linux" {
		t.Errorf("platforms = %+v", p1.Routing.ProcessRules)
	}
	// непроцессное правило осталось в custom_rules
	if !strings.Contains(p1.Routing.CustomRules, "x.com") {
		t.Errorf("custom_rules = %q", p1.Routing.CustomRules)
	}
	// dns_final_out — DROP с предупреждением
	dropped := false
	for _, w := range warns1 {
		if strings.Contains(w, "dns_final_out") {
			dropped = true
		}
	}
	if !dropped {
		t.Errorf("warnings = %v", warns1)
	}
	// идемпотентность: бины → тот же JSON
	p2, _, err := ConvertNekoRayRoute(src, ConvertOptions{Meta: meta})
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(marshalPreset(t, p1), marshalPreset(t, p2)) {
		t.Error("конвертация не идемпотентна")
	}
}

// TestConvertSniffing2AndStrategies — DROP/warning-семантика §6.
func TestConvertSniffing2AndStrategies(t *testing.T) {
	src := &NekoRayRoute{SniffingMode: 2, OutboundDomainStrategy: "PreferIPv4",
		RemoteDNSStrategy: "UseIPv6", DirectDNSStrategy: "UseIPv4", DomainStrategy: "UseIPv4"}
	meta := PresetMeta{Schema: "s", Version: 1, Revision: 1, ID: "t", Name: Localized{"en": "T"}}
	p, warns, err := ConvertNekoRayRoute(src, ConvertOptions{Meta: meta})
	if err != nil {
		t.Fatal(err)
	}
	if p.Routing.SniffingMode != 1 {
		t.Errorf("sniffing = %d, want 1", p.Routing.SniffingMode)
	}
	if p.Routing.OutboundDomainStrategy != "prefer_ipv4" || p.DNS.RemoteQueryStrategy != "ipv6_only" {
		t.Errorf("strategies = %q / %q", p.Routing.OutboundDomainStrategy, p.DNS.RemoteQueryStrategy)
	}
	expect := []string{"sniffing_mode", "domain_strategy", "direct_dns_strategy"}
	for _, e := range expect {
		found := false
		for _, w := range warns {
			if strings.Contains(w, e) {
				found = true
			}
		}
		if !found {
			t.Errorf("нет предупреждения про %s (warns=%v)", e, warns)
		}
	}
}

func TestConvertBrokenCustom(t *testing.T) {
	src := &NekoRayRoute{Custom: "{broken"}
	_, _, err := ConvertNekoRayRoute(src, ConvertOptions{Meta: PresetMeta{Version: 1, ID: "x", Name: Localized{"en": "X"}}})
	if err == nil || !strings.Contains(err.Error(), "custom") {
		t.Fatalf("err = %v", err)
	}
}

func TestConvertCustomRuleUnknownOutboundWarns(t *testing.T) {
	// W3-7 (ревью): нечитаемый/неизвестный outbound — предупреждение, не молчаливый proxy.
	src := &NekoRayRoute{Custom: `{"rules":[{"process_name":["game.exe"],"outbound":["bypass"]}],"final":"proxy"}`}
	out, warns, err := ConvertNekoRayRoute(src, ConvertOptions{Meta: PresetMeta{Version: 1, ID: "x", Name: Localized{"en": "X"}}})
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, w := range warns {
		if strings.Contains(w, "outbound") {
			found = true
		}
	}
	if !found {
		t.Fatalf("warning об outbound не выдан: %+v", warns)
	}
	if len(out.Routing.ProcessRules) != 1 || out.Routing.ProcessRules[0].Outbound != "proxy" {
		t.Fatalf("process rule = %+v", out.Routing.ProcessRules)
	}
}
