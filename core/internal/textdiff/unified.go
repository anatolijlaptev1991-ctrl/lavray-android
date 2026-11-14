// SPDX-License-Identifier: GPL-3.0-only

package textdiff

// unified.go — вывод сценария в формате unified diff (как `diff -u`):
// заголовок «--- before / +++ after», ханки «@@ -a,n +b,m @@» с ctx строками
// контекста. Строка без перевода строки в конце файла помечается
// «\ No newline at end of file».

import (
	"fmt"
	"strings"
)

// noNewline — маркер последней строки без перевода строки.
const noNewline = "\\ No newline at end of file\n"

// Unified возвращает diff a → b (пусто, если тексты равны). Вывод не длиннее
// maxLines строк (maxLines ≤ 0 — без предела); truncated — вывод обрезан.
func Unified(a, b string, ctx, maxLines int) (diff string, truncated bool) {
	if a == b {
		return "", false
	}
	ctx = max(ctx, 0)
	ops := diffLines(splitLines(a), splitLines(b))
	w := &writer{limit: maxLines}
	w.line("--- before\n")
	w.line("+++ after\n")
	for _, h := range hunks(ops, ctx) {
		if !w.hunk(ops, h) {
			break
		}
	}
	return w.b.String(), w.truncated
}

// splitLines режет текст на строки, оставляя у каждой её "\n".
func splitLines(s string) []string {
	if s == "" {
		return nil
	}
	lines := strings.SplitAfter(s, "\n")
	if lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	return lines
}

// span — ханк: полуинтервал индексов сценария [from, to).
type span struct{ from, to int }

// hunks группирует правки: соседние, между которыми не больше 2·ctx
// неизменных строк, попадают в один ханк.
func hunks(ops []op, ctx int) []span {
	var out []span
	for i := 0; i < len(ops); i++ {
		if ops[i].kind == opEqual {
			continue
		}
		from, last := max(i-ctx, 0), i
		for j := i + 1; j < len(ops) && j <= last+2*ctx+1; j++ {
			if ops[j].kind != opEqual {
				last = j
			}
		}
		to := min(last+ctx+1, len(ops))
		if n := len(out); n > 0 && from <= out[n-1].to {
			out[n-1].to = to
		} else {
			out = append(out, span{from, to})
		}
		i = last
	}
	return out
}

// writer копит вывод и следит за пределом строк.
type writer struct {
	b         strings.Builder
	limit     int
	lines     int
	truncated bool
}

// line пишет одну строку вывода; false — предел достигнут.
func (w *writer) line(s string) bool {
	if w.limit > 0 && w.lines >= w.limit {
		w.truncated = true
		return false
	}
	w.b.WriteString(s)
	w.lines++
	return true
}

// hunk пишет заголовок и строки ханка; false — вывод обрезан.
func (w *writer) hunk(ops []op, h span) bool {
	aStart, bStart := 0, 0
	for _, o := range ops[:h.from] {
		if o.kind != opInsert {
			aStart++
		}
		if o.kind != opDelete {
			bStart++
		}
	}
	aLen, bLen := 0, 0
	for _, o := range ops[h.from:h.to] {
		if o.kind != opInsert {
			aLen++
		}
		if o.kind != opDelete {
			bLen++
		}
	}
	if !w.line(fmt.Sprintf("@@ -%s +%s @@\n", rangeOf(aStart, aLen), rangeOf(bStart, bLen))) {
		return false
	}
	for _, o := range ops[h.from:h.to] {
		text := string(o.kind) + o.line
		if !strings.HasSuffix(o.line, "\n") {
			if !w.line(text + "\n") {
				return false
			}
			text = noNewline
		}
		if !w.line(text) {
			return false
		}
	}
	return true
}

// rangeOf — «старт,длина» ханка; у пустого диапазона старт — строка перед ним.
func rangeOf(start, n int) string {
	if n == 0 {
		return fmt.Sprintf("%d,0", start)
	}
	return fmt.Sprintf("%d,%d", start+1, n)
}
