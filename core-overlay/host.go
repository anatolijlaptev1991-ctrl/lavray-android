// SPDX-License-Identifier: GPL-3.0-only

package mobile

// host.go — общее состояние платформы на процесс и сборка контекста для
// одного экземпляра sing-box.
//
// Контекст нельзя готовить один раз в HTTP-обёртке: service.ContextWith кладёт
// в контекст изменяемый реестр служб, а временные инстансы проверки задержки
// (inst.NewTestInstance) наследуют контекст запроса и работают параллельно.
// Общий реестр заставил бы их затирать службы друг друга (Router, журнал,
// NetworkManager). Поэтому каждый экземпляр получает СВОЙ реестр через
// boxContext, а временные инстансы подключаются фабрикой (testfactory.go).

import (
	"context"
	"sync/atomic"

	"github.com/sagernet/sing-box/adapter"
	"github.com/sagernet/sing-box/option"
	"github.com/sagernet/sing/service"
)

// host — Platform от Kotlin, хаб монитора сети и реестр DNS-транспортов.
type host struct {
	plat Platform
	hub  *netHub
	dns  *dnsRegistryHolder
	// ownerLookup — включён ли поиск владельца соединения (Binder-вызов на
	// каждое соединение): нужен только правилам package_name и user*.
	ownerLookup atomic.Bool
}

func newHost(p Platform) *host {
	return &host{plat: p, hub: newNetHub(p), dns: newDNSRegistry()}
}

// boxContext возвращает контекст с новым реестром служб, платформой и
// реестром DNS-транспортов, где транспорт local заменён системным.
func (h *host) boxContext(parent context.Context) context.Context {
	ctx := service.ContextWithRegistry(parent, service.NewRegistry())
	ctx = service.ContextWith[adapter.PlatformInterface](ctx, newBoxPlatform(h))
	reg := h.dns.registry()
	ctx = service.ContextWith[option.DNSTransportOptionsRegistry](ctx, reg)
	ctx = service.ContextWith[adapter.DNSTransportRegistry](ctx, reg)
	return ctx
}
