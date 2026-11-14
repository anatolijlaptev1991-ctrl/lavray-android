// SPDX-License-Identifier: GPL-3.0-only

package cfgbuild

// customrules.go — разбор custom-правил маршрута: набора (строгий — ошибка
// останавливает сборку) и устаревшей глобальной настройки routing.custom_global
// (мягкий — негодное значение игнорируется с замечанием, старые установки
// продолжают запускаться).

import (
	"fmt"

	"github.com/anatolijlaptev1991-ctrl/lavray-android/core/internal/presets"
)

// warn добавляет замечание в результат сборки.
func (r *BuildResult) warn(msg string) {
	if r != nil {
		r.Warnings = append(r.Warnings, msg)
	}
}

// parseCustomRules — custom-правила (пресета или routing.custom_global) → список
// rule-объектов. Принимаются массив и {"rules":[...]}; разбор общий с хранением
// набора (presets.ParseCustomRules). Пустая строка/"{}" — пусто.
func parseCustomRules(raw, key string) ([]any, error) {
	rules, err := presets.ParseCustomRules(raw)
	if err != nil {
		return nil, invalidArgument(key, "invalid JSON (%s expected array or {\"rules\":[...]}): %v", key, err)
	}
	return rules, nil
}

// globalCustomRules — правила устаревшей настройки routing.custom_global. Раньше
// она принимала любой JSON с полем rules; теперь значение проверяется как у
// набора (объекты, до 256 КиБ). Негодное значение не должно ломать запуск
// установки, где оно сохранилось: правила пропускаются, причина уходит в warn.
func globalCustomRules(st *BuildSettings, warn func(string)) []any {
	rules, err := parseCustomRules(st.CustomRouteGlobalJSON, "routing.custom_global")
	if err != nil {
		if warn != nil {
			warn(fmt.Sprintf("настройка routing.custom_global проигнорирована: %v", err))
		}
		return nil
	}
	return rules
}
