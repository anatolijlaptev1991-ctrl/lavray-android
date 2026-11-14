// SPDX-License-Identifier: GPL-3.0-only

package api

// settings_event.go — событие SettingsChanged после замены настроек целиком
// (восстановление копии, T-83), SetSettings и ResetSettings. Рассылка —
// settings_broadcast.go, поток — settings_subscribe.go.

import (
	"sort"

	lavrayv1 "github.com/anatolijlaptev1991-ctrl/lavray-android/core/internal/gen/lavrayv1"
)

// settingsDiff сравнивает настройки до и после: changed — новые и изменённые
// значения, removed — ключи, которых больше нет (сброшены к умолчанию).
// Состояние обновлений и служебные ключи ядра в событие не входят. Второй
// результат — менялся ли ключ, вступающий в силу только после перезапуска.
func settingsDiff(before, after map[string]string) (*lavrayv1.SettingsChanged, bool) {
	ev := &lavrayv1.SettingsChanged{Changed: map[string]*lavrayv1.SettingValue{}}
	skip := func(key string) bool { return isUpdateKey(key) || descriptorMap[key].Internal }
	needsRestart := false
	for key, raw := range after {
		if prev, existed := before[key]; skip(key) || existed && prev == raw {
			continue
		}
		ev.Changed[key] = typedSettingValue(key, raw)
		needsRestart = needsRestart || descriptorMap[key].RequiresAppRestart
	}
	for key := range before {
		if _, kept := after[key]; kept || skip(key) {
			continue
		}
		ev.Removed = append(ev.Removed, key)
		needsRestart = needsRestart || descriptorMap[key].RequiresAppRestart
	}
	sort.Strings(ev.Removed)
	return ev, needsRestart
}
