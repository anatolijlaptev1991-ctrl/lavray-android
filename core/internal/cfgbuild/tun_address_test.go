// SPDX-License-Identifier: GPL-3.0-only

package cfgbuild

import (
	"net"
	"testing"
)

// overlaps — пересекаются ли две сети.
func overlaps(a, b *net.IPNet) bool { return a.Contains(b.IP) || b.Contains(a.IP) }

// Адрес TUN по умолчанию не пересекается с NekoBox (0.2.1): оба клиента
// работают на одном ПК, и одна подсеть у двух интерфейсов ломала маршрутизацию.
func TestTunAddressDoesNotOverlapNekoBox(t *testing.T) {
	st := DefaultSettings()
	st.Tun.Enabled = true
	st.Tun.IPv6 = true
	inbounds, err := buildInbounds(st, false)
	if err != nil {
		t.Fatal(err)
	}
	var addrs []any
	for _, raw := range inbounds {
		if in, _ := raw.(map[string]any); in["tag"] == "tun-in" {
			addrs, _ = in["address"].([]any)
		}
	}
	if len(addrs) != 2 {
		t.Fatalf("адреса tun-in: %v, ожидались IPv4 и IPv6", addrs)
	}
	neko := []string{"172.19.0.1/28", "fdfe:dcba:9876::1/126"}
	for i, raw := range addrs {
		_, ours, err := net.ParseCIDR(raw.(string))
		if err != nil {
			t.Fatalf("адрес %q: %v", raw, err)
		}
		_, theirs, _ := net.ParseCIDR(neko[i])
		if overlaps(ours, theirs) {
			t.Errorf("адрес TUN %s пересекается с NekoBox %s", ours, theirs)
		}
	}
	if addrs[0] != "172.18.0.1/30" {
		t.Errorf("IPv4-адрес TUN %v, ожидался 172.18.0.1/30", addrs[0])
	}
}
