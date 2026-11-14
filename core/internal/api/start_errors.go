// SPDX-License-Identifier: GPL-3.0-only

package api

// Понятные ошибки и предупреждения запуска (T-55, B1-36, B1-21).

import (
	"fmt"
	"regexp"
	"strconv"

	"github.com/anatolijlaptev1991-ctrl/lavray-android/core/internal/gen/lavrayv1"
	"github.com/anatolijlaptev1991-ctrl/lavray-android/core/internal/inst"
)

// explainStartError заменяет сырую ошибку sing-box о занятом порте («listen tcp
// 127.0.0.1:2080: bind: Only one usage of each socket address…», код INTERNAL)
// на FAILED_PRECONDITION с понятным текстом. nil — ошибка другая, оставить как есть.
// Порт по умолчанию 2080 совпадает с портом NekoBox — решение владельца
// (_master-findings §5 п. 7); при их одновременном запуске пользователь видит
// именно это сообщение.
func explainStartError(err error, inboundPort int) error {
	if !inst.IsAddrInUse(err) {
		return nil
	}
	if inboundPort > 0 && regexp.MustCompile(`:`+strconv.Itoa(inboundPort)+`(\D|$)`).MatchString(err.Error()) {
		return failedPrecondition("порт %d уже занят другой программой (например, NekoBox или второй копией LAVRay): "+
			"закройте её или смените порт в «Настройки → Сеть и прокси»", inboundPort)
	}
	return failedPrecondition("порт входящего подключения уже занят другой программой: " +
		"закройте её или смените порт в «Настройки → Сеть и прокси»")
}

// tunMixedWarning — предупреждение режима TUN, построенное по ФАКТИЧЕСКОМУ
// конфигу. Слушатель mixed в TUN остаётся (паритет с NekoBox, коммит e1baf39,
// cfgbuild/inbounds.go), поэтому прежнее «tun_mixed_disabled» было ложным.
// Нет mixed-in (inbound.port <= 0) — предупреждать не о чем.
func tunMixedWarning(cfg map[string]any, mode lavrayv1.ConnectMode) []string {
	if mode != lavrayv1.ConnectMode_CONNECT_MODE_TUN {
		return nil
	}
	inbounds, _ := cfg["inbounds"].([]any)
	for _, raw := range inbounds {
		in, _ := raw.(map[string]any)
		if in["tag"] != "mixed-in" {
			continue
		}
		return []string{fmt.Sprintf("tun_mixed_enabled: в режиме TUN слушатель mixed остаётся включённым на %v:%v "+
			"(системный прокси и приложения могут пользоваться им без TUN)", in["listen"], in["listen_port"])}
	}
	return nil
}
