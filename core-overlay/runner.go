// SPDX-License-Identifier: GPL-3.0-only

//go:build singbox_run

package mobile

// runner.go — раннер Android поверх inst.SingBoxRunner. Сохраняет все
// необязательные интерфейсы, которые проверяет ядро (api.NewServer —
// inst.LogSinker, inst.Manager — inst.ProxyDialer и inst.TrafficProvider),
// патчит конфиг (patch.go) и даёт sing-box контекст с платформой.

import (
	"context"
	"net"

	"github.com/anatolijlaptev1991-ctrl/lavray/core/internal/inst"
	"github.com/anatolijlaptev1991-ctrl/lavray/core/internal/traffic"
)

type androidRunner struct {
	inner *inst.SingBoxRunner
	h     *host
}

func newRunner(h *host) inst.Runner {
	return &androidRunner{inner: inst.NewSingBoxRunner(), h: h}
}

func (r *androidRunner) Start(ctx context.Context, cfg []byte) error {
	var p tunParams
	if cur := tunSettings.Load(); cur != nil {
		p = *cur
	}
	patched, rep, err := patchConfig(cfg, p)
	if err != nil {
		return err
	}
	r.h.ownerLookup.Store(rep.NeedsOwnerLookup)
	return r.inner.Start(r.h.boxContext(ctx), patched)
}

func (r *androidRunner) Stop(ctx context.Context) error { return r.inner.Stop(ctx) }

func (r *androidRunner) SetLogSink(sink inst.LogSink) { r.inner.SetLogSink(sink) }

func (r *androidRunner) DialProxy(ctx context.Context, network, address string) (net.Conn, error) {
	return r.inner.DialProxy(ctx, network, address)
}

func (r *androidRunner) TrafficSource() traffic.Source { return r.inner.TrafficSource() }

var (
	_ inst.Runner          = (*androidRunner)(nil)
	_ inst.ProxyDialer     = (*androidRunner)(nil)
	_ inst.LogSinker       = (*androidRunner)(nil)
	_ inst.TrafficProvider = (*androidRunner)(nil)
)
