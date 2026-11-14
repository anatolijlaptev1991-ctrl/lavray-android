// SPDX-License-Identifier: GPL-3.0-only

// hy2ports.go — перевод строки server_ports профиля в формат sing-box 1.14.
//
// В форке sing-box поле hysteria2 server_ports — список строк-диапазонов
// «a:b» (option.Hysteria2OutboundOptions.ServerPorts, badoption.Listable[string];
// разбор — sing-quic hysteria.ParsePorts: каждая запись обязана содержать «:»).
// Профиль хранит свободную строку (ссылка mport, импорт, Clash): «8443»,
// «1000:2000», «1000-2000», «1000:2000,3000:4000». Эмитить такую строку как есть
// нельзя — sing-box отвергает «8443» и список через запятую, не упав внятно.
package cfgbuild

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
)

// hysteria2ServerPorts приводит строку портов профиля к списку диапазонов
// «a:b»: одиночный порт p → «p:p», «a-b» и «a:b» → «a:b», запятая разделяет
// элементы. Пустая строка → nil без ошибки. Мусор (нечисло, порт вне 1..65535,
// a > b, пустой элемент) → *Error INVALID_ARGUMENT, а не падение при запуске.
func hysteria2ServerPorts(s string) ([]string, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil, nil
	}
	parts := strings.Split(s, ",")
	out := make([]string, 0, len(parts))
	for _, raw := range parts {
		item := strings.TrimSpace(raw)
		if item == "" {
			return nil, invalidArgument("detail.server_ports",
				"hysteria2: пустой элемент в списке портов %q", s)
		}
		lo, hi, err := parsePortRange(item)
		if err != nil {
			return nil, invalidArgument("detail.server_ports",
				"hysteria2: неверный диапазон портов %q (ожидается порт, a:b или a-b, 1–65535): %v", item, err)
		}
		out = append(out, strconv.Itoa(lo)+":"+strconv.Itoa(hi))
	}
	return out, nil
}

// parsePortRange разбирает «p», «a:b» или «a-b» в пару границ [lo, hi].
func parsePortRange(item string) (lo, hi int, err error) {
	sep := strings.IndexAny(item, ":-")
	if sep < 0 {
		lo, err = parsePortNumber(item)
		return lo, lo, err
	}
	if lo, err = parsePortNumber(item[:sep]); err != nil {
		return 0, 0, err
	}
	if hi, err = parsePortNumber(item[sep+1:]); err != nil {
		return 0, 0, err
	}
	if lo > hi {
		return 0, 0, errors.New("начало диапазона больше конца")
	}
	return lo, hi, nil
}

// parsePortNumber разбирает десятичный порт 1..65535 (без знака и пробелов).
func parsePortNumber(s string) (int, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, errors.New("пустая граница")
	}
	for i := 0; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return 0, fmt.Errorf("недопустимый символ в %q", s)
		}
	}
	n, err := strconv.Atoi(s)
	if err != nil || n < 1 || n > 65535 {
		return 0, fmt.Errorf("порт %q вне диапазона 1–65535", s)
	}
	return n, nil
}
