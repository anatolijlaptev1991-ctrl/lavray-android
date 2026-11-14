// SPDX-License-Identifier: GPL-3.0-only

package parser

import (
	"testing"
)

const clashSample = `
port: 7890
socks-port: 7891
proxies:
  - name: "🇯🇵 JP Tokyo"
    type: vmess
    server: jp.example.com
    port: 443
    uuid: 11111111-1111-1111-1111-111111111111
    alterId: 0
    cipher: auto
    tls: true
    network: ws
    ws-opts:
      path: /ws
      headers:
        Host: cdn.example.com
    sni: jp.example.com
  - name: "NL Amsterdam"
    type: trojan
    server: nl.example.com
    port: 8443
    password: secret
    sni: nl.example.com
    skip-cert-verify: true
  - name: "SS node"
    type: ss
    server: ss.example.com
    port: 8388
    cipher: aes-256-gcm
    password: pass
  - name: "HY2"
    type: hysteria2
    server: hy.example.com
    port: 443
    password: hypass
    sni: hy.example.com
    up: 100
    down: 500
  - name: "REALITY"
    type: vless
    server: r.example.com
    port: 443
    uuid: 22222222-2222-2222-2222-222222222222
    network: tcp
    tls: true
    servername: www.microsoft.com
    reality-opts:
      public-key: PUBKEY123
      short-id: abcd
    client-fingerprint: chrome
proxy-groups:
  - name: auto
    type: url-test
    proxies: ["🇯🇵 JP Tokyo", "NL Amsterdam"]
rules:
  - MATCH,auto
`

func TestParseClash(t *testing.T) {
	res, err := Parse([]byte(clashSample), Options{})
	if err != nil {
		t.Fatalf("clash parse: %v", err)
	}
	if res.Format != FormatClash {
		t.Fatalf("format = %q", res.Format)
	}
	if len(res.Profiles) != 5 {
		t.Fatalf("profiles = %d, want 5", len(res.Profiles))
	}
	byName := map[string]int{}
	for i, p := range res.Profiles {
		byName[p.Name] = i
	}
	vm := res.Profiles[byName["🇯🇵 JP Tokyo"]]
	if vm.Type != "vmess" || vm.Stream == nil || vm.Stream.Network != "ws" || vm.Stream.Path != "/ws" {
		t.Fatalf("vmess ws: %+v stream=%+v", vm, vm.Stream)
	}
	if vm.Stream.Host != "cdn.example.com" {
		t.Fatalf("ws host = %q", vm.Stream.Host)
	}
	hy := res.Profiles[byName["HY2"]]
	if hy.Type != "hysteria2" {
		t.Fatalf("hy2 type = %v", hy.Type)
	}
	rl := res.Profiles[byName["REALITY"]]
	if rl.Stream == nil || rl.Stream.Security != "reality" || rl.Stream.RealityPublicKey != "PUBKEY123" {
		t.Fatalf("reality: %+v", rl.Stream)
	}
	if rl.Stream.UTLSFingerprint != "chrome" {
		t.Fatalf("utls = %q", rl.Stream.UTLSFingerprint)
	}
	tr := res.Profiles[byName["NL Amsterdam"]]
	if tr.Stream == nil || !tr.Stream.AllowInsecure {
		t.Fatalf("trojan skip-cert: %+v", tr.Stream)
	}
}
