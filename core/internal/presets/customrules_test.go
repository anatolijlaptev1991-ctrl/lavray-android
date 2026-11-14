// SPDX-License-Identifier: GPL-3.0-only

package presets

import (
	"strings"
	"testing"
)

// TestCustomRulesArrayAndObject (H): оба вида записи приводятся к массиву;
// пустые формы — к пустой строке.
func TestCustomRulesArrayAndObject(t *testing.T) {
	const want = `[{"domain":["a.example"],"outbound":"proxy"},{"port":[443],"outbound":"direct"}]`
	for _, in := range []string{
		want,
		` [ {"domain":["a.example"],"outbound":"proxy"} , {"port":[443],"outbound":"direct"} ] `,
		`{"rules":[{"domain":["a.example"],"outbound":"proxy"},{"port":[443],"outbound":"direct"}]}`,
		`{"rules":[{"domain":["a.example"],"outbound":"proxy"},{"port":[443],"outbound":"direct"}],"extra":1}`,
	} {
		got, err := NormalizeCustomRules(in)
		if err != nil || got != want {
			t.Errorf("Normalize(%q) = %q, %v; want %q", in, got, err, want)
		}
		rules, err := ParseCustomRules(in)
		if err != nil || len(rules) != 2 {
			t.Errorf("Parse(%q) = %v, %v", in, rules, err)
		}
	}
	for _, empty := range []string{"", "  ", "{}", "[]", `{"rules":[]}`, `{"rules":null}`, "null"} {
		if got, err := NormalizeCustomRules(empty); err != nil || got != "" {
			t.Errorf("Normalize(%q) = %q, %v; want пусто", empty, got, err)
		}
		if rules, err := ParseCustomRules(empty); err != nil || len(rules) != 0 {
			t.Errorf("Parse(%q) = %v, %v", empty, rules, err)
		}
	}
	// Повторная нормализация ничего не меняет.
	once, _ := NormalizeCustomRules(`{"rules":[{"a":1}]}`)
	if twice, _ := NormalizeCustomRules(once); twice != once {
		t.Errorf("не идемпотентно: %q → %q", once, twice)
	}
}

// TestCustomRulesNonObjectRejected (I): элементы-не-объекты и не JSON — ошибка с путём.
func TestCustomRulesNonObjectRejected(t *testing.T) {
	cases := map[string]string{
		`[1]`:                 "custom_rules[0]",
		`[{"a":1},"x"]`:       "custom_rules[1]",
		`[null]`:              "custom_rules[0]",
		`[[{"a":1}]]`:         "custom_rules[0]",
		`{"rules":[true]}`:    "custom_rules[0]",
		`{"rules":{"a":1}}`:   "rules",
		`"text"`:              "массив",
		`42`:                  "массив",
		`{not json`:           "custom_rules",
		`{"other":[{"a":1}]}`: "rules",
		`[{"a":1}] trailing`:  "custom_rules",
		`[{"a":1},]`:          "custom_rules",
	}
	for in, frag := range cases {
		if _, err := NormalizeCustomRules(in); err == nil || !strings.Contains(err.Error(), frag) {
			t.Errorf("Normalize(%q): err = %v, want содержит %q", in, err, frag)
		}
		if _, err := ParseCustomRules(in); err == nil {
			t.Errorf("Parse(%q): ожидалась ошибка", in)
		}
	}
}

// TestCustomRulesSizeCap (B): ровно 256 КиБ проходит, на байт больше — нет.
func TestCustomRulesSizeCap(t *testing.T) {
	build := func(size int) string {
		pad := size - len(`[{"d":""}]`)
		return `[{"d":"` + strings.Repeat("x", pad) + `"}]`
	}
	ok := build(MaxCustomRulesBytes)
	if len(ok) != MaxCustomRulesBytes {
		t.Fatalf("тест собрал %d байт", len(ok))
	}
	if _, err := NormalizeCustomRules(ok); err != nil {
		t.Fatalf("ровно на пределе: %v", err)
	}
	over := build(MaxCustomRulesBytes + 1)
	if _, err := NormalizeCustomRules(over); err == nil || !strings.Contains(err.Error(), "custom_rules") {
		t.Fatalf("на байт больше: err = %v", err)
	}
	if _, err := ParseCustomRules(over); err == nil {
		t.Fatal("Parse не проверяет размер")
	}
}

// Символы <, > и & не экранируются при нормализации: json.Marshal превращал бы
// их в \u003c… (6 байт вместо 1), и сохранённое значение переходило предел
// 256 КиБ, который повторно проверяет сборка конфига.
func TestCustomRulesHTMLCharsKeepSizeUnderCap(t *testing.T) {
	filler := func(n int) string { return strings.Repeat("<>&", n/3+1)[:n] }
	build := func(size int) string {
		return `[{"d":"` + filler(size-len(`[{"d":""}]`)) + `"}]`
	}
	atCap := build(MaxCustomRulesBytes)
	out, err := NormalizeCustomRules(atCap)
	if err != nil {
		t.Fatalf("ровно на пределе: %v", err)
	}
	if out != atCap || strings.Contains(out, `\u003c`) || len(out) > MaxCustomRulesBytes {
		t.Fatalf("значение исказилось: %d байт, экранирование=%v", len(out), strings.Contains(out, `\u00`))
	}
	if again, err := NormalizeCustomRules(out); err != nil || again != out {
		t.Fatalf("сохранённое значение не проходит повторную проверку: %v", err)
	}
	if rules, err := ParseCustomRules(out); err != nil || len(rules) != 1 {
		t.Fatalf("сборка конфига отвергла сохранённое значение: %v", err)
	}
	// Разреженная запись с пробелами сжимается, а не раздувается.
	spaced := "[ \n  {\"d\": \"<&>\"} ,\n  {\"e\": \">\"}\n]"
	if got, err := NormalizeCustomRules(spaced); err != nil || got != `[{"d":"<&>"},{"e":">"}]` {
		t.Fatalf("сжатие: %q %v", got, err)
	}
	// Вид {"rules":[…]} у предела: результат не длиннее входа.
	wrapped := `{"rules":` + build(MaxCustomRulesBytes-len(`{"rules":}`)) + `}`
	if got, err := NormalizeCustomRules(wrapped); err != nil || len(got) > MaxCustomRulesBytes {
		t.Fatalf("объектный вид: %d байт, %v", len(got), err)
	}
}
