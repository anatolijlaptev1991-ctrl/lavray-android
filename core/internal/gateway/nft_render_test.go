// SPDX-License-Identifier: GPL-3.0-only

package gateway

// nft_render_test.go — рендер nft: golden-файлы (их же проверяет `nft -c -f` в
// job CI nft-check), инварианты и устойчивость к инъекциям. Обновить golden:
// LAVRAY_UPDATE_GOLDEN=1 go test ./internal/gateway -run Golden.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func baseCfg() Config { return Config{TProxyPort: 7893, Enabled: true} }

func fullCfg() Config {
	return Config{TProxyPort: 7893, Enabled: true, IPv6: true, DNSHijack: true, FwMark: 5,
		LANIfaces: []string{"br-lan", "br-iot"}, SelfUser: "lavray"}
}

func devicePolicies() []DevicePolicy {
	return []DevicePolicy{
		{MAC: "AA:BB:CC:11:22:34", Policy: PolicyDirect},
		{MAC: "aa:bb:cc:44:55:66", Policy: PolicyProxy},
		{MAC: "aa:bb:cc:77:88:9a", Policy: PolicyPreset, PresetID: "ru-direct"},
	}
}

var goldenCases = []struct {
	file     string
	cfg      Config
	policies []DevicePolicy
}{
	{"default.nft", baseCfg(), nil},
	{"policies.nft", baseCfg(), devicePolicies()},
	{"full.nft", fullCfg(), devicePolicies()},
}

func TestRenderGolden(t *testing.T) {
	for _, c := range goldenCases {
		got := NftRenderer{}.Render(c.cfg, c.policies, "")
		path := filepath.Join("testdata", c.file)
		if os.Getenv("LAVRAY_UPDATE_GOLDEN") == "1" {
			if err := os.MkdirAll("testdata", 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(path, []byte(got), 0o644); err != nil {
				t.Fatal(err)
			}
			continue
		}
		want, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("%s: %v (создайте: LAVRAY_UPDATE_GOLDEN=1)", c.file, err)
		}
		if string(want) != got {
			t.Errorf("%s расходится с рендером:\n--- ждали\n%s\n--- получили\n%s", c.file, want, got)
		}
	}
}

func TestRenderInvariants(t *testing.T) {
	out := NftRenderer{}.Render(fullCfg(), devicePolicies(), "")
	checks := []struct{ what, sub string }{
		{"таблица", "table inet lavray {"},
		{"идемпотентный заголовок", "delete table inet lavray"},
		{"только LAN", `iifname != { "br-lan", "br-iot" } return`},
		{"tproxy tcp", "meta l4proto tcp tproxy ip to :7893 meta mark set 5 accept"},
		{"tproxy udp v6", "meta l4proto udp tproxy ip6 to :7893 meta mark set 5 accept"},
		{"роутер не заворачиваем", "fib daddr type local return"},
		{"приватные исключены", "192.168.0.0/16"},
		{"интервальный набор", "flags interval"},
		{"DHCP не заворачиваем", "udp dport { 67, 68 } return"},
		{"DNS hijack до исключений", "udp dport 53 meta nfproto ipv4 jump proxy_v4"},
		{"MAC direct", "ether saddr aa:bb:cc:11:22:34 return"},
		{"MAC proxy", "ether saddr aa:bb:cc:44:55:66 meta nfproto ipv4 jump proxy_v4"},
		{"MAC preset", `# aa:bb:cc:77:88:9a: пресет "ru-direct"`},
		{"skuid", `meta skuid "lavray" return`},
	}
	for _, c := range checks {
		if !strings.Contains(out, c.sub) {
			t.Errorf("%s: нет %q", c.what, c.sub)
		}
	}
	// DNS-перехват стоит раньше исключений, правила MAC — после них.
	dns, excl, dev := strings.Index(out, "udp dport 53"), strings.Index(out, "fib daddr type local"), strings.Index(out, "ether saddr")
	if !(dns >= 0 && dns < excl && excl < dev) {
		t.Errorf("порядок правил: dns=%d исключения=%d устройства=%d", dns, excl, dev)
	}
	// Детерминизм и сортировка MAC независимо от порядка входа.
	rev := devicePolicies()
	rev[0], rev[2] = rev[2], rev[0]
	if out2 := (NftRenderer{}).Render(fullCfg(), rev, ""); out != out2 {
		t.Fatal("рендер зависит от порядка политик")
	}
	i1, i2 := strings.Index(out, "aa:bb:cc:11"), strings.Index(out, "aa:bb:cc:44")
	if i1 < 0 || i2 < 0 || i1 > i2 {
		t.Fatal("MAC-правила не отсортированы")
	}
}

func TestRenderIPv6OffByDefault(t *testing.T) {
	out := NftRenderer{}.Render(baseCfg(), devicePolicies(), "")
	for _, bad := range []string{"ip6", "exclude_addr6", "ipv6", "546"} {
		if strings.Contains(out, bad) {
			t.Errorf("IPv6 выключен по умолчанию (§5), но в правилах есть %q", bad)
		}
	}
	if !strings.Contains(out, "tproxy ip to :7893 meta mark set 1") {
		t.Error("метка по умолчанию — 1")
	}
}

// TestRenderResistsInjection — текст из конфигурации не выходит за пределы
// своей строки правила (S).
func TestRenderResistsInjection(t *testing.T) {
	cfg := baseCfg()
	cfg.LANIfaces = []string{"br-lan\" return\nadd rule inet lavray prerouting accept\n#"}
	cfg.SelfUser = "lavray\"\nflush ruleset\n"
	pol := []DevicePolicy{
		{MAC: "aa:bb:cc:dd:ee:00\nflush ruleset", Policy: PolicyDirect},
		{MAC: "aa:bb:cc:dd:ee:02", Policy: PolicyPreset, PresetID: "x\nflush ruleset"},
	}
	out := NftRenderer{}.Render(cfg, pol, "")
	if strings.Contains(out, "flush ruleset") || strings.Contains(out, "add rule") {
		t.Fatalf("инъекция прошла в правила:\n%s", out)
	}
	if strings.Contains(out, "ee:00") {
		t.Fatal("недопустимый MAC попал в правила")
	}
	for _, line := range strings.Split(out, "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "flush") {
			t.Fatalf("лишняя команда: %q", line)
		}
	}
}

func TestRenderNumericUser(t *testing.T) {
	out := NftRenderer{}.Render(baseCfg(), nil, "1001")
	if !strings.Contains(out, "meta skuid 1001 return") {
		t.Fatalf("числовой uid без кавычек ожидался:\n%s", out)
	}
}
