// SPDX-License-Identifier: GPL-3.0-only

package gateway

import (
	"fmt"
	"strings"
)

// ErrBadMAC — MAC не годится для политики устройства.
var ErrBadMAC = fmt.Errorf("gateway: недопустимый MAC-адрес")

// NormalizeMAC приводит MAC к виду aa:bb:cc:dd:ee:ff. Принимает разделители
// «:» и «-», любой регистр; отвергает всё остальное, а также нулевой,
// широковещательный и групповой (младший бит первого байта) адреса: политика
// пишется для одного устройства. Строгий формат важен для безопасности: MAC
// попадает в текст nft-правил, который исполняется от root.
func NormalizeMAC(raw string) (string, error) {
	mac := strings.ToLower(strings.TrimSpace(raw))
	mac = strings.ReplaceAll(mac, "-", ":")
	if !validMAC(mac) {
		return "", fmt.Errorf("%w: %q, ждём aa:bb:cc:dd:ee:ff", ErrBadMAC, shorten(raw))
	}
	if mac == "00:00:00:00:00:00" || mac == "ff:ff:ff:ff:ff:ff" {
		return "", fmt.Errorf("%w: служебный адрес %s", ErrBadMAC, mac)
	}
	if hexVal(mac[1])&1 == 1 {
		return "", fmt.Errorf("%w: групповой адрес %s", ErrBadMAC, mac)
	}
	return mac, nil
}

// validMAC — строгий формат xx:xx:xx:xx:xx:xx в нижнем регистре.
func validMAC(mac string) bool {
	if len(mac) != 17 {
		return false
	}
	for i := 0; i < len(mac); i++ {
		c := mac[i]
		if (i+1)%3 == 0 {
			if c != ':' {
				return false
			}
			continue
		}
		if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f')) {
			return false
		}
	}
	return true
}

func hexVal(c byte) byte {
	if c >= 'a' {
		return c - 'a' + 10
	}
	return c - '0'
}

// shorten обрезает чужой ввод для сообщения об ошибке (без управляющих символов).
func shorten(s string) string {
	r := []rune(s)
	if len(r) > 24 {
		r = append(r[:24], '…')
	}
	return strings.Map(func(c rune) rune {
		if c < 0x20 || c == 0x7f {
			return '?'
		}
		return c
	}, string(r))
}
