// SPDX-License-Identifier: GPL-3.0-only

package model

import (
	"encoding/json"
	"strings"
	"testing"
)

func mustJSON(t *testing.T, v any) string {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return string(b)
}

// roundtrip проверяет marshal→unmarshal→marshal на идентичность.
func roundtrip(t *testing.T, p *Profile) *Profile {
	t.Helper()
	b1, err := json.Marshal(p)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var q Profile
	if err := json.Unmarshal(b1, &q); err != nil {
		t.Fatalf("unmarshal %s: %v", b1, err)
	}
	b2, err := json.Marshal(&q)
	if err != nil {
		t.Fatalf("re-marshal: %v", err)
	}
	if string(b1) != string(b2) {
		t.Fatalf("roundtrip mismatch:\nfirst:  %s\nsecond: %s", b1, b2)
	}
	return &q
}

func sampleProfile() *Profile {
	latency := int32(42)
	tested := int64(1729700000000)
	return &Profile{
		ID: 7, GroupID: 1, Type: TypeHysteria2,
		Name: "hy2 hop", Address: "hy.example.com", Port: 443,
		Favorite: true, Tags: []string{"prod", "eu"}, Notes: "заметка",
		TestResult:            TestResult{LatencyMs: &latency, TestedAtMs: &tested},
		Traffic:               TrafficCounters{UplinkBytes: 11, DownlinkBytes: 22},
		CustomConfigJSON:      `{"route":{}}`,
		CustomFieldsConfirmed: true,
		Detail: &Hysteria2Detail{
			Password: "pw", ObfsPassword: "obfs", UploadMbps: 100, DownloadMbps: 200,
			ServerPorts: "20000:30000", HopInterval: "30s", SNI: "hy.example.com",
		},
	}
}

func TestProfileRoundtripAllTypes(t *testing.T) {
	profiles := []*Profile{
		{ID: 1, GroupID: 1, Type: TypeSOCKS, Name: "s5", Address: "1.2.3.4", Port: 1080,
			Detail: &SocksHTTPDetail{Proto: "socks5", Username: "u", Password: "p"}},
		{ID: 2, GroupID: 1, Type: TypeHTTP, Name: "http", Address: "h.io", Port: 8080,
			Detail: &SocksHTTPDetail{Proto: "http"}},
		{ID: 3, GroupID: 1, Type: TypeShadowsocks, Name: "ss", Address: "s.io", Port: 8388,
			Detail: &ShadowsocksDetail{Method: "2022-blake3-aes-128-gcm", Password: "x", Plugin: "obfs-local;obfs=http", UDPOverTCP: 2}},
		{ID: 4, GroupID: 1, Type: TypeVMess, Name: "vm", Address: "v.io", Port: 443,
			Stream: &StreamSettings{Network: "ws", Path: "/ws", Security: "tls", SNI: "v.io", UTLSFingerprint: "chrome", Multiplex: 1},
			Detail: &VMessDetail{UUID: "uuid-1", AlterID: 0, Security: "auto"}},
		{ID: 5, GroupID: 1, Type: TypeVLESS, Name: "vl", Address: "vl.io", Port: 443,
			Stream: &StreamSettings{Network: "tcp", Security: "reality", RealityPublicKey: "pbk", RealityShortID: "sid", PacketEncoding: "xudp"},
			Detail: &TrojanVlessDetail{Password: "uuid-2", Flow: "xtls-rprx-vision"}},
		{ID: 6, GroupID: 1, Type: TypeTrojan, Name: "tj", Address: "t.io", Port: 443,
			Detail: &TrojanVlessDetail{Password: "pw"}},
		{ID: 7, GroupID: 1, Type: TypeNaive, Name: "nv", Address: "n.io", Port: 443,
			Detail: &NaiveDetail{Username: "u", Password: "p", Proto: "https", DisableLogs: true}},
		sampleProfile(),
		{ID: 8, GroupID: 1, Type: TypeTUIC, Name: "tu", Address: "tq.io", Port: 443,
			Detail: &TUICDetail{UUID: "u1", Password: "p", CongestionControl: "bbr", UDPRelayMode: "native", Heartbeat: "10s", UDPOverStream: true}},
		{ID: 9, GroupID: 1, Type: TypeCustomOutbound, Name: "co", Address: "c.io", Port: 1,
			Detail: &CustomDetail{Core: "internal", OutboundJSON: `{"type":"socks","server":"c.io"}`}},
		{ID: 10, GroupID: 1, Type: TypeCustomConfig, Name: "cc",
			Detail: &CustomDetail{Core: "internal-full", FullConfigJSON: `{"inbounds":[]}`}},
		{ID: 11, GroupID: 1, Type: TypeCustomExternal, Name: "ce",
			Detail: &CustomDetail{Core: "external", ExternalCoreName: "naive", Command: []string{"naive", "--proxy"}}},
		{ID: 12, GroupID: 1, Type: TypeChain, Name: "ch",
			Detail: &ChainDetail{ProfileIDs: []int64{4, 5, 6}}},
	}
	for _, p := range profiles {
		got := roundtrip(t, p)
		if got.ID != p.ID || got.Type != p.Type || got.Address != p.Address || got.Port != p.Port {
			t.Fatalf("common fields changed: %+v", got)
		}
		if got.Detail == nil {
			t.Fatalf("type %s: detail lost after roundtrip", p.Type)
		}
	}
}

func TestProfileDetailConcreteType(t *testing.T) {
	got := roundtrip(t, sampleProfile())
	d, ok := got.Detail.(*Hysteria2Detail)
	if !ok {
		t.Fatalf("detail type = %T, want *Hysteria2Detail", got.Detail)
	}
	// FR-UI-268/LNB-35: имена полей sing-box 1.14.
	if d.ServerPorts != "20000:30000" || d.HopInterval != "30s" {
		t.Fatalf("hy2 port-hopping fields lost: %+v", d)
	}
}

func TestProfileJSONKeyNames(t *testing.T) {
	// Канонические имена полей (в т.ч. hy2 — sing-box 1.14, LNB-35).
	b := mustJSON(t, sampleProfile())
	for _, key := range []string{`"server_ports"`, `"hop_interval"`, `"group_id"`, `"custom_fields_confirmed"`} {
		if !strings.Contains(b, key) {
			t.Fatalf("key %s missing in %s", key, b)
		}
	}
}

func TestProfileUnknownTypeRejected(t *testing.T) {
	in := `{"id":1,"type":"hysteria","detail":{}}`
	var p Profile
	if err := json.Unmarshal([]byte(in), &p); err == nil {
		t.Fatalf("unknown type must be rejected, got %+v", p)
	}
}

func TestProfileWithoutDetail(t *testing.T) {
	// Профиль без детали (например, свежесозданный) должен переживать roundtrip.
	got := roundtrip(t, &Profile{ID: 1, GroupID: 1, Type: TypeSOCKS, Name: "x", Address: "a", Port: 1})
	if got.Detail != nil {
		t.Fatalf("expected nil detail, got %T", got.Detail)
	}
}

func TestGroupRoundtrip(t *testing.T) {
	total := int64(1000)
	g := &Group{
		ID: 2, Name: "Sub", Kind: GroupSubscription,
		SubscriptionURL:   "https://sub.example.com/list",
		FrontProxyID:      nil,
		SubscriptionInfo:  &SubscriptionInfo{TotalBytes: &total, Title: "My sub"},
		LastUpdateMs:      1729700000000,
		UpdateIntervalMin: nil,
		UserAgent:         "LAVRay/1",
		HTTPHeaders:       map[string]string{"X-Key": "val"},
		ProfileOrder:      []int64{5, 6},
	}
	got := roundtripJSON(t, g, &Group{}).(*Group)
	if got.Kind != GroupSubscription || got.SubscriptionInfo == nil || got.SubscriptionInfo.TotalBytes == nil || *got.SubscriptionInfo.TotalBytes != 1000 {
		t.Fatalf("group roundtrip lost fields: %+v", got)
	}
}

func roundtripJSON(t *testing.T, in any, out any) any {
	t.Helper()
	b, err := json.Marshal(in)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if err := json.Unmarshal(b, out); err != nil {
		t.Fatalf("unmarshal %s: %v", b, err)
	}
	return out
}

func TestSettingDescriptors(t *testing.T) {
	ds := SettingDescriptors()
	if len(ds) < 56 {
		t.Fatalf("settings skeleton too small: %d descriptors (карта миграции §5.4 — 56 ключей)", len(ds))
	}
	seen := map[string]bool{}
	for _, d := range ds {
		if d.Key == "" || d.Type == "" || d.GroupID == "" {
			t.Fatalf("incomplete descriptor: %+v", d)
		}
		if seen[d.Key] {
			t.Fatalf("duplicate key %s", d.Key)
		}
		seen[d.Key] = true
		if d.DefaultValue != "" {
			var v any
			if err := json.Unmarshal([]byte(d.DefaultValue), &v); err != nil {
				t.Fatalf("default of %s not JSON: %v", d.Key, err)
			}
		}
	}
	// extraCore.core_map — DROP (FR-UI-175): ключа быть не должно.
	for _, d := range ds {
		if d.DeprecatedBy == "extraCore.core_map" {
			t.Fatalf("extraCore.core_map must not be migrated (FR-UI-175)")
		}
	}
}
