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
	"encoding/json"
	"fmt"

	"github.com/anatolijlaptev1991-ctrl/lavray-android/core/internal/store"
)

// androidSettingDefaults — ключ → JSON-значение, записываемое, пока пользователь
// ключ не задавал.
// tun.impl=1 (gVisor) и tun.mtu=9000 выбраны замерами docs/MEASUREMENTS.md: из рабочих сочетаний
// это самое быстрое и дешёвое по процессору (MTU 1500 в 3–4 раза дороже на гигабайт). Стеки
// system и mixed на телефоне не работают, пока приложение исключено из VPN (addDisallowedApplication):
// ответы системного TCP-сокета ядра уходят мимо туннеля. Значения закреплены явно, чтобы смена
// умолчаний десктопа не меняла телефон.
var androidSettingDefaults = map[string]string{
	"sub.use_proxy": "false",
	"tun.impl":      "1",
	"tun.mtu":       "9000",
}

// Набор маршрутов по умолчанию на телефоне — LAV Preset RU Linux (internal/presets/builtin/lav-ru-linux.json): тот же
// LAV Preset RU без правил для программ Windows, которые на телефоне ничего не выбирают.
const (
	settingActivePreset  = "routing.active_preset"
	metaDNSOverride      = "routing.dns_override"
	androidDefaultPreset = "lav-ru-linux"
	desktopDefaultPreset = "lav-ru"
)

// applyAndroidDefaults записывает недостающие значения; заданные не трогает.
func applyAndroidDefaults(st *store.Store) error {
	if err := applyDefaultPreset(st); err != nil {
		return err
	}
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

// applyDefaultPreset ставит LAV Preset RU Linux, пока набор не выбран явно. Без строки
// настройки ядро берёт LAV Preset RU (значение по умолчанию рабочего стола), поэтому сюда
// попадают и новые установки, и прежние, где набор не меняли. Маршруты на телефоне от
// этого не меняются: правила для программ Windows patch.go и так снимал. Выбор DNS,
// сделанный для LAV Preset RU, переходит к новому набору, чтобы не пропасть молча.
// Явно выбранный набор, LAV Preset RU тоже, не трогается.
func applyDefaultPreset(st *store.Store) error {
	_, set, err := st.GetSetting(settingActivePreset)
	if err != nil {
		return fmt.Errorf("настройка %s: %w", settingActivePreset, err)
	}
	if set {
		return nil
	}
	if err := carryDNSChoice(st, desktopDefaultPreset, androidDefaultPreset); err != nil {
		return err
	}
	if err := st.SetSetting(settingActivePreset, strconvQuote(androidDefaultPreset)); err != nil {
		return fmt.Errorf("настройка %s: %w", settingActivePreset, err)
	}
	return nil
}

// carryDNSChoice копирует выбор DNS набора from в набор to, если у to своего ещё нет.
// Нечитаемая запись не трогается: её разбор — дело ядра.
func carryDNSChoice(st *store.Store, from, to string) error {
	raw, ok, err := st.GetMeta(metaDNSOverride)
	if err != nil {
		return fmt.Errorf("meta %s: %w", metaDNSOverride, err)
	}
	if !ok {
		return nil
	}
	var choices map[string]json.RawMessage
	if json.Unmarshal([]byte(raw), &choices) != nil {
		return nil
	}
	choice, has := choices[from]
	if _, taken := choices[to]; !has || taken {
		return nil
	}
	choices[to] = choice
	out, err := json.Marshal(choices)
	if err != nil {
		return fmt.Errorf("meta %s: %w", metaDNSOverride, err)
	}
	if err := st.SetMeta(metaDNSOverride, string(out)); err != nil {
		return fmt.Errorf("meta %s: %w", metaDNSOverride, err)
	}
	return nil
}

func strconvQuote(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}
