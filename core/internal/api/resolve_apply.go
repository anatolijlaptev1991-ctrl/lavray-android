// SPDX-License-Identifier: GPL-3.0-only

package api

// resolve_apply.go — как профиль меняется при ResolveDomains: адрес становится
// IP, а домен остаётся там, где он нужен соединению (SNI и host), если эти
// поля пусты.

import (
	"net/netip"
	"strings"

	"github.com/anatolijlaptev1991-ctrl/lavray-android/core/internal/model"
)

// resolvableHost — имя для разбора; false — профиль пропускается: цепочка,
// custom-профиль (адрес живёт в его конфиге), пустой адрес или уже IP.
func resolvableHost(p *model.Profile) (string, bool) {
	switch p.Type {
	case model.TypeChain, model.TypeCustomOutbound, model.TypeCustomConfig, model.TypeCustomExternal:
		return "", false
	}
	host := strings.Trim(strings.TrimSpace(p.Address), "[]")
	if host == "" {
		return "", false
	}
	if _, err := netip.ParseAddr(host); err == nil {
		return "", false
	}
	return strings.ToLower(host), true
}

// withResolvedAddress возвращает копию профиля с адресом addr; прежний домен
// переходит в SNI (если соединение с TLS) и в host (если транспорту он нужен),
// пока эти поля пусты. Исходный профиль не меняется.
func withResolvedAddress(p *model.Profile, addr netip.Addr) *model.Profile {
	domain := strings.TrimSpace(p.Address)
	out := *p
	out.Address = addr.String()
	if p.Stream != nil {
		stream := *p.Stream
		if usesTLS(&stream) && stream.SNI == "" {
			stream.SNI = domain
		}
		if sendsHost(&stream) && stream.Host == "" {
			stream.Host = domain
		}
		out.Stream = &stream
	}
	switch d := p.Detail.(type) {
	case *model.Hysteria2Detail:
		if d.SNI == "" && !d.DisableSNI {
			c := *d
			c.SNI = domain
			out.Detail = &c
		}
	case *model.TUICDetail:
		if d.SNI == "" && !d.DisableSNI {
			c := *d
			c.SNI = domain
			out.Detail = &c
		}
	case *model.NaiveDetail:
		if d.SNI == "" {
			c := *d
			c.SNI = domain
			out.Detail = &c
		}
	}
	return &out
}

// usesTLS: сборка конфига включает TLS для этого транспорта (tls или reality,
// в том числе по непустому ключу reality — как cfgbuild).
func usesTLS(s *model.StreamSettings) bool {
	return s.Security == "tls" || s.Security == "reality" || s.RealityPublicKey != ""
}

// sendsHost: транспорт передаёт серверу имя хоста (ws, http, httpupgrade и tcp
// с http-заголовком); у grpc и quic его нет.
func sendsHost(s *model.StreamSettings) bool {
	switch s.Network {
	case "ws", "http", "httpupgrade":
		return true
	case "", "tcp":
		return s.HeaderType == "http"
	}
	return false
}
