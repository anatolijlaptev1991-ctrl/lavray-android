// SPDX-License-Identifier: GPL-3.0-only

package model

import (
	"strings"
	"testing"
)

func mustHash(t *testing.T, p *Profile) string {
	t.Helper()
	h, err := CustomFieldsHash(p)
	if err != nil {
		t.Fatal(err)
	}
	return h
}

func internalFull(cfg string) *Profile {
	return &Profile{Type: TypeCustomConfig, Detail: &CustomDetail{Core: "internal-full", FullConfigJSON: cfg}}
}

func TestCustomFieldsHash_GoldenVector(t *testing.T) {
	// Формула фиксирована: смена — это новая версия customHashDomain, иначе
	// все подтверждения пользователей тихо разойдутся. Значение сверено
	// независимо: sha256(b"LAVCF1\x00" + out + b"\x00" + cfg) в Python.
	p := &Profile{CustomOutboundJSON: `{"tls":{"utls":{"enabled":true}}}`, CustomConfigJSON: `{"dns":{"final":"remote"}}`}
	if got, want := mustHash(t, p), "f532809093c68df49dce9a3530c94184fae4c5e93ceeda4029faa5e56e95137f"; got != want {
		t.Fatalf("hash = %s, want %s", got, want)
	}
}

func TestCustomFieldsHash_SegmentSeparation(t *testing.T) {
	x := `{"a":1}`
	a := mustHash(t, &Profile{CustomOutboundJSON: x})
	b := mustHash(t, &Profile{CustomConfigJSON: x})
	if a == b {
		t.Fatal("одинаковое содержимое в разных полях обязано давать разные хэши")
	}
}

func TestCustomFieldsHash_KeyOrderIrrelevant(t *testing.T) {
	a := mustHash(t, &Profile{CustomConfigJSON: `{"a":1,"b":{"c":2,"d":3}}`})
	b := mustHash(t, &Profile{CustomConfigJSON: "{ \"b\": {\"d\":3, \"c\":2},\n \"a\": 1 }"})
	if a != b {
		t.Fatal("порядок ключей и пробелы не меняют хэш")
	}
	if a == mustHash(t, &Profile{CustomConfigJSON: `{"a":1,"b":{"c":2,"d":4}}`}) {
		t.Fatal("другое значение обязано менять хэш")
	}
}

func TestCustomFieldsHash_InternalFullIncludesFullConfig(t *testing.T) {
	a := mustHash(t, internalFull(`{"outbounds":[{"type":"direct"}]}`))
	b := mustHash(t, internalFull(`{"outbounds":[{"type":"block"}]}`))
	if a == b {
		t.Fatal("полный конфиг internal-full входит в хэш")
	}
	// Не internal-full: FullConfigJSON к применяемому содержимому не относится.
	p := internalFull(`{"x":1}`)
	p.Detail.(*CustomDetail).Core = "internal"
	q := internalFull(`{"x":2}`)
	q.Detail.(*CustomDetail).Core = "internal"
	if mustHash(t, p) != mustHash(t, q) {
		t.Fatal("у core=internal полный конфиг в хэш не входит")
	}
	if mustHash(t, internalFull("")) == mustHash(t, &Profile{}) {
		t.Fatal("internal-full с пустым конфигом отличается от профиля без custom-полей")
	}
}

func TestCustomFieldsHash_InvalidJSON(t *testing.T) {
	for _, p := range []*Profile{
		{CustomOutboundJSON: `{"a":`},
		{CustomConfigJSON: `{"a":1,"a":2}`},
		internalFull(`{} trailing`),
	} {
		if _, err := CustomFieldsHash(p); err == nil {
			t.Errorf("ждём ошибку для %+v", p)
		}
	}
}

func TestHasCustomContent(t *testing.T) {
	cases := []struct {
		name string
		p    *Profile
		want bool
	}{
		{"пусто", &Profile{}, false},
		{"пробелы", &Profile{CustomConfigJSON: "  \n"}, false},
		{"пустой объект", &Profile{CustomOutboundJSON: "{ }", CustomConfigJSON: "{}"}, false},
		{"outbound", &Profile{CustomOutboundJSON: `{"a":1}`}, true},
		{"config", &Profile{CustomConfigJSON: `{"dns":{}}`}, true},
		{"невалидный JSON — тоже содержимое", &Profile{CustomConfigJSON: `{`}, true},
		{"internal-full", internalFull(""), true},
	}
	for _, c := range cases {
		if got := HasCustomContent(c.p); got != c.want {
			t.Errorf("%s: HasCustomContent = %v, want %v", c.name, got, c.want)
		}
	}
}

func TestCustomConfirmed(t *testing.T) {
	p := &Profile{CustomConfigJSON: `{"dns":{"final":"remote"}}`}
	if CustomConfirmed(p) {
		t.Fatal("без хэша не подтверждено")
	}
	p.CustomHash = mustHash(t, p)
	if !CustomConfirmed(p) {
		t.Fatal("хэш текущего содержимого — подтверждено")
	}
	p.CustomFieldsConfirmed = false // флаг модели ни на что не влияет
	p.CustomConfigJSON = `{"dns":{"final":"local"}}`
	if CustomConfirmed(p) {
		t.Fatal("правка содержимого снимает подтверждение")
	}
	empty := &Profile{CustomHash: mustHash(t, &Profile{})}
	if CustomConfirmed(empty) {
		t.Fatal("без custom-содержимого подтверждать нечего")
	}
}

func TestCustomFieldsHash_OverLimitUnconfirmable(t *testing.T) {
	p := &Profile{CustomConfigJSON: `{"route":{"x":"` + strings.Repeat("a", MaxCanonBytes) + `"}}`}
	if !HasCustomContent(p) {
		t.Fatal("слишком большой custom-JSON — всё равно содержимое")
	}
	if _, err := CustomFieldsHash(p); err == nil {
		t.Fatal("ждём ошибку для документа больше MaxCanonBytes")
	}
	p.CustomHash = strings.Repeat("a", 64)
	if CustomConfirmed(p) {
		t.Fatal("неканонизируемое содержимое не подтверждается")
	}
}
