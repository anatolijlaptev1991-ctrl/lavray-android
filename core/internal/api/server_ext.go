// SPDX-License-Identifier: GPL-3.0-only

package api

// server_ext.go — необязательные сервисы, которые включает обвязка процесса
// (cmd/lavray-core): веб-режим (WebSessionService) и шлюз OpenWrt
// (GatewayService). Без включения WebSessionService отвечает UNIMPLEMENTED
// (десктоп без --web-bind: постоянная запись в allowUnimplemented), а
// GatewayService — INVALID_ARGUMENT («шлюз только на роутере»).

import "github.com/anatolijlaptev1991-ctrl/lavray-android/core/internal/gen/lavrayv1connect"

// serverExt — включённые необязательные сервисы; задаются до Register.
type serverExt struct {
	web     lavrayv1connect.WebSessionServiceHandler
	gateway *gatewayRPC // не nil только при --platform openwrt (EnableGateway)
}

// webSessionHandler — включённый сервис либо заглушка UNIMPLEMENTED.
func (s *Server) webSessionHandler() lavrayv1connect.WebSessionServiceHandler {
	if s.ext.web != nil {
		return s.ext.web
	}
	return lavrayv1connect.UnimplementedWebSessionServiceHandler{}
}

// gatewayHandler — шлюз роутера либо десктопная заглушка с INVALID_ARGUMENT.
func (s *Server) gatewayHandler() lavrayv1connect.GatewayServiceHandler {
	if s.ext.gateway != nil {
		return s.ext.gateway
	}
	return desktopGateway{}
}
