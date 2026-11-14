// SPDX-License-Identifier: GPL-3.0-only

//go:build !singbox_run

package api

import (
	"errors"

	"github.com/anatolijlaptev1991-ctrl/lavray-android/core/internal/cfgbuild"
)

// ruleSetsSupported — без тега singbox_run читателя .srs нет: условия rule_set
// тестер не вычисляет (они попадают в Unresolved и в журнал). Рабочее ядро
// собирается с тегом singbox_run.
const ruleSetsSupported = false

func loadRuleSet([]byte) (cfgbuild.RuleSetMatcher, error) {
	return nil, errors.New("rule_set не читается в сборке без тега singbox_run")
}
