// SPDX-License-Identifier: GPL-3.0-only

package subs

import (
	"fmt"
	"net"
	"net/url"
	"strings"
)

// ValidateStatic — проверка URL без сети и DNS (создание подписки, T-51): схема
// http/https, непустой хост; localhost и литеральные IP не приватные (если не
// AllowPrivate). Имя, которое резолвится в приватный адрес, ловят Validate при
// обновлении и пиннинг IP при соединении.
func (g *URLGuard) ValidateStatic(rawURL string) error {
	u, err := url.Parse(rawURL)
	if err != nil {
		return fmt.Errorf("URL подписки некорректен: %v", err)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return fmt.Errorf("схема URL подписки должна быть http или https (получено %q)", u.Scheme)
	}
	host := u.Hostname()
	if host == "" {
		return fmt.Errorf("в URL подписки нет хоста")
	}
	if g.AllowPrivate {
		return nil
	}
	lower := strings.ToLower(host)
	if lower == "localhost" || strings.HasSuffix(lower, ".localhost") {
		return fmt.Errorf("URL подписки указывает на локальный адрес %s (разрешается только с явной настройкой «локальные адреса»)", host)
	}
	if ip := net.ParseIP(host); ip != nil {
		if err := g.CheckIP(ip); err != nil {
			return fmt.Errorf("URL подписки указывает на локальный/приватный адрес %s (разрешается только с явной настройкой «локальные адреса»)", ip)
		}
	}
	return nil
}
