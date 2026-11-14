// SPDX-License-Identifier: GPL-3.0-only

package parser

import (
	"errors"
	"fmt"
	"runtime"
	"strings"
	"testing"
)

// t50ClashYAML — валидный Clash YAML не короче minBytes (формат замера A1-14).
func t50ClashYAML(minBytes int) string {
	var sb strings.Builder
	sb.WriteString("proxies:\n")
	for i := 0; sb.Len() < minBytes; i++ {
		fmt.Fprintf(&sb, "  - {name: n%d, type: ss, server: 1.2.3.4, port: 8388, cipher: aes-128-gcm, password: pw%d}\n", i, i)
	}
	return sb.String()
}

// T-50 (A1-14): Clash YAML сверх лимита отвергается ДО разбора. Раньше 60 МиБ
// давали пик 2,2 ГиБ ОЗУ; здесь 9 МиБ дали бы около 300 МиБ.
func TestParseClashTooLargeIsRejectedCheaply(t *testing.T) {
	big := t50ClashYAML(MaxClashYAMLBytes + 1<<20)
	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)
	res, err := Parse([]byte(big), Options{})
	runtime.ReadMemStats(&after)

	if res != nil || err == nil {
		t.Fatalf("YAML %d МиБ принят: res=%v err=%v", len(big)>>20, res != nil, err)
	}
	var pe *Error
	if !errors.As(err, &pe) || pe.Msg != ErrTooLargeMsg {
		t.Fatalf("ожидалась *Error с Msg=ErrTooLargeMsg (api.mapErr → RESOURCE_EXHAUSTED), получено: %v", err)
	}
	if !strings.Contains(err.Error(), "МиБ") {
		t.Errorf("в тексте ошибки нет размера и лимита: %v", err)
	}
	if spent := (after.TotalAlloc - before.TotalAlloc) >> 20; spent > 64 {
		t.Fatalf("отказ обошёлся в %d МиБ выделений памяти, ожидалось не более 64", spent)
	}
}

// Лимит не мешает обычным подпискам: ≈ 0,5 МиБ (5–6 тысяч серверов) разбираются.
func TestParseClashBelowLimitStillParses(t *testing.T) {
	yaml := t50ClashYAML(512 << 10)
	res, err := Parse([]byte(yaml), Options{})
	if err != nil {
		t.Fatalf("YAML %d КиБ отвергнут: %v", len(yaml)>>10, err)
	}
	if res.Format != FormatClash || len(res.Profiles) < 1000 || len(res.Warnings) != 0 {
		t.Fatalf("format=%s profiles=%d warnings=%v", res.Format, len(res.Profiles), res.Warnings)
	}
}
