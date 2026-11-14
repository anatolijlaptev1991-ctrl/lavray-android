// SPDX-License-Identifier: GPL-3.0-only

// route_check.go — чтение гео-наборов для тестера маршрута (T-30).
package api

import (
	"errors"
	"fmt"
	"io/fs"
	"os"

	"github.com/anatolijlaptev1991-ctrl/lavray-android/core/internal/cfgbuild"
	"github.com/anatolijlaptev1991-ctrl/lavray-android/core/internal/presets"
)

// ruleSetLookup читает набор по тегу rule_set: файл из каталога данных (его
// могло обновить гео-обновление), а если файла нет — встроенную копию, как
// это делает EnsureGeo при первом запуске.
func (s *Server) ruleSetLookup(table *cfgbuild.RouteTable) cfgbuild.RuleSetLookup {
	return func(tag string) (cfgbuild.RuleSetMatcher, error) {
		ref, ok := table.RuleSets[tag]
		if !ok {
			return nil, fmt.Errorf("rule_set %q не объявлен в правилах", tag)
		}
		raw, err := os.ReadFile(ref.Path)
		if errors.Is(err, fs.ErrNotExist) {
			raw, err = presets.GeoFile(ref.Rel)
		}
		if err != nil {
			return nil, err
		}
		return loadRuleSet(raw)
	}
}
