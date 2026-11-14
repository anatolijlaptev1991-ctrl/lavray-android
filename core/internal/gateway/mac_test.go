// SPDX-License-Identifier: GPL-3.0-only

package gateway

import (
	"errors"
	"strings"
	"testing"
)

// TestPolicyMACValidation — MAC проверяется строго: он попадает в текст правил
// nft, которые исполняются от root (I, S).
func TestPolicyMACValidation(t *testing.T) {
	good := map[string]string{
		"aa:bb:cc:dd:ee:00":    "aa:bb:cc:dd:ee:00",
		"AA:BB:CC:DD:EE:00":    "aa:bb:cc:dd:ee:00",
		"  aa-bb-cc-dd-ee-00 ": "aa:bb:cc:dd:ee:00",
		"02:00:00:00:00:01":    "02:00:00:00:00:01", // локально администрируемый, одиночный
	}
	for in, want := range good {
		if got, err := NormalizeMAC(in); err != nil || got != want {
			t.Errorf("NormalizeMAC(%q) = %q, %v; ждали %q", in, got, err, want)
		}
	}
	bad := []string{
		"", "aa:bb:cc:dd:ee", "aa:bb:cc:dd:ee:gg", "aabbccddeeff", "aa:bb:cc:dd:ee:00:11",
		"aa:bb:cc:dd:ee:00\nadd rule inet lavray prerouting accept", // инъекция в nft
		"aa:bb:cc:dd:ee:0\"", "aa;bb;cc;dd;ee;00", "aa:bb:cc:dd:ee:0é",
		"00:00:00:00:00:00", "ff:ff:ff:ff:ff:ff", // служебные
		"01:00:5e:00:00:01", "33:33:00:00:00:01", // групповые
	}
	for _, in := range bad {
		_, err := NormalizeMAC(in)
		if !errors.Is(err, ErrBadMAC) {
			t.Errorf("NormalizeMAC(%q): err=%v, ждали ErrBadMAC", in, err)
		}
		if err != nil && strings.ContainsAny(err.Error(), "\n\r") {
			t.Errorf("сообщение об ошибке для %q содержит перевод строки", in)
		}
	}
}
