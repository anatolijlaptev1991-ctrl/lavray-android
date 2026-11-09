// SPDX-License-Identifier: GPL-3.0-only

package mobile

// dns.go — DNS-транспорт «local» на Android. Штатный транспорт sing-box читает
// /etc/resolv.conf, которого на Android нет, и падает на 127.0.0.1:53; пресет
// LAV RU использует direct_dns=local, так что без замены ломается маршрут
// по умолчанию. libbox решает это обратным вызовом в Kotlin (LocalDNSTransport),
// здесь используется резолвер Go: на Android при cgo это getaddrinfo из bionic
// (net/conf.go, goosPrefersCgo), то есть системный резолвер через netd на
// сети по умолчанию. Приложение исключено из туннеля, поэтому запрос идёт мимо VPN.
// Поддерживаются запросы A и AAAA (как режим без raw в libbox); остальные
// типы получают ошибку, SVCB/HTTPS cfgbuild закрывает правилом predefined.
//
// Порт experimental/libbox/dns.go (sing-box, GPL-3.0-or-later).

import (
	"context"
	"errors"
	"net"
	"net/netip"
	"strings"
	"sync"

	"github.com/sagernet/sing-box/adapter"
	C "github.com/sagernet/sing-box/constant"
	"github.com/sagernet/sing-box/dns"
	"github.com/sagernet/sing-box/dns/transport/local"
	"github.com/sagernet/sing-box/include"
	"github.com/sagernet/sing-box/log"
	"github.com/sagernet/sing-box/option"
	"github.com/sagernet/sing/service"

	mDNS "github.com/miekg/dns"
)

// lookupFunc — разрешение имени в адреса заданного семейства ("ip4", "ip6").
type lookupFunc func(ctx context.Context, network, host string) ([]netip.Addr, error)

// systemLookup — net.DefaultResolver (cgo/getaddrinfo на Android).
func systemLookup(ctx context.Context, network, host string) ([]netip.Addr, error) {
	return net.DefaultResolver.LookupNetIP(ctx, network, host)
}

// dnsRegistryHolder лениво создаёт один реестр транспортов на процесс:
// реестр хранит только конструкторы, делить его между инстансами безопасно.
type dnsRegistryHolder struct {
	once sync.Once
	reg  *dns.TransportRegistry
}

func newDNSRegistry() *dnsRegistryHolder { return &dnsRegistryHolder{} }

func (h *dnsRegistryHolder) registry() *dns.TransportRegistry {
	h.once.Do(func() {
		h.reg = include.DNSTransportRegistry()
		registerSystemLocal(h.reg, systemLookup)
	})
	return h.reg
}

// registerSystemLocal подменяет транспорт local.
func registerSystemLocal(reg *dns.TransportRegistry, lookup lookupFunc) {
	dns.RegisterTransport[option.LocalDNSServerOptions](reg, C.DNSTypeLocal,
		func(ctx context.Context, l log.ContextLogger, tag string, opts option.LocalDNSServerOptions) (adapter.DNSTransport, error) {
			return newSystemTransport(ctx, l, tag, opts, lookup)
		})
}

type systemTransport struct {
	dns.TransportAdapter
	preferred *local.PreferredDomainResolver
	nm        adapter.NetworkManager
	lookup    lookupFunc
}

var (
	_ adapter.DNSTransport                    = (*systemTransport)(nil)
	_ adapter.DNSTransportWithPreferredDomain = (*systemTransport)(nil)
	_ adapter.DNSTransportWithEnvironment     = (*systemTransport)(nil)
)

func newSystemTransport(ctx context.Context, l log.ContextLogger, tag string, opts option.LocalDNSServerOptions, lookup lookupFunc) (*systemTransport, error) {
	preferred, err := local.NewPreferredDomainResolver(ctx, l, opts)
	if err != nil {
		return nil, err
	}
	return &systemTransport{
		TransportAdapter: dns.NewTransportAdapterWithLocalOptions(C.DNSTypeLocal, tag, opts),
		preferred:        preferred,
		nm:               service.FromContext[adapter.NetworkManager](ctx),
		lookup:           lookup,
	}, nil
}

func (t *systemTransport) Start(stage adapter.StartStage) error {
	t.preferred.Start(stage)
	return nil
}

func (t *systemTransport) Close() error { return nil }
func (t *systemTransport) Reset()       {}

func (t *systemTransport) PreferredDomain(domain string) bool {
	return t.preferred.PreferredDomain(domain)
}

// Environment — подпись окружения для сброса кэша DNS: серверы сети по умолчанию.
func (t *systemTransport) Environment() []string {
	if t.nm == nil {
		return nil
	}
	if def := t.nm.DefaultNetworkInterface(); def != nil {
		return def.DNSServers
	}
	return nil
}

func (t *systemTransport) Exchange(ctx context.Context, msg *mDNS.Msg) (*mDNS.Msg, error) {
	if resp := t.preferred.Lookup(msg); resp != nil {
		return resp, nil
	}
	return t.exchangeLookup(ctx, msg)
}

// exchangeLookup отвечает на вопрос через резолвер системы.
func (t *systemTransport) exchangeLookup(ctx context.Context, msg *mDNS.Msg) (*mDNS.Msg, error) {
	if len(msg.Question) == 0 {
		return nil, errors.New("dns: запрос без вопроса")
	}
	q := msg.Question[0]
	network, err := lookupNetwork(q.Qtype)
	if err != nil {
		return nil, err
	}
	addrs, err := t.lookup(ctx, network, strings.TrimSuffix(q.Name, "."))
	if err != nil {
		var dnsErr *net.DNSError
		if errors.As(err, &dnsErr) && dnsErr.IsNotFound {
			// getaddrinfo не различает NXDOMAIN и пустой ответ: отвечаем
			// NOERROR без адресов, чтобы не кэшировать «имени нет».
			return dns.FixedResponse(msg.Id, q, nil, C.DefaultDNSTTL), nil
		}
		return nil, err
	}
	return dns.FixedResponse(msg.Id, q, addrs, C.DefaultDNSTTL), nil
}

func (t *systemTransport) ExchangeAsync(ctx context.Context, msg *mDNS.Msg, callback func(*mDNS.Msg, error)) {
	go func() { callback(t.Exchange(ctx, msg)) }()
}

func lookupNetwork(qtype uint16) (string, error) {
	switch qtype {
	case mDNS.TypeA:
		return "ip4", nil
	case mDNS.TypeAAAA:
		return "ip6", nil
	}
	return "", errors.New("dns: системный резолвер Android отвечает только на запросы A и AAAA")
}
