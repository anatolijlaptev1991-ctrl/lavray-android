// SPDX-License-Identifier: GPL-3.0-only

package presets

import "testing"

// TestValidateOpenwrtPlatform (P): платформа openwrt допустима в процессных
// правилах; прочие неизвестные по-прежнему отклоняются.
func TestValidateOpenwrtPlatform(t *testing.T) {
	p := NewCustomFrom(builtinByID(t, "global"), "custom:r")
	p.Routing.ProcessRules = []ProcessRule{{Platforms: []string{"windows", "openwrt"}, Outbound: "direct", ProcessNames: []string{"a.exe"}}}
	if err := p.Validate(); err != nil {
		t.Fatalf("openwrt отклонена: %v", err)
	}
	p.Routing.ProcessRules[0].Platforms = []string{"android"}
	if err := p.Validate(); err == nil {
		t.Fatal("android принят")
	}
	// На обычной платформе правило только для роутера не действует.
	p.Routing.ProcessRules[0].Platforms = []string{"openwrt"}
	if got, _ := EffectiveProcessRules(p, "windows", false); len(got) != 0 {
		t.Fatalf("правило роутера действует на windows: %v", got)
	}
}
