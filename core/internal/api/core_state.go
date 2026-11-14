// SPDX-License-Identifier: GPL-3.0-only

package api

// core_state.go — соответствие состояний менеджера инстанса и режимов
// подключения типам контракта (вынесено из server.go: файл в size-ratchet).

import (
	"time"

	lavrayv1 "github.com/anatolijlaptev1991-ctrl/lavray-android/core/internal/gen/lavrayv1"
	"github.com/anatolijlaptev1991-ctrl/lavray-android/core/internal/inst"
)

func coreStateOf(st inst.State) lavrayv1.CoreState {
	switch st {
	case inst.StateStarting:
		return lavrayv1.CoreState_CORE_STATE_CONNECTING
	case inst.StateReady:
		return lavrayv1.CoreState_CORE_STATE_CONNECTED
	case inst.StateStopping:
		return lavrayv1.CoreState_CORE_STATE_DISCONNECTING
	case inst.StateCrashed:
		return lavrayv1.CoreState_CORE_STATE_ERROR
	}
	return lavrayv1.CoreState_CORE_STATE_IDLE
}

func connectModeOf(mode string) lavrayv1.ConnectMode {
	switch mode {
	case "tun":
		return lavrayv1.ConnectMode_CONNECT_MODE_TUN
	case "gateway_tproxy":
		return lavrayv1.ConnectMode_CONNECT_MODE_GATEWAY_TPROXY
	case "gateway_tun":
		return lavrayv1.ConnectMode_CONNECT_MODE_GATEWAY_TUN
	}
	return lavrayv1.ConnectMode_CONNECT_MODE_SYSTEM_PROXY
}

// nowMs — время для стримов (выделено для будущих тест-инъекций).
func nowMs() int64 { return time.Now().UnixMilli() }
