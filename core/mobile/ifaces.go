// SPDX-License-Identifier: GPL-3.0-only

package mobile

// ifaces.go — разбор JSON сетевых интерфейсов от Kotlin (Platform.Interfaces).

import (
	"encoding/json"
	"fmt"
	"net"
	"net/netip"

	"github.com/sagernet/sing-box/adapter"
	C "github.com/sagernet/sing-box/constant"
	"github.com/sagernet/sing/common/control"
)

// Биты IFF_* Linux: Kotlin собирает их из android.system.OsConstants.
const (
	iffUp          = 0x1
	iffBroadcast   = 0x2
	iffLoopback    = 0x8
	iffPointToPt   = 0x10
	iffRunning     = 0x40
	iffMulticast   = 0x1000
	defaultIfFlags = iffUp | iffRunning | iffMulticast
)

type ifaceJSON struct {
	Name    string          `json:"name"`
	Index   int             `json:"index"`
	MTU     int             `json:"mtu"`
	Addrs   []string        `json:"addrs"`
	Flags   *uint32         `json:"flags"`
	Metered bool            `json:"metered"`
	Type    json.RawMessage `json:"type"`
	DNS     []string        `json:"dns"`
	Gateway []string        `json:"gateway"`
}

// parseInterfaces превращает JSON-массив в интерфейсы sing-box. Повторные имена
// отбрасываются, непонятные адреса пропускаются.
func parseInterfaces(raw string) ([]adapter.NetworkInterface, error) {
	var list []ifaceJSON
	if err := json.Unmarshal([]byte(raw), &list); err != nil {
		return nil, fmt.Errorf("mobile: Interfaces(): %w", err)
	}
	seen := make(map[string]bool, len(list))
	out := make([]adapter.NetworkInterface, 0, len(list))
	for _, it := range list {
		if it.Name == "" || seen[it.Name] {
			continue
		}
		seen[it.Name] = true
		out = append(out, toNetworkInterface(it))
	}
	return out, nil
}

func toNetworkInterface(it ifaceJSON) adapter.NetworkInterface {
	flags := uint32(defaultIfFlags)
	if it.Flags != nil {
		flags = *it.Flags
	}
	var gateways []netip.Addr
	for _, g := range it.Gateway {
		if a, err := netip.ParseAddr(g); err == nil {
			gateways = append(gateways, a.Unmap().WithZone(""))
		}
	}
	return adapter.NetworkInterface{
		Interface: control.Interface{
			Index:     it.Index,
			MTU:       it.MTU,
			Name:      it.Name,
			Addresses: parsePrefixes(it.Addrs),
			Flags:     linkFlags(flags),
		},
		Type:       parseIfType(it.Type),
		DNSServers: it.DNS,
		Gateways:   gateways,
		Expensive:  it.Metered,
	}
}

func parsePrefixes(list []string) []netip.Prefix {
	out := make([]netip.Prefix, 0, len(list))
	for _, s := range list {
		if p, err := netip.ParsePrefix(s); err == nil {
			out = append(out, p)
		} else if a, err := netip.ParseAddr(s); err == nil {
			out = append(out, netip.PrefixFrom(a, a.BitLen()))
		}
	}
	return out
}

// parseIfType принимает строку ("wifi") или число (C.InterfaceType).
func parseIfType(raw json.RawMessage) C.InterfaceType {
	var s string
	if json.Unmarshal(raw, &s) == nil {
		if t, ok := C.StringToInterfaceType[s]; ok {
			return t
		}
		return C.InterfaceTypeOther
	}
	var n int
	if json.Unmarshal(raw, &n) == nil && n >= 0 && n <= int(C.InterfaceTypeOther) {
		return C.InterfaceType(n)
	}
	return C.InterfaceTypeOther
}

// linkFlags — IFF_* Linux в net.Flags (не зависит от ОС, чтобы работали тесты
// на рабочей станции).
func linkFlags(raw uint32) net.Flags {
	var f net.Flags
	pairs := []struct {
		bit  uint32
		flag net.Flags
	}{
		{iffUp, net.FlagUp}, {iffRunning, net.FlagRunning}, {iffBroadcast, net.FlagBroadcast},
		{iffLoopback, net.FlagLoopback}, {iffPointToPt, net.FlagPointToPoint}, {iffMulticast, net.FlagMulticast},
	}
	for _, p := range pairs {
		if raw&p.bit != 0 {
			f |= p.flag
		}
	}
	return f
}
