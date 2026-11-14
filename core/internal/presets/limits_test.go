// SPDX-License-Identifier: GPL-3.0-only

package presets

import (
	"fmt"
	"strings"
	"testing"
)

func userPreset(t *testing.T) *Preset {
	t.Helper()
	return NewCustomFrom(builtinByID(t, "global"), "custom:u")
}

func bigList(n int) []string {
	out := make([]string, n)
	for i := range out {
		out[i] = fmt.Sprintf("h%d.example", i)
	}
	return out
}

// TestPrepareUserNormalizes (H): custom_rules становится массивом, nil-списки —
// пустыми массивами (в JSON не «null»).
func TestPrepareUserNormalizes(t *testing.T) {
	p := userPreset(t)
	p.Routing.CustomRules = `{"rules":[{"domain":["a.example"],"outbound":"proxy"}]}`
	p.Routing.ProxyIP = nil
	p.Routing.ProcessRules = nil
	if err := p.PrepareUser(); err != nil {
		t.Fatal(err)
	}
	if p.Routing.CustomRules != `[{"domain":["a.example"],"outbound":"proxy"}]` {
		t.Errorf("custom_rules = %q", p.Routing.CustomRules)
	}
	if p.Routing.ProxyIP == nil || p.Routing.ProcessRules == nil {
		t.Error("nil-списки не приведены к пустым")
	}
}

// TestPrepareUserNameLimit (B/E): имя 1..64 символа (считаются руны, не байты).
func TestPrepareUserNameLimit(t *testing.T) {
	cases := []struct {
		name string
		ok   bool
	}{
		{"", false},
		{"   ", false},
		{"x", true},
		{strings.Repeat("я", 64), true},
		{strings.Repeat("я", 65), false},
	}
	for _, c := range cases {
		p := userPreset(t)
		p.Name = Localized{"en": c.name}
		err := p.PrepareUser()
		if (err == nil) != c.ok || (err != nil && !strings.Contains(err.Error(), "name")) {
			t.Errorf("name %d рун: err = %v, ok = %v", len([]rune(c.name)), err, c.ok)
		}
	}
}

// TestPrepareUserListAndRuleLimits (B): 10 000 записей в списке, 200 процессных
// правил, 64 имени в правиле — предел проходит, на единицу больше — нет.
func TestPrepareUserListAndRuleLimits(t *testing.T) {
	mk := func(names int) ProcessRule {
		n := make([]string, names)
		for i := range n {
			n[i] = fmt.Sprintf("p%d.exe", i)
		}
		return ProcessRule{Outbound: "direct", ProcessNames: n}
	}
	rules := func(n int) []ProcessRule {
		out := make([]ProcessRule, n)
		for i := range out {
			out[i] = mk(1)
		}
		return out
	}
	cases := []struct {
		label  string
		mutate func(*Preset)
		ok     bool
		frag   string
	}{
		{"список = 10000", func(p *Preset) { p.Routing.ProxyDomain = bigList(MaxListEntries) }, true, ""},
		{"список = 10001", func(p *Preset) { p.Routing.ProxyDomain = bigList(MaxListEntries + 1) }, false, "routing.proxy_domain"},
		{"ip-список = 10001", func(p *Preset) { p.Routing.BlockIP = make([]string, MaxListEntries+1) }, false, "routing.block_ip"},
		{"правил = 200", func(p *Preset) { p.Routing.ProcessRules = rules(MaxProcessRules) }, true, ""},
		{"правил = 201", func(p *Preset) { p.Routing.ProcessRules = rules(MaxProcessRules + 1) }, false, "routing.process_rules"},
		{"имён = 64", func(p *Preset) { p.Routing.ProcessRules = []ProcessRule{mk(MaxProcessNames)} }, true, ""},
		{"имён = 65", func(p *Preset) { p.Routing.ProcessRules = []ProcessRule{mk(MaxProcessNames + 1)} }, false, "process_names"},
		{"custom_rules не массив", func(p *Preset) { p.Routing.CustomRules = `[1]` }, false, "custom_rules"},
	}
	for _, c := range cases {
		p := userPreset(t)
		p.Routing.ProxyDomain = nil
		c.mutate(p)
		err := p.PrepareUser()
		if (err == nil) != c.ok {
			t.Errorf("%s: err = %v, ok = %v", c.label, err, c.ok)
		}
		if err != nil && !strings.Contains(err.Error(), c.frag) {
			t.Errorf("%s: err = %v, want содержит %q", c.label, err, c.frag)
		}
	}
}

// Версия схемы новее поддерживаемой отклоняется с путём поля.
func TestPrepareUserRejectsFutureVersion(t *testing.T) {
	p := userPreset(t)
	p.Version = SchemaVersion + 1
	if err := p.PrepareUser(); err == nil || !strings.Contains(err.Error(), "version") {
		t.Fatalf("err = %v", err)
	}
}
