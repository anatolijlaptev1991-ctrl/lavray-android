// SPDX-License-Identifier: GPL-3.0-only

package subs

// Валидатор URL подписок (LNB-05; SSRF-минимум для подписок — LNB-29/W3):
// только http/https; домен резолвится, адреса цели проверяются на
// приватность/loopback/link-local/ULA/multicast (кроме явного AllowPrivate —
// «разрешить локальные адреса»); редиректы — своим контролем, каждая цель
// через тот же валидатор (лимит 5). Тот же резолвер и та же проверка
// адресов применяются ПОВТОРНО при установке соединения (пиннинг IP в
// subscriber.dialPinned) — между Validate и dial DNS может «подменить»
// адрес (DNS rebinding TOCTOU, W3-2).

import (
	"fmt"
	"net"
	"net/url"
)

// MaxRedirects — предел цепочки редиректов (план §4.2: ≤5).
const MaxRedirects = 5

// URLGuard — валидатор URL (инъекция резолвера для тестов).
type URLGuard struct {
	// Resolver — системный резолвер по умолчанию; nil → net.DefaultResolver.
	Resolver interface {
		LookupIP(host string) ([]net.IP, error)
	}
	// AllowPrivate — явная галка «разрешить локальные адреса» (LNB-05).
	AllowPrivate bool
}

// Validate проверяет URL подписки ДО запроса.
func (g *URLGuard) Validate(rawURL string) error {
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
	if ip := net.ParseIP(host); ip != nil {
		if err := g.CheckIP(ip); err != nil {
			return fmt.Errorf("URL подписки указывает на локальный/приватный адрес %s (разрешается только с явной настройкой «локальные адреса»)", ip)
		}
		return nil
	}
	// Имя хоста: резолвим и проверяем все адреса (SSRF через DNS-имена).
	ips, err := g.lookupIP(host)
	if err != nil {
		return fmt.Errorf("хост подписки %s не резолвится: %v", host, err)
	}
	if len(ips) == 0 {
		return fmt.Errorf("хост подписки %s не имеет адресов", host)
	}
	for _, ip := range ips {
		if err := g.CheckIP(ip); err != nil {
			return fmt.Errorf("хост подписки %s резолвится в локальный/приватный адрес %s", host, ip)
		}
	}
	return nil
}

// CheckIP проверяет один адрес по политике приватности (AllowPrivate).
// Используется и валидатором ДО запроса, и пиннинг-диалером при dial
// (W3-2: второй резолв — между проверкой и соединением DNS мог подменить
// адрес на приватный).
func (g *URLGuard) CheckIP(ip net.IP) error {
	if !g.AllowPrivate && IsPrivateIP(ip) {
		return fmt.Errorf("адрес %s — локальный/приватный", ip)
	}
	return nil
}

func (g *URLGuard) lookupIP(host string) ([]net.IP, error) {
	if g.Resolver != nil {
		return g.Resolver.LookupIP(host)
	}
	return net.LookupIP(host)
}

// IsPrivateIP — приватные/служебные диапазоны (LNB-05):
// loopback, RFC1918, CGNAT 100.64/10, link-local, ULA fc00::/7,
// link-local IPv6 fe80::/10, multicast, unspecified, broadcast.
func IsPrivateIP(ip net.IP) bool {
	if ip == nil {
		return true
	}
	privateNets := []string{
		"127.0.0.0/8",        // loopback v4
		"10.0.0.0/8",         // RFC1918
		"172.16.0.0/12",      // RFC1918
		"192.168.0.0/16",     // RFC1918
		"169.254.0.0/16",     // link-local v4 (метаданные облаков)
		"100.64.0.0/10",      // CGNAT
		"0.0.0.0/8",          // unspecified/this-network
		"224.0.0.0/4",        // multicast v4
		"255.255.255.255/32", // broadcast
		"::1/128",            // loopback v6
		"fc00::/7",           // ULA
		"fe80::/10",          // link-local v6
		"ff00::/8",           // multicast v6
		"::/128",             // unspecified v6
		// W-06 (аудит round 2): ранее не покрытые диапазоны, через которые
		// SSRF достаёт служебные сервисы хостера/трансляторы.
		"240.0.0.0/4",     // reserved (class E)
		"198.18.0.0/15",   // benchmarking (часто занят провайдером)
		"192.0.0.0/24",    // IETF protocol assignments
		"192.88.99.0/24",  // 6to4 relay anycast
		"192.0.2.0/24",    // TEST-NET-1
		"198.51.100.0/24", // TEST-NET-2
		"203.0.113.0/24",  // TEST-NET-3
		"64:ff9b::/96",    // NAT64 (позволяет дотянуться до v4-loopback)
		"2002::/16",       // 6to4
		"fec0::/10",       // site-local (устаревший, но живой)
		// ::ffff:0:0/96 НЕ в списке: после To4() адрес нормализуется в
		// 4-байтный, и Contains по этой сети ложно матчил бы ЛЮБОЙ IPv4.
		// Mapped-адреса покрываются самой нормализацией ниже.
	}
	// IPv4-mapped IPv6 (::ffff:127.0.0.1) приводим к v4 — иначе Contains по
	// v4-сетям не совпадёт.
	if v4 := ip.To4(); v4 != nil {
		ip = v4
	}
	for _, cidr := range privateNets {
		if _, network, err := net.ParseCIDR(cidr); err == nil {
			if network.Contains(ip) {
				return true
			}
		}
	}
	return ip.IsLoopback() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() ||
		ip.IsPrivate() || ip.IsUnspecified() || ip.IsMulticast()
}
