// SPDX-License-Identifier: GPL-3.0-only

// Package textdiff — построчный unified diff без внешних зависимостей
// (T-08B: diff конфига без custom-полей и с ними).
package textdiff

// myers.go — кратчайший сценарий правки (алгоритм Майерса, O((N+M)·D)).
// Общие начало и конец отрезаются заранее; если правок больше maxEdits,
// середина выдаётся как «всё удалено, всё добавлено» — diff остаётся
// корректным, только не минимальным.

// opKind — вид строки сценария: ' ' без изменений, '-' удалена, '+' добавлена.
type opKind byte

const (
	opEqual  opKind = ' '
	opDelete opKind = '-'
	opInsert opKind = '+'
)

// op — одна строка сценария (с её переводом строки, если он был).
type op struct {
	kind opKind
	line string
}

// maxEdits — предел D для Майерса: память трассы ~D² чисел.
const maxEdits = 1000

// diffLines — сценарий правки a → b.
func diffLines(a, b []string) []op {
	pre := 0
	for pre < len(a) && pre < len(b) && a[pre] == b[pre] {
		pre++
	}
	suf := 0
	for suf < len(a)-pre && suf < len(b)-pre && a[len(a)-1-suf] == b[len(b)-1-suf] {
		suf++
	}
	ops := make([]op, 0, len(a)+len(b))
	for _, l := range a[:pre] {
		ops = append(ops, op{opEqual, l})
	}
	midA, midB := a[pre:len(a)-suf], b[pre:len(b)-suf]
	if mid, ok := myers(midA, midB); ok {
		ops = append(ops, mid...)
	} else {
		for _, l := range midA {
			ops = append(ops, op{opDelete, l})
		}
		for _, l := range midB {
			ops = append(ops, op{opInsert, l})
		}
	}
	for _, l := range a[len(a)-suf:] {
		ops = append(ops, op{opEqual, l})
	}
	return ops
}

// myers — минимальный сценарий или false, если правок больше maxEdits.
// trace[d] хранит x для диагоналей k ∈ [-d-1, d+1] (индекс k+d+1) до шага d.
func myers(a, b []string) ([]op, bool) {
	n, m := len(a), len(b)
	limit := min(n+m, maxEdits)
	off := limit + 1
	v := make([]int, 2*off+1)
	trace := make([][]int, 0, 16)
	for d := 0; d <= limit; d++ {
		trace = append(trace, append([]int(nil), v[off-d-1:off+d+2]...))
		for k := -d; k <= d; k += 2 {
			x := v[off+k-1] + 1 // шаг вправо: удаление из a
			if k == -d || (k != d && v[off+k-1] < v[off+k+1]) {
				x = v[off+k+1] // шаг вниз: вставка из b
			}
			y := x - k
			for x < n && y < m && a[x] == b[y] {
				x++
				y++
			}
			v[off+k] = x
			if x >= n && y >= m {
				return backtrack(trace, a, b), true
			}
		}
	}
	return nil, false
}

// backtrack восстанавливает сценарий по трассе, от конца к началу.
func backtrack(trace [][]int, a, b []string) []op {
	x, y := len(a), len(b)
	var rev []op
	for d := len(trace) - 1; d >= 0; d-- {
		v := trace[d]
		at := func(k int) int { return v[k+d+1] }
		k := x - y
		prevK := k - 1
		if k == -d || (k != d && at(k-1) < at(k+1)) {
			prevK = k + 1
		}
		prevX := at(prevK)
		prevY := prevX - prevK
		for x > prevX && y > prevY {
			rev = append(rev, op{opEqual, a[x-1]})
			x--
			y--
		}
		if d > 0 {
			if x == prevX {
				rev = append(rev, op{opInsert, b[y-1]})
			} else {
				rev = append(rev, op{opDelete, a[x-1]})
			}
		}
		x, y = prevX, prevY
	}
	for i, j := 0, len(rev)-1; i < j; i, j = i+1, j-1 {
		rev[i], rev[j] = rev[j], rev[i]
	}
	return rev
}
