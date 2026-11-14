// SPDX-License-Identifier: GPL-3.0-only

package mobile

import (
	"net"
	"net/netip"
	"testing"

	C "github.com/sagernet/sing-box/constant"
)

// kotlinInterfacesJSON — дословно то, что строит InterfaceInfo.encodeList в Kotlin
// (тест InterfaceInfoTest.goldenJsonForTheGoSide): флаги — маски IFF_* Linux по типу сети.
const kotlinInterfacesJSON = `[{"name":"wlan0","index":3,"mtu":1500,"addrs":["192.168.1.5/24","fe80::1/64"],"flags":4163,"metered":false,"type":"wifi","dns":["192.168.1.1"],"gateway":["192.168.1.1"]},` +
	`{"name":"rmnet0","index":4,"mtu":1400,"addrs":["10.1.2.3/30"],"flags":4177,"metered":true,"type":"cellular","dns":[],"gateway":[]}]`

func TestParseInterfacesFromKotlin(t *testing.T) {
	list, err := parseInterfaces(kotlinInterfacesJSON)
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 2 {
		t.Fatalf("интерфейсов %d, ждали 2", len(list))
	}
	wifi, cell := list[0], list[1]

	wantWifi := net.FlagUp | net.FlagRunning | net.FlagBroadcast | net.FlagMulticast
	if wifi.Flags != wantWifi {
		t.Errorf("wlan0 flags = %v, ждали %v", wifi.Flags, wantWifi)
	}
	if wifi.Flags&net.FlagPointToPoint != 0 {
		t.Error("wlan0 не должен быть точка-точка")
	}
	if wifi.Type != C.InterfaceTypeWIFI || wifi.Expensive {
		t.Errorf("wlan0 = %+v", wifi)
	}
	if len(wifi.DNSServers) != 1 || wifi.DNSServers[0] != "192.168.1.1" {
		t.Errorf("wlan0 dns = %v", wifi.DNSServers)
	}
	if len(wifi.Gateways) != 1 || wifi.Gateways[0] != netip.MustParseAddr("192.168.1.1") {
		t.Errorf("wlan0 gateway = %v", wifi.Gateways)
	}

	wantCell := net.FlagUp | net.FlagRunning | net.FlagPointToPoint | net.FlagMulticast
	if cell.Flags != wantCell {
		t.Errorf("rmnet0 flags = %v, ждали %v", cell.Flags, wantCell)
	}
	if cell.Flags&net.FlagBroadcast != 0 {
		t.Error("rmnet0 не должен быть широковещательным")
	}
	if cell.Type != C.InterfaceTypeCellular || !cell.Expensive {
		t.Errorf("rmnet0 = %+v", cell)
	}
}
