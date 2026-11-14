// SPDX-License-Identifier: GPL-3.0-only

package api

import (
	"strings"

	"github.com/anatolijlaptev1991-ctrl/lavray-android/core/internal/subs"
)

// validateNewSubscriptionURL — проверка URL при СОЗДАНИИ подписки (T-51, C2 §11,
// LNB-05). Локальные и приватные адреса (127.0.0.1, 192.168.x.x, localhost)
// требуют явного разрешения allow_private_urls; в CreateSubscriptionRequest
// такого поля пока нет (его добавление — отдельная правка .proto), поэтому
// сейчас разрешения нет, и такая подписка отвергается сразу, а не создаётся
// группой, которую потом невозможно обновить. Сеть и DNS не используются:
// имя хоста, резолвящееся в приватный адрес, ловит проверка при обновлении.
func validateNewSubscriptionURL(raw string) error {
	return (&subs.URLGuard{}).ValidateStatic(strings.TrimSpace(raw))
}
