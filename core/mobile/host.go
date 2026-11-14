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
	"strings"
	"sync"
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
	// packages — пакеты за uid (int32 → []string) в пределах запуска sing-box: без него
	// каждое соединение стоило бы второго Binder-вызова (getPackagesForUid).
	packages sync.Map
}

// packagesFor — пакеты uid: из запомненного или от Kotlin (один раз на uid за запуск).
func (h *host) packagesFor(uid int32) []string {
	if v, ok := h.packages.Load(uid); ok {
		return v.([]string)
	}
	var out []string
	for _, pkg := range strings.Split(h.plat.PackagesForUid(uid), ",") {
		if pkg = strings.TrimSpace(pkg); pkg != "" {
			out = append(out, pkg)
		}
	}
	// Пустой ответ для приложения не запоминается: getPackagesForUid мог не успеть увидеть новую
	// установку. У системных uid (root, сетевой стек) пакетов нет вовсе, и без запоминания каждое
	// их соединение стоило бы лишнего вызова.
	if len(out) > 0 || uid < firstApplicationUID {
		h.packages.Store(uid, out)
	}
	return out
}

// firstApplicationUID — android.os.Process.FIRST_APPLICATION_UID: ниже — системные uid.
const firstApplicationUID = 10000

// forgetPackages очищает packagesFor перед новым запуском.
func (h *host) forgetPackages() { h.packages.Clear() }

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
