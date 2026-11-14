// SPDX-License-Identifier: GPL-3.0-only

package api

// preset_convert.go — конвертация набора маршрутов: формат LAVRay
// (internal/presets) ↔ контракт lavray.v1 (RoutingPreset, DnsSettings).
// Вынесено из convert.go (T-80, первый коммит Ф4); логика не менялась.

import (
	"github.com/anatolijlaptev1991-ctrl/lavray-android/core/internal/gen/lavrayv1"
	"github.com/anatolijlaptev1991-ctrl/lavray-android/core/internal/presets"
)

// ---- RoutingPreset ↔ presets.Preset ----

var strategyToModel = map[string]lavrayv1.DomainStrategy{
	"":            lavrayv1.DomainStrategy_DOMAIN_STRATEGY_UNSPECIFIED,
	"prefer_ipv4": lavrayv1.DomainStrategy_DOMAIN_STRATEGY_PREFER_IPV4,
	"prefer_ipv6": lavrayv1.DomainStrategy_DOMAIN_STRATEGY_PREFER_IPV6,
	"ipv4_only":   lavrayv1.DomainStrategy_DOMAIN_STRATEGY_IPV4_ONLY,
	"ipv6_only":   lavrayv1.DomainStrategy_DOMAIN_STRATEGY_IPV6_ONLY,
}

var strategyFromModel = func() map[lavrayv1.DomainStrategy]string {
	m := map[lavrayv1.DomainStrategy]string{}
	for k, v := range strategyToModel {
		m[v] = k
	}
	return m
}()

func outboundToProto(s string) lavrayv1.Outbound {
	switch s {
	case "proxy":
		return lavrayv1.Outbound_OUTBOUND_PROXY
	case "direct", "bypass": // тег bypass = DIRECT (контракт, enum Outbound)
		return lavrayv1.Outbound_OUTBOUND_DIRECT
	case "block":
		return lavrayv1.Outbound_OUTBOUND_BLOCK
	}
	return lavrayv1.Outbound_OUTBOUND_UNSPECIFIED
}

func outboundFromProto(o lavrayv1.Outbound) string {
	switch o {
	case lavrayv1.Outbound_OUTBOUND_PROXY:
		return "proxy"
	case lavrayv1.Outbound_OUTBOUND_DIRECT:
		return "direct"
	case lavrayv1.Outbound_OUTBOUND_BLOCK:
		return "block"
	}
	return ""
}

// presetToProto — формат LAVRay → контракт. DisplayName: для builtin — ключ
// локализации "preset.<id>", для пользовательских — говорящее имя.
func presetToProto(p *presets.Preset) *lavrayv1.RoutingPreset {
	r := &lavrayv1.RoutingPreset{
		Id:                     p.ID,
		Builtin:                p.Builtin,
		Revision:               int32(p.Revision),
		ProxyDomain:            p.Routing.ProxyDomain,
		DirectDomain:           p.Routing.DirectDomain,
		BlockDomain:            p.Routing.BlockDomain,
		ProxyIp:                p.Routing.ProxyIP,
		DirectIp:               p.Routing.DirectIP,
		BlockIp:                p.Routing.BlockIP,
		DefaultOutbound:        outboundToProto(p.Routing.DefOutbound),
		OutboundDomainStrategy: strategyToModel[p.Routing.OutboundDomainStrategy],
		CustomRulesJson:        strpIf(p.Routing.CustomRules),
		BuiltinSourceId:        p.SourceID,
		BuiltinSourceRevision:  int32(p.SourceRevision),
	}
	if p.Routing.SniffingMode == 1 {
		r.SniffingMode = lavrayv1.SniffingMode_SNIFFING_FOR_ROUTING
	}
	if p.Builtin {
		r.DisplayName = "preset." + p.ID
	} else {
		r.DisplayName = presetDisplayName(p)
	}
	r.ProcessRules = make([]*lavrayv1.ProcessRule, 0, len(p.Routing.ProcessRules))
	for _, pr := range p.Routing.ProcessRules {
		r.ProcessRules = append(r.ProcessRules, &lavrayv1.ProcessRule{
			ProcessNames: pr.ProcessNames,
			Outbound:     outboundToProto(pr.Outbound),
			Platforms:    pr.Platforms,
		})
	}
	r.Dns = dnsBlockToProto(&p.DNS)
	return r
}

// presetFromProto — контракт → формат LAVRay (проверки — отдельно, PrepareUser).
// Источник копии (builtin_source_*) из контракта не читается: его ведёт ядро.
func presetFromProto(r *lavrayv1.RoutingPreset) *presets.Preset {
	p := &presets.Preset{
		Schema:   presets.SchemaRef,
		Version:  presets.SchemaVersion,
		ID:       r.GetId(),
		Builtin:  false, // пользовательские пресеты из контракта никогда не builtin
		Revision: int(r.GetRevision()),
		Name:     presets.Localized{"en": r.GetDisplayName()},
	}
	routing := &p.Routing
	routing.DefOutbound = outboundFromProto(r.GetDefaultOutbound())
	if routing.DefOutbound == "" {
		routing.DefOutbound = "direct"
	}
	routing.OutboundDomainStrategy = strategyFromModel[r.GetOutboundDomainStrategy()]
	if r.GetSniffingMode() == lavrayv1.SniffingMode_SNIFFING_FOR_ROUTING {
		routing.SniffingMode = 1
	}
	routing.ProxyDomain = r.GetProxyDomain()
	routing.DirectDomain = r.GetDirectDomain()
	routing.BlockDomain = r.GetBlockDomain()
	routing.ProxyIP = r.GetProxyIp()
	routing.DirectIP = r.GetDirectIp()
	routing.BlockIP = r.GetBlockIp()
	routing.CustomRules = optStr(r.CustomRulesJson)
	for _, pr := range r.GetProcessRules() {
		if pr == nil {
			continue
		}
		routing.ProcessRules = append(routing.ProcessRules, presets.ProcessRule{
			ProcessNames: pr.GetProcessNames(),
			Outbound:     outboundFromProto(pr.GetOutbound()),
			Platforms:    pr.GetPlatforms(),
		})
	}
	if d := r.GetDns(); d != nil {
		p.DNS = *dnsBlockFromProto(d)
	} else {
		p.DNS = presets.DNSBlock{Mode: "simple", RemoteDNS: "https://dns.google/dns-query", DirectDNS: "local", RoutingEnabled: true}
	}
	return p
}

// ---- DnsSettings ↔ presets.DNSBlock ----

func dnsBlockToProto(d *presets.DNSBlock) *lavrayv1.DnsSettings {
	if d == nil {
		return nil
	}
	out := &lavrayv1.DnsSettings{
		RemoteDns:      strpIf(d.RemoteDNS),
		DirectDns:      strpIf(d.DirectDNS),
		RoutingEnabled: d.RoutingEnabled,
		AdvancedJson:   strpIf(d.AdvancedJSON),
		UnderlyingDns:  strpIf(d.UnderlyingDNS),
	}
	if s, ok := strategyToModel[d.RemoteQueryStrategy]; ok {
		v := s
		out.RemoteQueryStrategy = &v
	}
	if d.Mode == "advanced" {
		out.Mode = lavrayv1.DnsSettings_MODE_ADVANCED
	} else {
		out.Mode = lavrayv1.DnsSettings_MODE_SIMPLE
	}
	return out
}

func dnsBlockFromProto(d *lavrayv1.DnsSettings) *presets.DNSBlock {
	out := &presets.DNSBlock{
		RemoteDNS:           optStr(d.RemoteDns),
		DirectDNS:           optStr(d.DirectDns),
		RoutingEnabled:      d.GetRoutingEnabled(),
		RemoteQueryStrategy: strategyFromModel[d.GetRemoteQueryStrategy()],
		AdvancedJSON:        optStr(d.AdvancedJson),
		UnderlyingDNS:       optStr(d.UnderlyingDns),
		Mode:                "simple",
	}
	if d.GetMode() == lavrayv1.DnsSettings_MODE_ADVANCED {
		out.Mode = "advanced"
	}
	return out
}
