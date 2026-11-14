// SPDX-License-Identifier: GPL-3.0-only

package textdiff

import (
	"fmt"
	"strconv"
	"strings"
	"testing"
)

// applyUnified применяет diff к a (проверка корректности вывода Unified).
func applyUnified(t testing.TB, a, diff string) string {
	t.Helper()
	if diff == "" {
		return a
	}
	src := splitLines(a)
	lines := splitLines(diff)
	if len(lines) < 2 || lines[0] != "--- before\n" || lines[1] != "+++ after\n" {
		t.Fatalf("нет заголовка diff: %q", diff)
	}
	var out []string
	pos := 0 // следующая неиспользованная строка src
	for i := 2; i < len(lines); {
		var aStart, aLen, bStart, bLen int
		if _, err := fmt.Sscanf(lines[i], "@@ -%d,%d +%d,%d @@\n", &aStart, &aLen, &bStart, &bLen); err != nil {
			t.Fatalf("строка %d не заголовок ханка: %q", i, lines[i])
		}
		i++
		from := aStart - 1
		if aLen == 0 {
			from = aStart
		}
		out = append(out, src[pos:from]...)
		pos = from
		for i < len(lines) && !strings.HasPrefix(lines[i], "@@ ") {
			kind, body := lines[i][0], lines[i][1:]
			i++
			if i < len(lines) && lines[i] == noNewline {
				body = strings.TrimSuffix(body, "\n")
				i++
			}
			switch kind {
			case ' ':
				if src[pos] != body {
					t.Fatalf("контекст не совпал: %q != %q", src[pos], body)
				}
				out = append(out, body)
				pos++
			case '-':
				if src[pos] != body {
					t.Fatalf("удаляемая строка не совпала: %q != %q", src[pos], body)
				}
				pos++
			case '+':
				out = append(out, body)
			default:
				t.Fatalf("неизвестная строка diff: %q", string(kind)+body)
			}
		}
	}
	return strings.Join(append(out, src[pos:]...), "")
}

func numbered(n int, mark func(int) string) string {
	var b strings.Builder
	for i := 1; i <= n; i++ {
		b.WriteString(mark(i))
		b.WriteByte('\n')
	}
	return b.String()
}

func TestUnifiedIdentical(t *testing.T) {
	for _, s := range []string{"", "a\n", "a\nb"} {
		if d, tr := Unified(s, s, 3, 0); d != "" || tr {
			t.Errorf("Unified(%q, %q) = %q, %v; ждём пустой diff", s, s, d, tr)
		}
	}
}

func TestUnifiedContextHunks(t *testing.T) {
	a := numbered(20, strconv.Itoa)
	b := numbered(20, func(i int) string {
		switch i {
		case 2:
			return "two"
		case 18:
			return "eighteen"
		}
		return strconv.Itoa(i)
	})
	d, tr := Unified(a, b, 2, 0)
	if tr {
		t.Fatal("не ждём обрезания")
	}
	want := "--- before\n+++ after\n" +
		"@@ -1,4 +1,4 @@\n 1\n-2\n+two\n 3\n 4\n" +
		"@@ -16,5 +16,5 @@\n 16\n 17\n-18\n+eighteen\n 19\n 20\n"
	if d != want {
		t.Fatalf("diff:\n%s\nждём:\n%s", d, want)
	}
	if got := applyUnified(t, a, d); got != b {
		t.Fatalf("патч дал %q", got)
	}
	// Правки ближе 2·ctx склеиваются в один ханк.
	if d, _ := Unified(a, numbered(20, func(i int) string {
		if i == 5 || i == 9 {
			return "x"
		}
		return strconv.Itoa(i)
	}), 2, 0); strings.Count(d, "\n@@ ") != 1 {
		t.Fatalf("ждём один ханк:\n%s", d)
	}
}

func TestUnifiedEmptySides(t *testing.T) {
	d, _ := Unified("", "x\ny\n", 3, 0)
	if !strings.Contains(d, "@@ -0,0 +1,2 @@\n+x\n+y\n") {
		t.Fatalf("вставка в пустой текст:\n%s", d)
	}
	d, _ = Unified("x\n", "", 3, 0)
	if !strings.Contains(d, "@@ -1,1 +0,0 @@\n-x\n") {
		t.Fatalf("удаление всего текста:\n%s", d)
	}
}

func TestUnifiedNoNewlineAtEOF(t *testing.T) {
	a, b := "a\nb", "a\nb\n"
	d, _ := Unified(a, b, 3, 0)
	if !strings.Contains(d, "-b\n"+noNewline+"+b\n") {
		t.Fatalf("маркер отсутствующего перевода строки:\n%s", d)
	}
	if got := applyUnified(t, a, d); got != b {
		t.Fatalf("патч дал %q", got)
	}
}

func TestUnifiedCapped(t *testing.T) {
	a := numbered(500, strconv.Itoa)
	b := numbered(500, func(i int) string { return "x" + strconv.Itoa(i) })
	d, tr := Unified(a, b, 3, 100)
	if !tr {
		t.Fatal("ждём truncated")
	}
	if n := strings.Count(d, "\n"); n != 100 {
		t.Fatalf("строк вывода %d, ждём ровно 100", n)
	}
	if _, tr := Unified(a, b, 3, 0); tr {
		t.Fatal("maxLines ≤ 0 — без предела")
	}
}

func TestUnifiedFallbackBeyondMaxEdits(t *testing.T) {
	// Больше maxEdits правок: середина — «всё удалено, всё добавлено», но патч верен.
	a := numbered(maxEdits, func(i int) string { return "a" + strconv.Itoa(i) })
	b := numbered(maxEdits, func(i int) string { return "b" + strconv.Itoa(i) })
	d, _ := Unified("head\n"+a+"tail\n", "head\n"+b+"tail\n", 1, 0)
	if got := applyUnified(t, "head\n"+a+"tail\n", d); got != "head\n"+b+"tail\n" {
		t.Fatal("патч при отказе от Майерса неверен")
	}
}

func TestMyersIsMinimal(t *testing.T) {
	ops := diffLines(splitLines("a\nb\nc\nd\n"), splitLines("a\nx\nc\nd\ne\n"))
	edits := 0
	for _, o := range ops {
		if o.kind != opEqual {
			edits++
		}
	}
	if edits != 3 { // -b +x +e
		t.Fatalf("правок %d, ждём 3: %+v", edits, ops)
	}
}

func FuzzUnified(f *testing.F) {
	f.Add("a\nb\nc\n", "a\nc\nd\n", 3)
	f.Add("", "x", 0)
	f.Add("x\ny", "x\ny\n", 1)
	f.Add("{\n  \"a\": 1\n}\n", "{\n  \"a\": 2,\n  \"b\": [1]\n}", 2)
	f.Fuzz(func(t *testing.T, a, b string, ctx int) {
		ctx %= 8
		d, tr := Unified(a, b, ctx, 0)
		if tr {
			t.Fatal("без предела вывод не обрезается")
		}
		if (d == "") != (a == b) {
			t.Fatalf("пустой diff ⇔ равные тексты: a=%q b=%q d=%q", a, b, d)
		}
		if got := applyUnified(t, a, d); got != b {
			t.Fatalf("патч к a не дал b:\na=%q\nb=%q\nd=%q\ngot=%q", a, b, d, got)
		}
	})
}
