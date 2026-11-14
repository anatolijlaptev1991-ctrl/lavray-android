// SPDX-License-Identifier: GPL-3.0-only

package model

import "testing"

// T-54 (B2-21): ложные срабатывания DetectGeo.
func TestDetectGeoFalsePositives(t *testing.T) {
	cases := []struct{ name, addr, want string }{
		// «таллин» → Эстония (ee), а не Эфиопия (et).
		{"Таллин", "", "ee"},
		{"таллин-2", "", "ee"},
		{"Tallinn", "", "ee"},
		// Теги протоколов между разделителями — не страны.
		{"VLESS-WS-CDN", "", ""},
		{"vless-ws-cdn", "cdn.example.com", ""},
		{"trojan_ss_tls", "", ""},
		{"vmess-cf-01", "", ""},
		{"[WS]", "", ""},
		{"xx-zz-1", "", ""}, // две буквы, не код ISO
		// Короткие алиасы — только целым словом.
		{"Fukuoka 01", "", ""}, // раньше «uk» → gb
		{"Busan KR", "", ""},   // раньше «usa» → us
		{"Bukhara", "", ""},    // раньше «uk» → gb
	}
	for _, c := range cases {
		if got := DetectGeo(c.name, c.addr); got != c.want {
			t.Errorf("DetectGeo(%q, %q) = %q, ожидалось %q", c.name, c.addr, got, c.want)
		}
	}
}

// Поведение, которое исправление сохранять обязано.
func TestDetectGeoKeepsWorkingCases(t *testing.T) {
	cases := []struct{ name, addr, want string }{
		{"[NL] Amsterdam", "", "nl"},
		{"(de) fast", "", "de"},
		{"vless-jp-01", "", "jp"},
		{"trojan_us_2", "", "us"},
		{"UK London", "", "gb"},
		{"uk-1", "", "gb"},
		{"[UK]", "", "gb"}, // «uk» — не код ISO, но ходовой алиас Великобритании
		{"USA 1", "", "us"},
		{"usa2", "", "us"},
		{"Tokyo01", "", "jp"},
		{"JapanTokyo", "", "jp"},
		{"Эстония", "", "ee"},
		{"🇩🇪 Frankfurt", "", "de"},
		{"node", "x.example.nl", "nl"},
		{"РФ Москва", "", "ru"},
		{"ОАЭ", "", "ae"},
	}
	for _, c := range cases {
		if got := DetectGeo(c.name, c.addr); got != c.want {
			t.Errorf("DetectGeo(%q, %q) = %q, ожидалось %q", c.name, c.addr, got, c.want)
		}
	}
}

// Таблица кодов не потеряла и не получила лишнего.
func TestISOCountryCodesTable(t *testing.T) {
	if len(isoCountryCodes) != 249 {
		t.Fatalf("в таблице %d кодов ISO 3166-1 alpha-2, ожидалось 249", len(isoCountryCodes))
	}
	for c := range isoCountryCodes {
		if !isAlpha2(c) {
			t.Errorf("некорректный код %q", c)
		}
	}
	for _, c := range []string{"ru", "us", "de", "jp", "nl", "gb", "ee", "ws", "ss", "cf"} {
		if !isoCountryCodes[c] {
			t.Errorf("нет кода %q", c)
		}
	}
	if isoCountryCodes["uk"] || isoCountryCodes["xx"] {
		t.Error("в таблице есть не-ISO код (uk, xx)")
	}
}
