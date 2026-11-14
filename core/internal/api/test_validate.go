// SPDX-License-Identifier: GPL-3.0-only

package api

// test_validate.go — проверка адресов URL- и полного теста (T-84): они приходят
// и в запросе TestLatency, и из настроек test.url / test.url_dl / test.url_ip /
// test.udp_target. Правила владельца: URL — только http/https с непустым
// хостом, UDP-цель — host:port. Приватные и loopback-адреса разрешены: запрос
// идёт через исходящее профиля, а не с этого компьютера.

import (
	"errors"
	"net"
	"net/url"
	"strconv"
	"strings"
)

// testURLSettings — настройки-адреса, проверяемые validateTestURL.
var testURLSettings = map[string]bool{
	"test.url":    true,
	"test.url_dl": true,
	"test.url_ip": true,
}

// validateTestURL: http/https с непустым хостом.
func validateTestURL(raw string) error {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return errors.New("адрес пуст")
	}
	u, err := url.Parse(raw)
	if err != nil {
		return errors.New("адрес не разобрать как URL")
	}
	switch strings.ToLower(u.Scheme) {
	case "http", "https":
	default:
		return errors.New("разрешены только схемы http и https")
	}
	if u.Hostname() == "" {
		return errors.New("в адресе нет хоста")
	}
	return nil
}

// validateUDPTarget: host:port, порт 1…65535; host — IP или имя без пробелов.
func validateUDPTarget(raw string) error {
	raw = strings.TrimSpace(raw)
	host, port, err := net.SplitHostPort(raw)
	if err != nil {
		return errors.New("ожидается host:port")
	}
	if host == "" || strings.ContainsAny(host, " \t/") {
		return errors.New("в адресе нет корректного хоста")
	}
	if n, err := strconv.Atoi(port); err != nil || n < 1 || n > 65535 {
		return errors.New("порт должен быть числом 1–65535")
	}
	return nil
}
