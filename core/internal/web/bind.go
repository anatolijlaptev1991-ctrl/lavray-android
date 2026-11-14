// SPDX-License-Identifier: GPL-3.0-only

package web

import (
	"fmt"
	"net"
	"strconv"
)

// CheckBind — политика адреса веб-слушателя (--web-bind). Loopback допустим
// всегда. Любой другой адрес, включая 0.0.0.0, [::] и пустой хост, — только
// при allowRemote (--web-allow-remote): случайное открытие панели в сеть на
// старте исключено. Имена хостов, кроме localhost, не принимаются: адрес должен
// быть конкретным IP интерфейса.
func CheckBind(addr string, allowRemote bool) error {
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return fmt.Errorf("--web-bind %q: нужен адрес вида host:port: %v", addr, err)
	}
	if p, perr := strconv.Atoi(port); perr != nil || p < 0 || p > 65535 {
		return fmt.Errorf("--web-bind %q: порт должен быть числом 0–65535", addr)
	}
	if host == "localhost" {
		return nil
	}
	ip := net.ParseIP(host)
	if host != "" && ip == nil {
		return fmt.Errorf("--web-bind %q: %q не IP-адрес (имена хостов не принимаются)", addr, host)
	}
	if ip != nil && ip.IsLoopback() {
		return nil
	}
	if !allowRemote {
		shown := host
		if shown == "" {
			shown = "все интерфейсы"
		}
		return fmt.Errorf("--web-bind %q открывает панель наружу (%s): добавьте --web-allow-remote, если это осознанно (роутер)", addr, shown)
	}
	return nil
}

// IsLoopbackBind — адрес слушателя только на loopback (для предупреждения
// «Не защищено» достаточно знать, что панель видна только этому компьютеру).
func IsLoopbackBind(addr string) bool {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return false
	}
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}
