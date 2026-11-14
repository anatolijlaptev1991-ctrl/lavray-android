// SPDX-License-Identifier: GPL-3.0-only

package api

// nekoray_route.go — ImportNekoRayRoute (T-80/Э-2): маршрут NekoRay 4.x
// (config/routes_box/<name>) → пользовательский набор через presets.ParseNekoRoute.
// Общий путь вставки с ImportPreset — insertImported; тот же код переиспользует
// перенос из NekoBox (T-78).

import (
	"context"
	"strings"

	"connectrpc.com/connect"

	lavrayv1 "github.com/anatolijlaptev1991-ctrl/lavray-android/core/internal/gen/lavrayv1"
	"github.com/anatolijlaptev1991-ctrl/lavray-android/core/internal/presets"
)

// nekoRouteFallbackName — имя набора, если клиент его не передал.
const nekoRouteFallbackName = "NekoRay route"

// ImportNekoRayRoute конвертирует маршрут NekoRay в новый пользовательский набор;
// предупреждения — что конвертер отбросил или заменил.
func (s *Server) ImportNekoRayRoute(ctx context.Context, req *connect.Request[lavrayv1.ImportNekoRayRouteRequest]) (*connect.Response[lavrayv1.ImportNekoRayRouteResponse], error) {
	name := strings.TrimSpace(optStr(req.Msg.Name))
	if name == "" {
		name = nekoRouteFallbackName
	}
	p, warnings, err := presets.ParseNekoRoute([]byte(req.Msg.NekoboxJson), name)
	if err != nil {
		return nil, mapErr(err)
	}
	out, err := s.insertImported(p)
	if err != nil {
		return nil, mapErr(err)
	}
	return connect.NewResponse(&lavrayv1.ImportNekoRayRouteResponse{Preset: out, Warnings: warnings}), nil
}
