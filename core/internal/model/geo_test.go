// SPDX-License-Identifier: GPL-3.0-only

// geo_test.go — определение страны по имени/адресу (группировка по странам).
package model

import "testing"

func TestDetectGeo(t *testing.T) {
	cases := []struct {
		name, addr, want string
	}{
		{"🇯🇵 Токио", "jp.example.com", "jp"},
		{"🇳🇱 Amsterdam", "nl.example.com", "nl"},
		{"[NL] fast-1", "x.com", "nl"},
		{"(DE) Frankfurt", "x.com", "de"},
		{"_us_ west", "x.com", "us"},
		{"Токио — основной", "tokyo.example", "jp"},
		{"Нидерланды резерв", "x.com", "nl"},
		{"Франкфурт 2", "x.com", "de"},
		{"США восток", "x.com", "us"},
		{"Фины", "x.com", ""}, // «фины» не алиас (только «финляндия»)
		{"Finland edge", "x.com", "fi"},
		{"New Zealand", "x.com", "nz"},          // длинный алиас важнее короткого
		{"server-1", "node.ru.example.org", ""}, // TLD=org: страна не выводится
		{"server-2", "example.com", ""},
		{"server-3", "site.io", ""}, // служебные TLD не страна
		{"Нью-Йорк 01", "x.com", "us"},
		{"Armenia", "x.com", "am"},
		{"", "", ""},
	}
	for _, c := range cases {
		if got := DetectGeo(c.name, c.addr); got != c.want {
			t.Errorf("DetectGeo(%q, %q) = %q, want %q", c.name, c.addr, got, c.want)
		}
	}
}

func TestFlagToISO(t *testing.T) {
	if code, ok := flagToISO("🇩🇪 Германия"); !ok || code != "de" {
		t.Fatalf("flag: %q %v", code, ok)
	}
	if _, ok := flagToISO("no flag"); ok {
		t.Fatal("ложный флаг")
	}
}
