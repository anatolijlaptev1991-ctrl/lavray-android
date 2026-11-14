// SPDX-License-Identifier: GPL-3.0-only

package api

// resolver.go — разбор имён серверов для ResolveDomains (хвосты A): трейт
// Resolver, системная реализация и параллельный разбор с пределом времени на
// хост. Подставной Resolver в тестах — без DNS.

import (
	"context"
	"net"
	"net/netip"
	"sync"
	"time"
)

// Resolver разбирает имя сервера в адреса. Настоящий — системный резолвер,
// тесты подставляют свой.
type Resolver interface {
	LookupAddrs(ctx context.Context, host string) ([]netip.Addr, error)
}

// systemResolver — резолвер ОС (тот же, что у остального ядра).
type systemResolver struct{}

func (systemResolver) LookupAddrs(ctx context.Context, host string) ([]netip.Addr, error) {
	return net.DefaultResolver.LookupNetIP(ctx, "ip", host)
}

const (
	resolveParallelDefault = 8               // одновременных разборов
	resolvePerHostDefault  = 5 * time.Second // предел на один хост
)

// Причины отказа (ResolveFailure.reason): стабильные коды без имени сервера и
// без текста системной ошибки.
const (
	reasonNotFound     = "not_found"     // профиля нет
	reasonTimeout      = "timeout"       // хост не ответил за предел
	reasonNoAddress    = "no_address"    // ответ без пригодного адреса
	reasonLookupFailed = "lookup_failed" // резолвер вернул ошибку
	reasonCanceled     = "canceled"      // запрос отменён
	reasonChanged      = "changed"       // профиль изменён за время разбора
)

// resolveConfig — резолвер и пределы ResolveDomains; нулевое значение — системный
// резолвер, 8 параллельно, 5 с на хост (тесты подставляют своё).
type resolveConfig struct {
	resolver Resolver
	perHost  time.Duration
	parallel int
}

func (c resolveConfig) withDefaults() resolveConfig {
	if c.resolver == nil {
		c.resolver = systemResolver{}
	}
	if c.perHost <= 0 {
		c.perHost = resolvePerHostDefault
	}
	if c.parallel <= 0 {
		c.parallel = resolveParallelDefault
	}
	return c
}

// resolution — итог разбора одного имени: адрес либо код причины.
type resolution struct {
	addr   netip.Addr
	reason string // "" — адрес получен
}

// lookupAll разбирает имена параллельно (не больше c.parallel сразу). Отмена
// ctx завершает разбор: оставшиеся имена получают причину canceled.
func (c resolveConfig) lookupAll(ctx context.Context, hosts []string) map[string]resolution {
	c = c.withDefaults()
	results := make(map[string]resolution, len(hosts))
	var mu sync.Mutex
	var wg sync.WaitGroup
	slots := make(chan struct{}, c.parallel)
	for _, host := range hosts {
		slots <- struct{}{}
		wg.Add(1)
		go func() {
			defer wg.Done()
			defer func() { <-slots }()
			res := c.lookupOne(ctx, host)
			mu.Lock()
			results[host] = res
			mu.Unlock()
		}()
	}
	wg.Wait()
	return results
}

// lookupOne разбирает одно имя за не более c.perHost. Резолвер, не глядящий на
// контекст, не удерживает запрос: ответ ждётся в отдельной горутине.
func (c resolveConfig) lookupOne(ctx context.Context, host string) resolution {
	hctx, cancel := context.WithTimeout(ctx, c.perHost)
	defer cancel()
	type answer struct {
		addrs []netip.Addr
		err   error
	}
	done := make(chan answer, 1)
	go func() {
		addrs, err := c.resolver.LookupAddrs(hctx, host)
		done <- answer{addrs, err}
	}()
	select {
	case <-hctx.Done():
		return resolution{reason: interruptReason(ctx)}
	case a := <-done:
		if a.err != nil {
			if hctx.Err() != nil {
				return resolution{reason: interruptReason(ctx)}
			}
			return resolution{reason: reasonLookupFailed}
		}
		addr, ok := pickAddr(a.addrs)
		if !ok {
			return resolution{reason: reasonNoAddress}
		}
		return resolution{addr: addr}
	}
}

// interruptReason: разбор прерван отменой запроса (canceled) или пределом хоста.
func interruptReason(ctx context.Context) string {
	if ctx.Err() != nil {
		return reasonCanceled
	}
	return reasonTimeout
}

// pickAddr выбирает адрес сервера: IPv4, если он есть (не у всех сетей есть
// IPv6), иначе первый IPv6. Нулевой адрес и multicast не годятся: так
// «заблокированные» домены сообщают о блокировке.
func pickAddr(addrs []netip.Addr) (netip.Addr, bool) {
	var v6 netip.Addr
	for _, a := range addrs {
		a = a.Unmap().WithZone("")
		if !a.IsValid() || a.IsUnspecified() || a.IsMulticast() {
			continue
		}
		if a.Is4() {
			return a, true
		}
		if !v6.IsValid() {
			v6 = a
		}
	}
	return v6, v6.IsValid()
}
