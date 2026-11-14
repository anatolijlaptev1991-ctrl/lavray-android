// SPDX-License-Identifier: GPL-3.0-only

package api

// gateway_service.go — GatewayService на роутере (--platform openwrt): состояние,
// список и изменение политик устройств по MAC. Работа с nft и watchdog — пакет
// gateway; здесь только proto и коды Connect: недопустимый MAC или действие —
// INVALID_ARGUMENT, слишком много политик — RESOURCE_EXHAUSTED, сбой применения
// правил — INTERNAL (политика при этом не сохраняется).

import (
	"context"
	"errors"

	"connectrpc.com/connect"

	"github.com/anatolijlaptev1991-ctrl/lavray-android/core/internal/gateway"
	lavrayv1 "github.com/anatolijlaptev1991-ctrl/lavray-android/core/internal/gen/lavrayv1"
	"github.com/anatolijlaptev1991-ctrl/lavray-android/core/internal/inst"
)

// gatewayRPC — реализация lavrayv1connect.GatewayServiceHandler.
type gatewayRPC struct {
	svc *gateway.Service
	cfg gateway.Config
	run func() inst.Status // снимок инстанса: режим шлюза в GetGatewayStatus
}

// EnableGateway включает GatewayService и режимы GATEWAY_* (--platform openwrt).
// Вызывать до Register.
func (s *Server) EnableGateway(svc *gateway.Service, cfg gateway.Config) {
	s.ext.gateway = &gatewayRPC{svc: svc, cfg: cfg, run: s.inst.Status}
}

func (g *gatewayRPC) GetGatewayStatus(_ context.Context, _ *connect.Request[lavrayv1.Empty]) (*connect.Response[lavrayv1.GatewayStatus], error) {
	st := g.svc.Status()
	out := &lavrayv1.GatewayStatus{
		Mode:            g.mode(),
		Ipv6Enabled:     st.IPv6,
		NftRulesActive:  st.Active,
		WatchdogTripped: st.WatchdogTripped,
		LastError:       strpIf(st.LastError),
	}
	return connect.NewResponse(out), nil
}

// mode — режим по запущенному инстансу: шлюз «выкл», пока он не в режиме шлюза.
func (g *gatewayRPC) mode() lavrayv1.GatewayStatus_Mode {
	run := g.run()
	if run.State != inst.StateReady || run.Run == nil {
		return lavrayv1.GatewayStatus_MODE_OFF
	}
	switch run.Run.Mode {
	case modeGatewayTProxy:
		return lavrayv1.GatewayStatus_MODE_TPROXY
	case modeGatewayTun:
		return lavrayv1.GatewayStatus_MODE_TUN
	}
	return lavrayv1.GatewayStatus_MODE_OFF
}

func (g *gatewayRPC) ListDevicePolicies(_ context.Context, _ *connect.Request[lavrayv1.Empty]) (*connect.Response[lavrayv1.ListDevicePoliciesResponse], error) {
	list := g.svc.Policies()
	out := &lavrayv1.ListDevicePoliciesResponse{Policies: make([]*lavrayv1.DevicePolicy, 0, len(list))}
	for _, p := range list {
		out.Policies = append(out.Policies, policyToProto(p))
	}
	return connect.NewResponse(out), nil
}

func (g *gatewayRPC) SetDevicePolicy(_ context.Context, req *connect.Request[lavrayv1.SetDevicePolicyRequest]) (*connect.Response[lavrayv1.DevicePolicy], error) {
	in := req.Msg.GetPolicy()
	if in == nil {
		return nil, invalidArg("policy обязателен")
	}
	if in.GetAction() == lavrayv1.DevicePolicy_ACTION_UNSPECIFIED { // «наследовать общую»
		if err := g.svc.ClearPolicy(in.GetMac()); err != nil {
			return nil, gatewayErr(err)
		}
		mac, _ := gateway.NormalizeMAC(in.GetMac())
		return connect.NewResponse(&lavrayv1.DevicePolicy{Mac: mac}), nil
	}
	p, err := g.svc.SetPolicy(policyFromProto(in))
	if err != nil {
		return nil, gatewayErr(err)
	}
	return connect.NewResponse(policyToProto(p)), nil
}

func policyFromProto(p *lavrayv1.DevicePolicy) gateway.DevicePolicy {
	out := gateway.DevicePolicy{MAC: p.GetMac(), PresetID: p.GetPresetId()}
	switch p.GetAction() {
	case lavrayv1.DevicePolicy_ACTION_PROXY:
		out.Policy = gateway.PolicyProxy
	case lavrayv1.DevicePolicy_ACTION_DIRECT:
		out.Policy = gateway.PolicyDirect
	case lavrayv1.DevicePolicy_ACTION_PRESET:
		out.Policy = gateway.PolicyPreset
	}
	return out
}

func policyToProto(p gateway.DevicePolicy) *lavrayv1.DevicePolicy {
	out := &lavrayv1.DevicePolicy{Mac: p.MAC, PresetId: strpIf(p.PresetID), Hostname: strpIf(p.Hostname)}
	switch p.Policy {
	case gateway.PolicyProxy:
		out.Action = lavrayv1.DevicePolicy_ACTION_PROXY
	case gateway.PolicyDirect:
		out.Action = lavrayv1.DevicePolicy_ACTION_DIRECT
	case gateway.PolicyPreset:
		out.Action = lavrayv1.DevicePolicy_ACTION_PRESET
	}
	return out
}

// gatewayErr переводит ошибки пакета gateway в коды Connect.
func gatewayErr(err error) error {
	switch {
	case errors.Is(err, gateway.ErrBadMAC), errors.Is(err, gateway.ErrBadPolicy):
		return connect.NewError(connect.CodeInvalidArgument, err)
	case errors.Is(err, gateway.ErrTooManyPolicies):
		return connect.NewError(connect.CodeResourceExhausted, err)
	}
	return err // INTERNAL через mapErr
}
