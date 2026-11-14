// SPDX-License-Identifier: GPL-3.0-only

package cfgbuild

import (
	"testing"

	"connectrpc.com/connect"
)

func tproxyInbound_(t *testing.T, st *BuildSettings, forTest bool) (map[string]any, error) {
	t.Helper()
	inbounds, err := buildInbounds(st, forTest)
	if err != nil {
		return nil, err
	}
	for _, raw := range inbounds {
		if in, _ := raw.(map[string]any); in["tag"] == "tproxy-in" {
			return in, nil
		}
	}
	return nil, nil
}

// TestTProxyInboundBuilt — на OpenWrt режим шлюза даёт tproxy-вход на порту
// шлюза: IPv4 по умолчанию, dual-stack при явном IPv6.
func TestTProxyInboundBuilt(t *testing.T) {
	st := DefaultSettings()
	st.OpenWRT = true
	st.TProxy = TProxySettings{Enabled: true, Port: 7893}
	in, err := tproxyInbound_(t, st, false)
	if err != nil || in == nil {
		t.Fatalf("tproxy-in не собран: in=%v err=%v", in, err)
	}
	if in["type"] != "tproxy" || in["listen"] != "0.0.0.0" || in["listen_port"] != 7893 {
		t.Fatalf("параметры tproxy-in: %v", in)
	}
	st.TProxy.IPv6 = true
	in, _ = tproxyInbound_(t, st, false)
	if in["listen"] != "::" {
		t.Fatalf("IPv6 включён, а слушатель %v", in["listen"])
	}
}

// TestTProxyRequiresOpenWrt — на десктопе tproxy недопустим: INVALID_ARGUMENT (P).
func TestTProxyRequiresOpenWrt(t *testing.T) {
	st := DefaultSettings()
	st.TProxy = TProxySettings{Enabled: true, Port: 7893}
	_, err := tproxyInbound_(t, st, false)
	ce, ok := AsError(err)
	if !ok || ce.Code != connect.CodeInvalidArgument {
		t.Fatalf("десктоп: err=%v, ждали INVALID_ARGUMENT", err)
	}
}

func TestTProxyOffByDefaultAndForTest(t *testing.T) {
	st := DefaultSettings()
	if in, err := tproxyInbound_(t, st, false); err != nil || in != nil {
		t.Fatalf("по умолчанию tproxy-in не нужен: %v %v", in, err)
	}
	st.OpenWRT = true
	st.TProxy = TProxySettings{Enabled: true, Port: 7893}
	if in, _ := tproxyInbound_(t, st, true); in != nil {
		t.Fatal("временный инстанс теста задержки получил слушатель шлюза")
	}
}

func TestTProxyPortBoundaries(t *testing.T) {
	for _, port := range []int{0, -1, 65536} {
		st := DefaultSettings()
		st.OpenWRT = true
		st.TProxy = TProxySettings{Enabled: true, Port: port}
		if _, err := tproxyInbound_(t, st, false); err == nil {
			t.Errorf("порт %d принят", port)
		}
	}
	for _, port := range []int{1, 65535} {
		st := DefaultSettings()
		st.OpenWRT = true
		st.TProxy = TProxySettings{Enabled: true, Port: port}
		if in, err := tproxyInbound_(t, st, false); err != nil || in == nil {
			t.Errorf("порт %d отклонён: %v", port, err)
		}
	}
}

// TestTProxyBuildsFullConfig — tproxy-in входит в итоговый конфиг Build.
func TestTProxyBuildsFullConfig(t *testing.T) {
	st := DefaultSettings()
	st.OpenWRT = true
	st.TProxy = TProxySettings{Enabled: true, Port: 7893}
	res, err := Build(BuildRequest{Profile: socksProfile(7, "1.2.3.4", 1080), Settings: st})
	if err != nil {
		t.Fatal(err)
	}
	found := false
	switch ins := res.Config["inbounds"].(type) {
	case []any:
		for _, raw := range ins {
			if in, _ := raw.(map[string]any); in["tag"] == "tproxy-in" {
				found = true
			}
		}
	case []map[string]any:
		for _, in := range ins {
			found = found || in["tag"] == "tproxy-in"
		}
	}
	if !found {
		t.Fatal("в конфиге нет tproxy-in")
	}
}
