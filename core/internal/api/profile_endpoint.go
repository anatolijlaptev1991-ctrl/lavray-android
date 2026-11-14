// SPDX-License-Identifier: GPL-3.0-only

package api

// profile_endpoint.go — порт сервера проверяется при сохранении профиля, а не
// только при подключении: прогон 0.9.0 (D-5) создал CreateProfile'ом профиль с
// port 0, который заведомо не подключится.

import "github.com/anatolijlaptev1991-ctrl/lavray-android/core/internal/model"

// endpointless — типы без своего адреса сервера: он в JSON или в звеньях цепочки.
var endpointless = map[model.ProfileType]bool{
	model.TypeCustomOutbound: true,
	model.TypeCustomConfig:   true,
	model.TypeCustomExternal: true,
	model.TypeChain:          true,
}

// checkEndpoint: порт сервера 1–65535. Неизменённый порт (тот же тип и порт,
// что в old) не проверяется: профиль, сохранённый до этого правила, можно
// переименовать, не исправляя его.
func checkEndpoint(m, old *model.Profile) error {
	if endpointless[m.Type] {
		return nil
	}
	if old != nil && old.Type == m.Type && old.Port == m.Port {
		return nil
	}
	if m.Port < 1 || m.Port > 65535 {
		return invalidArg("port: %d вне диапазона 1–65535", m.Port)
	}
	return nil
}
