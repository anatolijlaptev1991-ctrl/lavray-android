// SPDX-License-Identifier: GPL-3.0-only

package model

import (
	"errors"
	"strings"
	"testing"
)

func TestCanonicalJSON_SortsKeysStripsWhitespace(t *testing.T) {
	got, err := CanonicalJSON(" {\n  \"b\": [1, {\"z\": true, \"a\": null}],\n\t\"a\": \"x\" }\r\n")
	if err != nil {
		t.Fatal(err)
	}
	if want := `{"a":"x","b":[1,{"a":null,"z":true}]}`; got != want {
		t.Fatalf("got %s, want %s", got, want)
	}
}

func TestCanonicalJSON_PreservesBigNumbers(t *testing.T) {
	// Числа — текстом: 2^63 и 1e400 не теряют разрядов и не падают на float64.
	got, err := CanonicalJSON(`{"big":9223372036854775808,"huge":1e400,"neg":-0.5}`)
	if err != nil {
		t.Fatal(err)
	}
	if want := `{"big":9223372036854775808,"huge":1e400,"neg":-0.5}`; got != want {
		t.Fatalf("got %s, want %s", got, want)
	}
}

func TestCanonicalJSON_EmptyIsEmpty(t *testing.T) {
	for _, raw := range []string{"", "   ", "\n\t"} {
		got, err := CanonicalJSON(raw)
		if err != nil || got != "" {
			t.Errorf("CanonicalJSON(%q) = %q, %v; ждём пусто без ошибки", raw, got, err)
		}
	}
}

func TestCanonicalJSON_RejectsDuplicateKeys(t *testing.T) {
	for _, raw := range []string{
		`{"a":1,"a":2}`,
		`{"x":{"k":1,"k":1}}`,
		`[{"dns":{},"dns":{}}]`,
		`{"a":1,"a":2}`, // экранирование не прячет дубль
	} {
		if _, err := CanonicalJSON(raw); !errors.Is(err, ErrDuplicateKey) {
			t.Errorf("CanonicalJSON(%s): err = %v, ждём ErrDuplicateKey", raw, err)
		}
	}
}

func TestCanonicalJSON_RejectsTrailingGarbage(t *testing.T) {
	for _, raw := range []string{`{} x`, `{}{}`, `{"a":1}]`, `[1] 2`, `{"a":`, `{"a" 1}`, `nul`} {
		if _, err := CanonicalJSON(raw); err == nil {
			t.Errorf("CanonicalJSON(%q): ждём ошибку", raw)
		}
	}
}

func TestCanonicalJSON_UnicodeEscapesNormalized(t *testing.T) {
	// A и «A» — один документ; HTML-символы не экранируются.
	a, err := CanonicalJSON(`{"k":"A<&"}`)
	if err != nil {
		t.Fatal(err)
	}
	b, err := CanonicalJSON(`{"k":"A<&"}`)
	if err != nil {
		t.Fatal(err)
	}
	if a != b || a != `{"k":"A<&"}` {
		t.Fatalf("канон разошёлся: %s vs %s", a, b)
	}
	// Управляющие символы остаются экранированными (никогда не сырой 0x00).
	c, err := CanonicalJSON(`["\u0000\n"]`)
	if err != nil {
		t.Fatal(err)
	}
	if strings.ContainsRune(c, 0) || c != `["\u0000\n"]` {
		t.Fatalf("got %q", c)
	}
}

func TestCanonicalJSON_ScalarsAndDepth(t *testing.T) {
	for raw, want := range map[string]string{`true`: `true`, ` false `: `false`, `null`: `null`, `"s"`: `"s"`, `[]`: `[]`, `{}`: `{}`} {
		if got, err := CanonicalJSON(raw); err != nil || got != want {
			t.Errorf("CanonicalJSON(%q) = %q, %v", raw, got, err)
		}
	}
	deep := strings.Repeat("[", maxCanonDepth+2) + strings.Repeat("]", maxCanonDepth+2)
	if _, err := CanonicalJSON(deep); err == nil {
		t.Fatal("вложенность сверх maxCanonDepth должна отвергаться")
	}
}

func FuzzCanonicalJSON(f *testing.F) {
	for _, s := range []string{`{"b":1,"a":[true,null,"x"]}`, `[1e400]`, `{"a":{"a":{}}}`, `"é"`, `{"a":1,"a":2}`, ``} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, raw string) {
		c, err := CanonicalJSON(raw)
		if err != nil {
			return
		}
		again, err := CanonicalJSON(c)
		if err != nil || again != c {
			t.Fatalf("канон не идемпотентен: %q → %q → %q (%v)", raw, c, again, err)
		}
	})
}

func TestCanonicalJSON_TooLarge(t *testing.T) {
	// Граница ровно на MaxCanonBytes: столько — можно, на байт больше — нет.
	body := `{"a":"` + strings.Repeat("x", MaxCanonBytes-len(`{"a":""}`)) + `"}`
	if len(body) != MaxCanonBytes {
		t.Fatalf("длина %d", len(body))
	}
	if _, err := CanonicalJSON(body); err != nil {
		t.Fatalf("ровно MaxCanonBytes: %v", err)
	}
	if _, err := CanonicalJSON(body + " "); !errors.Is(err, ErrCanonTooLarge) {
		t.Fatalf("MaxCanonBytes+1: %v", err)
	}
}

func BenchmarkCanonicalJSONDeep(b *testing.B) {
	// Глубина × размер не перемножаются: вывод одним проходом.
	doc := strings.Repeat(`{"k":`, 500) + `"` + strings.Repeat("v", 200<<10) + `"` + strings.Repeat("}", 500)
	b.ResetTimer()
	for range b.N {
		if _, err := CanonicalJSON(doc); err != nil {
			b.Fatal(err)
		}
	}
}
