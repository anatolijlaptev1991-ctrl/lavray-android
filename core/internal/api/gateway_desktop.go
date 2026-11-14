// SPDX-License-Identifier: GPL-3.0-only

package api

// gateway_desktop.go — GatewayService на компьютере: шлюз существует только на
// роутере (--platform openwrt), поэтому все методы отвечают INVALID_ARGUMENT с
// пояснением (контракт proto: ConnectMode.GATEWAY_* на десктопе — тот же код).

import (
	"context"

	"connectrpc.com/connect"

	lavrayv1 "github.com/anatolijlaptev1991-ctrl/lavray-android/core/internal/gen/lavrayv1"
)

// errDesktopGateway — общий ответ десктопной заглушки.
func errDesktopGateway() error {
	return invalidArg("шлюз доступен только в сборке для роутера (--platform openwrt); на компьютере — системный прокси или TUN")
}

// desktopGateway — реализация lavrayv1connect.GatewayServiceHandler для десктопа.
type desktopGateway struct{}

func (desktopGateway) GetGatewayStatus(context.Context, *connect.Request[lavrayv1.Empty]) (*connect.Response[lavrayv1.GatewayStatus], error) {
	return nil, errDesktopGateway()
}

func (desktopGateway) ListDevicePolicies(context.Context, *connect.Request[lavrayv1.Empty]) (*connect.Response[lavrayv1.ListDevicePoliciesResponse], error) {
	return nil, errDesktopGateway()
}

func (desktopGateway) SetDevicePolicy(context.Context, *connect.Request[lavrayv1.SetDevicePolicyRequest]) (*connect.Response[lavrayv1.DevicePolicy], error) {
	return nil, errDesktopGateway()
}
