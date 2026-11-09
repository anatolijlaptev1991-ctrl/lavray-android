// SPDX-License-Identifier: GPL-3.0-only

package mobile

// defaults.go — значения настроек по умолчанию, отличающиеся на Android.
//
// «Через туннель» ядро ходит через mixed-in работающего инстанса (адрес берётся из
// конфига запуска), а на телефоне mixed-in удалён (patch.go): другие приложения не
// должны видеть локальный прокси. Подписка при включённом sub.use_proxy и поднятом
// туннеле тогда падала бы с «proxyconnect tcp: connection refused». Собственное
// приложение исключено из туннеля, поэтому с sub.use_proxy=false запрос идёт напрямую.

import (
	"fmt"

	"github.com/anatolijlaptev1991-ctrl/lavray/core/internal/store"
)

// androidSettingDefaults — ключ → JSON-значение, записываемое, пока пользователь
// ключ не задавал.
var androidSettingDefaults = map[string]string{
	"sub.use_proxy": "false",
}

// applyAndroidDefaults записывает недостающие значения; заданные не трогает.
func applyAndroidDefaults(st *store.Store) error {
	for key, value := range androidSettingDefaults {
		_, set, err := st.GetSetting(key)
		if err != nil {
			return fmt.Errorf("настройка %s: %w", key, err)
		}
		if set {
			continue
		}
		if err := st.SetSetting(key, value); err != nil {
			return fmt.Errorf("настройка %s: %w", key, err)
		}
	}
	return nil
}
