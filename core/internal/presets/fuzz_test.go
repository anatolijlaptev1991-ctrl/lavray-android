// SPDX-License-Identifier: GPL-3.0-only

package presets

import (
	"encoding/json"
	"testing"
)

// FuzzConvertNekoRayRoute — инвариант (core-port-plan §4.3): конвертер не
// паникует на любом RoutingBean; успешный результат проходит валидацию схемы.
func FuzzConvertNekoRayRoute(f *testing.F) {
	f.Add(`{"custom":"{\"rules\":[]}","def_outbound":"proxy","sniffing_mode":1}`)
	f.Add(`{"custom":"not-json"}`)
	f.Add(`{"custom":"{\"rules\":[{\"process_name\":\"a.exe\",\"outbound\":\"bypass\"}]}"}`)
	f.Add(`{"proxy_domain":"geosite:google\ndomain:x","direct_ip":"geoip:ru","sniffing_mode":2}`)
	f.Add(`{}`)
	f.Add(`{"def_outbound":"weird","outbound_domain_strategy":"PreferIPv4","dns_routing":true,"use_dns_object":true,"dns_object":"{\"servers\":[]}"}`)
	f.Fuzz(func(t *testing.T, raw string) {
		var src NekoRayRoute
		if err := json.Unmarshal([]byte(raw), &src); err != nil {
			return // не RoutingBean — не наш вход
		}
		meta := PresetMeta{Schema: "preset.schema.v1.json", Version: 1, Revision: 1,
			ID: "fuzz", Name: Localized{"en": "F"}}
		p, _, err := ConvertNekoRayRoute(&src, ConvertOptions{Meta: meta, AddAdsBlocklist: true})
		if err != nil {
			return // структурная ошибка — норма
		}
		if err := p.Validate(); err != nil {
			t.Fatalf("успешная конвертация даёт невалидную схему: %v (source: %s)", err, raw)
		}
	})
}

// FuzzNormalizeCustomRules: нет паник; принятая запись нормализуется в массив
// объектов, а повторная нормализация и разбор для сборки её не меняют.
func FuzzNormalizeCustomRules(f *testing.F) {
	for _, s := range []string{"", "[]", "{}", `[{"a":1}]`, `{"rules":[{"a":1}]}`, `[1]`, `{"rules":5}`, "null", `{"rules":[{"a":[{"b":null}]}]}`, "\x00", "[{"} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, raw string) {
		out, err := NormalizeCustomRules(raw)
		if err != nil {
			if _, perr := ParseCustomRules(raw); perr == nil {
				t.Fatalf("Normalize отклонил %q, а Parse принял", raw)
			}
			return
		}
		if again, err := NormalizeCustomRules(out); err != nil || again != out {
			t.Fatalf("не идемпотентно: %q → %q (%v)", out, again, err)
		}
		if rules, err := ParseCustomRules(out); err != nil || (out == "") != (len(rules) == 0) {
			t.Fatalf("Parse(%q) = %v, %v", out, rules, err)
		}
	})
}

// FuzzDetectAndParse: нет паник на любом файле импорта; успешный разбор даёт
// набор, который PrepareUser либо принимает, либо отклоняет ошибкой.
func FuzzDetectAndParse(f *testing.F) {
	f.Add([]byte(`{"routing":{"def_outbound":"proxy"},"version":1,"id":"x","name":{"en":"X"},"dns":{"mode":"simple"}}`))
	f.Add([]byte(`{"direct_domain":"a
b","def_outbound":"direct"}`))
	f.Add([]byte(`{"routing":null}`))
	f.Add([]byte(`[]`))
	f.Add([]byte(``))
	f.Fuzz(func(t *testing.T, data []byte) {
		p, _, err := DetectAndParse(data, "n")
		if err != nil {
			return
		}
		if p == nil {
			t.Fatal("nil без ошибки")
		}
		_ = p.PrepareUser()
	})
}
