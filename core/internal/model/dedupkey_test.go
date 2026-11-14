// SPDX-License-Identifier: GPL-3.0-only

package model

import "testing"

func trojanProfile() *Profile {
	return &Profile{
		ID: 1, GroupID: 1, Type: TypeTrojan, Name: "a", Address: "srv.example.org", Port: 443,
		Detail: &TrojanVlessDetail{Password: "secret"},
		Stream: &StreamSettings{Security: "tls", SNI: "srv.example.org"},
	}
}

// Имя, заметки, теги, избранное, страна, результат теста и трафик в ключ не входят.
func TestDedupKeyIgnoresMetadata(t *testing.T) {
	a := trojanProfile()
	b := trojanProfile()
	b.ID, b.GroupID, b.Name = 77, 9, "другое имя"
	b.Notes, b.Tags, b.Favorite, b.Geo, b.Manual = "заметка", []string{"x"}, true, "nl", true
	lat := int32(120)
	b.TestResult = TestResult{LatencyMs: &lat, LastError: "boom"}
	b.Traffic = TrafficCounters{UplinkBytes: 5, DownlinkBytes: 6}
	b.CreatedAtMs, b.UpdatedAtMs = 1, 2
	if DedupKey(a) != DedupKey(b) {
		t.Fatal("метаданные изменили ключ")
	}
}

// Адрес сравнивается без учёта регистра и крайних пробелов.
func TestDedupKeyNormalizesAddress(t *testing.T) {
	a := trojanProfile()
	b := trojanProfile()
	b.Address = "  SRV.Example.ORG "
	if DedupKey(a) != DedupKey(b) {
		t.Fatal("регистр или пробелы адреса считаются различием")
	}
}

// Любое различие, меняющее конфиг, даёт другой ключ.
func TestDedupKeyDiffersOnConfig(t *testing.T) {
	base := DedupKey(trojanProfile())
	cases := map[string]func(*Profile){
		"пароль":    func(p *Profile) { p.Detail = &TrojanVlessDetail{Password: "other"} },
		"порт":      func(p *Profile) { p.Port = 8443 },
		"адрес":     func(p *Profile) { p.Address = "other.example.org" },
		"тип":       func(p *Profile) { p.Type = TypeVLESS },
		"sni":       func(p *Profile) { p.Stream = &StreamSettings{Security: "tls", SNI: "x.org"} },
		"транспорт": func(p *Profile) { p.Stream = &StreamSettings{Security: "tls", SNI: "srv.example.org", Network: "ws"} },
		"custom":    func(p *Profile) { p.CustomOutboundJSON = `{"a":1}` },
	}
	for name, mutate := range cases {
		p := trojanProfile()
		mutate(p)
		if DedupKey(p) == base {
			t.Errorf("%s: ключ не изменился", name)
		}
	}
}

// Цепочки с теми же звеньями в том же порядке равны, в другом порядке — нет.
func TestDedupKeyChainOrder(t *testing.T) {
	chain := func(ids ...int64) *Profile {
		return &Profile{Type: TypeChain, Name: "c", Detail: &ChainDetail{ProfileIDs: ids}}
	}
	if DedupKey(chain(1, 2)) != DedupKey(chain(1, 2)) {
		t.Fatal("одинаковые цепочки различаются")
	}
	if DedupKey(chain(1, 2)) == DedupKey(chain(2, 1)) {
		t.Fatal("порядок звеньев не учитывается")
	}
}

// Ключ не раскрывает секреты: это хэш, а не склейка полей.
func TestDedupKeyIsOpaque(t *testing.T) {
	key := DedupKey(trojanProfile())
	if len(key) != 64 {
		t.Fatalf("ключ %q — не sha256 hex", key)
	}
}
