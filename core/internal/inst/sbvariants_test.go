// SPDX-License-Identifier: GPL-3.0-only

//go:build singbox_check

package inst

import (
	"context"
	"fmt"
	"testing"

	box "github.com/sagernet/sing-box"
	"github.com/sagernet/sing-box/include"
	"github.com/sagernet/sing-box/option"
	sbjson "github.com/sagernet/sing/common/json"
)

func tryRules(t *testing.T, name string, rules []map[string]any) {
	t.Helper()
	cfg := map[string]any{
		"log":       map[string]any{"level": "info"},
		"inbounds":  []any{map[string]any{"type": "mixed", "tag": "mixed-in", "listen": "127.0.0.1", "listen_port": 18082}},
		"outbounds": []any{map[string]any{"type": "direct", "tag": "direct"}},
		"route":     map[string]any{"rules": rules, "final": "direct"},
		"dns":       map[string]any{"servers": []any{map[string]any{"tag": "dns-direct", "type": "local"}}},
	}
	raw, _ := sbjson.Marshal(cfg)
	var opts option.Options
	if err := sbjson.UnmarshalContext(include.Context(context.Background()), raw, &opts); err != nil {
		t.Logf("%-28s unmarshal: %v", name, err)
		return
	}
	inst, err := box.New(box.Options{Context: include.Context(context.Background()), Options: opts})
	if err != nil {
		fmt.Printf("%-28s FAIL: %v\n", name, err)
		return
	}
	_ = inst.Close()
	fmt.Printf("%-28s OK\n", name)
}

func TestRuleVariants(t *testing.T) {
	tryRules(t, "sniff+inbound", []map[string]any{{"action": "sniff", "inbound": []any{"mixed-in"}, "timeout": "3s"}})
	tryRules(t, "sniff only", []map[string]any{{"action": "sniff"}})
	tryRules(t, "sniff+sniffer", []map[string]any{{"action": "sniff", "sniffer": []any{"http", "tls"}}})
	tryRules(t, "hijack-dns+protocol", []map[string]any{{"protocol": "dns", "action": "hijack-dns"}})
	tryRules(t, "hijack-dns only", []map[string]any{{"action": "hijack-dns"}})
	tryRules(t, "route outbound", []map[string]any{{"protocol": "dns", "outbound": "direct"}})
	tryRules(t, "process_name", []map[string]any{{"process_name": []any{"steam.exe"}, "outbound": "direct"}})
	tryRules(t, "ip_cidr", []map[string]any{{"ip_cidr": []any{"224.0.0.0/3"}, "outbound": "direct"}})
	tryRules(t, "rule_set", []map[string]any{{"rule_set": []any{"missing-set"}, "outbound": "direct"}})
}
