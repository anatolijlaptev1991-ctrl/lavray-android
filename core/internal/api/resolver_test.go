// SPDX-License-Identifier: GPL-3.0-only

package api

import (
	"context"
	"fmt"
	"net/netip"
	"sync"
	"time"
)

// fakeResolver — подставной резолвер: ответы по имени, без DNS. Умеет
// задерживать/подвешивать ответ и считает параллельность.
type fakeResolver struct {
	mu        sync.Mutex
	answers   map[string][]netip.Addr
	failures  map[string]error
	hang      map[string]bool          // имя, на которое резолвер отвечает только по отмене контекста
	delay     time.Duration            // пауза каждого ответа
	calls     map[string]int           // обращений по имени
	inflight  int                      // сейчас в разборе
	peak      int                      // наибольшее число одновременных разборов
	ignoreCtx map[string]time.Duration // имя, на которое резолвер спит, не глядя на контекст
}

func newFakeResolver() *fakeResolver {
	return &fakeResolver{
		answers:   map[string][]netip.Addr{},
		failures:  map[string]error{},
		hang:      map[string]bool{},
		calls:     map[string]int{},
		ignoreCtx: map[string]time.Duration{},
	}
}

func addrs(list ...string) []netip.Addr {
	out := make([]netip.Addr, 0, len(list))
	for _, s := range list {
		out = append(out, netip.MustParseAddr(s))
	}
	return out
}

func (r *fakeResolver) LookupAddrs(ctx context.Context, host string) ([]netip.Addr, error) {
	r.mu.Lock()
	r.calls[host]++
	r.inflight++
	r.peak = max(r.peak, r.inflight)
	hang, sleep, delay := r.hang[host], r.ignoreCtx[host], r.delay
	r.mu.Unlock()
	defer func() {
		r.mu.Lock()
		r.inflight--
		r.mu.Unlock()
	}()
	switch {
	case hang:
		<-ctx.Done()
		return nil, ctx.Err()
	case sleep > 0:
		time.Sleep(sleep)
	case delay > 0:
		select {
		case <-time.After(delay):
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if err := r.failures[host]; err != nil {
		return nil, err
	}
	if a, ok := r.answers[host]; ok {
		return a, nil
	}
	return nil, fmt.Errorf("no such host %s", host)
}

func (r *fakeResolver) callCount(host string) int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.calls[host]
}
