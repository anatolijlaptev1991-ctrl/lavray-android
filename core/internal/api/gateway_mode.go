// SPDX-License-Identifier: GPL-3.0-only

package api

// gateway_mode.go — режимы подключения GATEWAY_* и жизненный цикл правил шлюза.
// Режимы допустимы только при --platform openwrt (EnableGateway); на десктопе
// ответ INVALID_ARGUMENT. Правила nft ставятся, когда инстанс в режиме
// gateway_tproxy стал Ready, и снимаются при любом выходе из этого состояния
// (отключение, падение, выход процесса).

import (
	"context"

	"github.com/anatolijlaptev1991-ctrl/lavray-android/core/internal/cfgbuild"
	lavrayv1 "github.com/anatolijlaptev1991-ctrl/lavray-android/core/internal/gen/lavrayv1"
	"github.com/anatolijlaptev1991-ctrl/lavray-android/core/internal/inst"
	"github.com/anatolijlaptev1991-ctrl/lavray-android/core/internal/logs"
)

// Строки режима в inst.RunInfo.Mode.
const (
	modeSystemProxy   = "system_proxy"
	modeTun           = "tun"
	modeGatewayTProxy = "gateway_tproxy"
	modeGatewayTun    = "gateway_tun"
)

func isGatewayMode(m lavrayv1.ConnectMode) bool {
	return m == lavrayv1.ConnectMode_CONNECT_MODE_GATEWAY_TPROXY || m == lavrayv1.ConnectMode_CONNECT_MODE_GATEWAY_TUN
}

// runModeString — значение RunInfo.Mode для режима подключения.
func runModeString(m lavrayv1.ConnectMode) string {
	switch m {
	case lavrayv1.ConnectMode_CONNECT_MODE_TUN:
		return modeTun
	case lavrayv1.ConnectMode_CONNECT_MODE_GATEWAY_TPROXY:
		return modeGatewayTProxy
	case lavrayv1.ConnectMode_CONNECT_MODE_GATEWAY_TUN:
		return modeGatewayTun
	}
	return modeSystemProxy
}

// checkGatewayMode отвергает режим шлюза на десктопе.
func (s *Server) checkGatewayMode(m lavrayv1.ConnectMode) error {
	if isGatewayMode(m) && s.ext.gateway == nil {
		return invalidArg("режим шлюза доступен только в сборке для роутера (--platform openwrt); на компьютере — системный прокси или TUN")
	}
	return nil
}

// applyGateway дополняет настройки сборки для режимов шлюза: tproxy-вход с
// портом шлюза либо внутренний TUN роутера.
func (s *Server) applyGateway(st *cfgbuild.BuildSettings, m lavrayv1.ConnectMode) {
	g := s.ext.gateway
	if g == nil {
		return
	}
	st.OpenWRT = true
	switch m {
	case lavrayv1.ConnectMode_CONNECT_MODE_GATEWAY_TPROXY:
		st.TProxy = cfgbuild.TProxySettings{Enabled: true, Port: g.cfg.TProxyPort, IPv6: g.cfg.IPv6}
	case lavrayv1.ConnectMode_CONNECT_MODE_GATEWAY_TUN:
		st.Tun.Enabled = true
	}
}

// RunGateway следит за инстансом и держит правила nft в согласии с ним, пока не
// отменён ctx; запускать в горутине после EnableGateway. При старте снимает
// правила, оставшиеся от прошлого запуска.
func (s *Server) RunGateway(ctx context.Context) {
	g := s.ext.gateway
	if g == nil {
		return
	}
	if err := g.svc.ClearOrphans(); err != nil {
		s.logs.Logf(logs.LevelWarn, logs.SourceAPI, "шлюз: %v", err)
	}
	g.svc.SetNotify(func(failover bool) {
		if failover {
			s.logs.Logf(logs.LevelWarn, logs.SourceAPI, "шлюз: нет связи, правила nft сняты — интернет у LAN идёт напрямую")
		} else {
			s.logs.Logf(logs.LevelInfo, logs.SourceAPI, "шлюз: связь восстановлена, правила nft возвращены")
		}
	})
	go g.svc.Run(ctx, 0, s.gatewayProbe)
	events, cancel := s.inst.Subscribe()
	defer cancel()
	s.syncGateway(s.inst.Status()) // инстанс мог подняться до подписки
	for {
		select {
		case <-ctx.Done():
			return
		case ev, ok := <-events:
			if !ok {
				return
			}
			s.syncGateway(ev.Status)
		}
	}
}

// gatewayProbe — связь для watchdog: инстанс шлюза работает.
func (s *Server) gatewayProbe() bool { return isGatewayRun(s.inst.Status()) }

func isGatewayRun(st inst.Status) bool {
	return st.State == inst.StateReady && st.Run != nil && st.Run.Mode == modeGatewayTProxy
}

// syncGateway включает или снимает правила по состоянию инстанса.
func (s *Server) syncGateway(st inst.Status) {
	g := s.ext.gateway
	want := isGatewayRun(st)
	switch {
	case want && !g.svc.Enabled():
		if err := g.svc.Activate(); err != nil {
			s.logs.Logf(logs.LevelError, logs.SourceAPI, "шлюз: правила nft не применены: %v", err)
		} else {
			s.logs.Logf(logs.LevelInfo, logs.SourceAPI, "шлюз: правила nft применены")
		}
	case !want && g.svc.Enabled():
		s.shutdownGateway()
	}
}

// shutdownGateway снимает правила; зовётся при выходе из режима шлюза и при
// выходе процесса (StopInstance).
func (s *Server) shutdownGateway() {
	g := s.ext.gateway
	if g == nil {
		return
	}
	if err := g.svc.Deactivate(); err != nil {
		s.logs.Logf(logs.LevelError, logs.SourceAPI, "шлюз: правила nft не сняты: %v", err)
		return
	}
	s.logs.Logf(logs.LevelInfo, logs.SourceAPI, "шлюз: правила nft сняты")
}
