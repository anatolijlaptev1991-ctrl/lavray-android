// SPDX-License-Identifier: GPL-3.0-only

package presets

// process_rules.go — процессные правила набора, применимые на платформе сборки.

import "fmt"

// EffectiveProcessRules — правила, применимые на платформе goos
// ("windows"/"linux"/"macos"; пустые platforms = все). На OpenWrt-сборке
// процессные правила опускаются с предупреждением (SCHEMA §2.1: «Не действует
// на роутере» — локализация в UI; ядро в конфиг их не вставляет).
func EffectiveProcessRules(p *Preset, goos string, openwrt bool) ([]ProcessRule, []string) {
	var out []ProcessRule
	var warnings []string
	if openwrt {
		for _, r := range p.Routing.ProcessRules {
			warnings = append(warnings,
				fmt.Sprintf("процессное правило (%s) не действует на роутере — опущено", firstOr(r.ProcessNames, r.Outbound)))
		}
		return nil, warnings
	}
	plat := map[string]string{"windows": "windows", "darwin": "macos", "linux": "linux"}[goos]
	if plat == "" {
		if len(p.Routing.ProcessRules) > 0 {
			warnings = append(warnings, fmt.Sprintf("платформа %s не поддерживает процессные правила — опущены", goos))
		}
		return nil, warnings
	}
	for _, r := range p.Routing.ProcessRules {
		if len(r.Platforms) == 0 {
			out = append(out, r)
			continue
		}
		for _, p2 := range r.Platforms {
			if p2 == plat {
				out = append(out, r)
				break
			}
		}
	}
	return out, warnings
}

func firstOr(list []string, def string) string {
	if len(list) > 0 {
		return list[0]
	}
	return def
}
