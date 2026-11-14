// SPDX-License-Identifier: GPL-3.0-only

//go:build singbox_check

package inst

import (
	"context"
	"encoding/json"
	"os"
	"testing"

	box "github.com/sagernet/sing-box"
	"github.com/sagernet/sing-box/include"
	"github.com/sagernet/sing-box/option"
)

// TestSniffRuleOnly — минимальный конфиг: понимает ли sing-box 1.14
// правило «sniff» и «hijack-dns» в предложенном виде.
func TestSniffRuleOnly(t *testing.T) {
	raw, err := os.ReadFile("../../t1.json")
	if err != nil {
		t.Skip(err)
	}
	var opts option.Options
	if err := json.Unmarshal(raw, &opts); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	instance, err := box.New(box.Options{Context: include.Context(context.Background()), Options: opts})
	if err != nil {
		t.Fatalf("box.New: %v", err)
	}
	_ = instance.Close()
}
