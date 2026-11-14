// SPDX-License-Identifier: GPL-3.0-only

// inbounds.go — генерация inbounds (порт BuildConfigSingBox,
// ConfigBuilder.cpp:393–450): mixed-слушатель по настройкам inbound.*
// (auth опционален; FR-UI-279: не-loopback адрес требует пары user/pass;
// в режиме TUN loopback-слушатель остаётся, как в NekoBox), TUN-инбаунд по tun.*
// (FR-UI-184: interface_name/address/mtu/stack/strict_route/auto_route;
// kill-switch FR-UI-275 форсирует strict_route), custom_inbound-добавка.
//
// Отклонения от оракула (см. deviations.yaml):
//   - FR-UI-279 сохранён как ТРЕБОВАНИЕ БЕЗОПАСНОСТИ для LAN: адрес не
//     loopback требует пары user/pass. Ограничение «в TUN нет mixed»
//     снято: NekoBox держит оба слушателя одновременно (системный прокси
//     для браузеров + TUN для игр), и пользователь ожидает тот же паритет.
//     Риск «локальный процесс воспользуется прокси» остаётся тем же, что у
//     любого прокси-клиента: mixed слушает loopback, а не сеть;
//   - endpoint_independent_nat не эмитится — поле удалено в sing-box 1.14
//     (Deprecated: removed, option/tun.go);
//   - interface_name по умолчанию lavray-tun (darwin: utun9), не neko-tun;
//   - адрес TUN по умолчанию не пересекается с NekoBox (172.19.0.1/28 и
//     fdfe:dcba:9876::1/126): на одном ПК они работают рядом, и одинаковая
//     подсеть у двух интерфейсов ломала маршрутизацию обоих (0.2.1).
package cfgbuild

import (
	"encoding/json"
	"net"
	"runtime"
	"strings"
)

// Адреса TUN-интерфейса по умолчанию (FR-UI-184). Не совпадают с NekoBox, который
// на том же компьютере занимает 172.19.0.1/28 и fdfe:dcba:9876::1/126.
const (
	tunIPv4Address = "172.18.0.1/30"
	tunIPv6Address = "fd4c:4156:5259::1/126"
)

// defaultTunName — имя TUN-интерфейса по умолчанию (plan §3.1.3:
// lavray-tun — настраиваемое; macOS — utun9 как в оракуле).
func defaultTunName(goos string) string {
	if goos == "darwin" {
		return "utun9"
	}
	return "lavray-tun"
}

// buildInbounds — mixed-in + tun-in + custom_inbound (порядок оракула).
func buildInbounds(st *BuildSettings, forTest bool) ([]any, error) {
	var inbounds []any

	// mixed-in: включается независимо от TUN — как в NekoBox (два
	// независимых чекбокса «System Proxy» и «Tun Mode»). Это позволяет
	// одновременно гонять браузеры через системный прокси, а игры/консоль —
	// через TUN. Безопасность обеспечивается адресом (loopback по
	// умолчанию) и требованием авторизации для не-loopback (FR-UI-279).
	if !forTest && st.Inbound.Port > 0 && st.Inbound.Port <= 65535 {
		listen := strings.TrimSpace(st.Inbound.Address)
		if listen == "" {
			listen = "127.0.0.1"
		}
		// FR-UI-279: слушатель не на loopback — только с аутентификацией.
		if !isLoopbackAddr(listen) && (st.Inbound.AuthUser == "" || st.Inbound.AuthPass == "") {
			return nil, invalidArgument("inbound.address",
				"non-loopback inbound address %q requires inbound.auth.user and inbound.auth.pass (FR-UI-279)", listen)
		}
		in := map[string]any{
			"tag":         "mixed-in",
			"type":        "mixed",
			"listen":      listen,
			"listen_port": st.Inbound.Port,
		}
		if st.Inbound.AuthUser != "" && st.Inbound.AuthPass != "" {
			in["users"] = []any{map[string]any{
				"username": st.Inbound.AuthUser,
				"password": st.Inbound.AuthPass,
			}}
		}
		inbounds = append(inbounds, in)
	}

	// tproxy-in: шлюз OpenWrt (tproxy.go); на десктопе — INVALID_ARGUMENT.
	tp, err := tproxyInbound(st, forTest)
	if err != nil {
		return nil, err
	}
	if tp != nil {
		inbounds = append(inbounds, tp)
	}

	// tun-in (FR-UI-184; оракул ConfigBuilder.cpp:414–428).
	if st.Tun.Enabled && !forTest {
		name := strings.TrimSpace(st.Tun.InterfaceName)
		if name == "" {
			name = defaultTunName(goosOf(st))
		}
		mtu := st.Tun.MTU
		if mtu <= 0 {
			mtu = 9000
		}
		address := []any{tunIPv4Address}
		if st.Tun.IPv6 {
			address = append(address, tunIPv6Address)
		}
		in := map[string]any{
			"tag":            "tun-in",
			"type":           "tun",
			"interface_name": name,
			"auto_route":     true,
			"mtu":            mtu,
			"address":        address,
		}
		if stack := strings.TrimSpace(st.Tun.Stack); stack != "" {
			in["stack"] = stack
		}
		// Kill-switch (FR-UI-275, §10.6 плана): в конфигбилдере — форс
		// strict_route (трафик не может обойти упавший TUN); полная
		// блокировка при падении туннеля — координация с помощником/фаерволом
		// (shell-lifecycle §7, W8). Исключение приватных сетей сохраняется
		// (geoip:private в direct-списках пресетов).
		in["strict_route"] = st.Tun.StrictRoute || st.Tun.KillSwitch
		inbounds = append(inbounds, in)
	}

	// custom_inbound: {"inbounds":[...]} добавляется к сгенерированным.
	// L11 аудита №1: раньше объект добавлялся без единой проверки — можно
	// было открыть 0.0.0.0:1080 без авторизации (та же настройка через
	// inbound.address отвергается) или занять тег mixed-in и сломать старт.
	if !forTest {
		raw := strings.TrimSpace(st.Inbound.CustomInboundJSON)
		if raw != "" && raw != "{}" {
			var custom struct {
				Inbounds []map[string]any `json:"inbounds"`
			}
			if err := json.Unmarshal([]byte(raw), &custom); err != nil {
				return nil, invalidArgument("inbound.custom_inbound", "invalid JSON: %v", err)
			}
			usedTags := map[string]bool{}
			for _, in := range inbounds {
				if m, ok := in.(map[string]any); ok {
					if tag, _ := m["tag"].(string); tag != "" {
						usedTags[tag] = true
					}
				}
			}
			for _, in := range custom.Inbounds {
				tag, _ := in["tag"].(string)
				if tag != "" && usedTags[tag] {
					return nil, invalidArgument("inbound.custom_inbound",
						"тег %q уже занят сгенерированным inbound'ом", tag)
				}
				usedTags[tag] = true
				listen, _ := in["listen"].(string)
				if listen == "" {
					listen, _ = in["listen_address"].(string)
				}
				if listen != "" && !isLoopbackAddr(listen) {
					// FR-UI-279: наружу — только с авторизацией.
					if st.Inbound.AuthUser == "" || st.Inbound.AuthPass == "" {
						return nil, invalidArgument("inbound.custom_inbound",
							"inbound на %s требует inbound.auth.user/pass (FR-UI-279)", listen)
					}
				}
				inbounds = append(inbounds, in)
			}
		}
	}
	return inbounds, nil
}

// isLoopbackAddr — IP-литерал loopback (127.0.0.0/8, ::1). Имя хоста
// считается loopback только для "localhost".
func isLoopbackAddr(s string) bool {
	if strings.EqualFold(s, "localhost") {
		return true
	}
	ip := net.ParseIP(strings.TrimSpace(s))
	return ip != nil && ip.IsLoopback()
}

// goosOf — платформа сборки/выполнения для tun-name и процессных правил.
func goosOf(st *BuildSettings) string {
	if st.GOOS != "" {
		return st.GOOS
	}
	return runtime.GOOS
}
