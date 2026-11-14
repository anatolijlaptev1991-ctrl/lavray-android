// SPDX-License-Identifier: GPL-3.0-only

package cfgbuild

// tproxy.go — tproxy-вход шлюза OpenWrt (режим GATEWAY_TPROXY). Правила nft из
// PREROUTING направляют трафик LAN на этот порт (internal/gateway). Вход строится
// только при --platform openwrt: на десктопе режим шлюза — INVALID_ARGUMENT.

// TProxySettings — параметры tproxy-входа.
type TProxySettings struct {
	Enabled bool `json:"enabled"`
	Port    int  `json:"port,omitempty"`
	IPv6    bool `json:"ipv6,omitempty"` // слушать и IPv6 (явное включение в шлюзе)
}

// tproxyInbound возвращает вход tproxy-in либо nil, если режим не включён. Для
// временных инстансов (forTest) слушателей нет вовсе.
func tproxyInbound(st *BuildSettings, forTest bool) (map[string]any, error) {
	if !st.TProxy.Enabled || forTest {
		return nil, nil
	}
	if !st.OpenWRT {
		return nil, invalidArgument("gateway.tproxy",
			"tproxy-вход доступен только в сборке для роутера (--platform openwrt); на компьютере — системный прокси или TUN")
	}
	if st.TProxy.Port < 1 || st.TProxy.Port > 65535 {
		return nil, invalidArgument("gateway.tproxy", "порт tproxy %d вне диапазона 1–65535", st.TProxy.Port)
	}
	listen := "0.0.0.0" // IPv6 выключен по умолчанию (OPENWRT-GATEWAY §5)
	if st.TProxy.IPv6 {
		listen = "::"
	}
	return map[string]any{
		"tag":         "tproxy-in",
		"type":        "tproxy",
		"listen":      listen,
		"listen_port": st.TProxy.Port,
	}, nil
}
