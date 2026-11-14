// SPDX-License-Identifier: GPL-3.0-only

package cfgbuild

// validate_ent.go — проверка звена цепочки перед сборкой outbound'а.

import (
	"strings"

	"github.com/anatolijlaptev1991-ctrl/lavray-android/core/internal/model"
)

// validateEnt проверяет профиль-звено: транспорт поддерживается ядром, адрес и
// порт заданы, reality не без ключа.
func validateEnt(p *model.Profile) error {
	if err := checkTransportSupported(p); err != nil {
		return err
	}
	// Адрес/порт custom internal живёт в JSON outbound'а — поля профиля
	// валидации не подлежат.
	if p.Type == model.TypeCustomOutbound {
		return nil
	}
	if strings.TrimSpace(p.Address) == "" {
		return invalidArgument("address", "profile %d: server address is empty", p.ID)
	}
	if p.Port <= 0 || p.Port > 65535 {
		return invalidArgument("port", "profile %d: invalid port %d", p.ID, p.Port)
	}
	// L10 аудита №1: reality без ключа — не «tls по умолчанию», а ошибка:
	// иначе получился бы открытый текст с утечкой UUID.
	if p.Stream != nil && p.Stream.Security == "reality" &&
		strings.TrimSpace(p.Stream.RealityPublicKey) == "" {
		return invalidArgument("stream.reality_public_key",
			"profile %d: security=reality требует public_key", p.ID)
	}
	return nil
}
