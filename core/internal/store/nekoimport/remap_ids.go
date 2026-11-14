// SPDX-License-Identifier: GPL-3.0-only

package nekoimport

// remap_ids.go — выдача id при переносе. Id NekoBox сохраняются, пока свободны;
// занятый id получает новый — выше всего, что выдавалось таблице и что есть в
// самом каталоге, поэтому новый id не совпадёт ни с чужим, ни с id, который
// сохранит другая запись этого же переноса.

type idAlloc struct {
	taken  map[int64]bool
	next   int64
	floor  int64
	strict bool
}

// newIDAlloc: existing — занятые id, floor — наибольший выданный таблице id,
// plan — id записей каталога, strict — id ≤ floor нельзя использовать повторно
// (кроме свежей таблицы: ни строк, ни выданных id).
func newIDAlloc(existing map[int64]bool, floor int64, plan []int64, strict bool) *idAlloc {
	a := &idAlloc{
		taken: make(map[int64]bool, len(existing)+len(plan)), next: floor, floor: floor,
		strict: strict && (len(existing) > 0 || floor > 0),
	}
	for id := range existing {
		a.taken[id] = true
	}
	for _, id := range plan {
		if id > a.next {
			a.next = id
		}
	}
	return a
}

// assign выдаёт id записи: свой, если свободен и forceNew не задан, иначе новый.
// Второе значение — id сменился.
func (a *idAlloc) assign(want int64, forceNew bool) (int64, bool) {
	if !forceNew && want >= 0 && !a.taken[want] && !(a.strict && want <= a.floor) {
		a.taken[want] = true
		return want, false
	}
	for {
		a.next++
		if !a.taken[a.next] {
			a.taken[a.next] = true
			return a.next, !forceNew
		}
	}
}

func groupIDsOf(gs []PlanGroup) []int64 {
	out := make([]int64, 0, len(gs))
	for _, g := range gs {
		out = append(out, g.NekoID)
	}
	return out
}

func profileIDsOf(ps []PlanProfile) []int64 {
	out := make([]int64, 0, len(ps))
	for _, p := range ps {
		out = append(out, p.NekoID)
	}
	return out
}
