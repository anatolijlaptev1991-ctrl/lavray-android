// SPDX-License-Identifier: GPL-3.0-only

package api

// profile_dedup_plan.go — план удаления дубликатов (хвосты A): чистая функция
// над списками профилей и групп, ничего не читает и не пишет. Пробный прогон и
// применение берут один и тот же план.

import (
	"github.com/anatolijlaptev1991-ctrl/lavray-android/core/internal/model"
)

// dedupPlan — что сделает DeduplicateProfiles.
type dedupPlan struct {
	removed []*model.Profile // удаляемые копии, в порядке группы
	chains  []*model.Profile // цепочки с переписанными ссылками (новые копии)
	groups  []*model.Group   // группы с переписанным front proxy (новые копии)
}

// replacements — «удалённый id → оставшийся id».
type replacements map[int64]int64

// resolve идёт по цепочке замен до профиля, который остаётся.
func (r replacements) resolve(id int64) int64 {
	for {
		next, ok := r[id]
		if !ok {
			return id
		}
		id = next
	}
}

// rewriteChain возвращает цепочку с исправленными звеньями; без изменений —
// тот же указатель. Не-цепочки возвращаются как есть.
func (r replacements) rewriteChain(p *model.Profile) *model.Profile {
	d, ok := p.Detail.(*model.ChainDetail)
	if !ok || p.Type != model.TypeChain {
		return p
	}
	ids := make([]int64, len(d.ProfileIDs))
	changed := false
	for i, id := range d.ProfileIDs {
		ids[i] = r.resolve(id)
		changed = changed || ids[i] != id
	}
	if !changed {
		return p
	}
	cp := *p
	cp.Detail = &model.ChainDetail{ProfileIDs: ids}
	return &cp
}

// planDedup ищет копии среди профилей scope (одна группа). Остаётся первый
// профиль класса, а если в классе есть активный — он. Цепочки сравниваются
// после переписывания звеньев, поэтому удаление копии сервера может сделать
// копиями и цепочки: поиск повторяется, пока находится новое. Ссылки цепочек
// из всех групп (all) и front proxy групп переписываются на оставшиеся.
func planDedup(scope, all []*model.Profile, groups []*model.Group, active int64) dedupPlan {
	repl := replacements{}
	for collapseOnce(scope, repl, active) {
	}
	var plan dedupPlan
	for _, p := range scope {
		if _, gone := repl[p.ID]; gone {
			plan.removed = append(plan.removed, p)
		}
	}
	for _, p := range all {
		if _, gone := repl[p.ID]; gone {
			continue
		}
		if rewritten := repl.rewriteChain(p); rewritten != p {
			plan.chains = append(plan.chains, rewritten)
		}
	}
	for _, g := range groups {
		if g.FrontProxyID == nil || repl.resolve(*g.FrontProxyID) == *g.FrontProxyID {
			continue
		}
		cp := *g
		front := repl.resolve(*g.FrontProxyID)
		cp.FrontProxyID = &front
		plan.groups = append(plan.groups, &cp)
	}
	return plan
}

// collapseOnce находит копии среди ещё не удалённых профилей и дописывает
// замены в repl; false — новых копий нет.
func collapseOnce(scope []*model.Profile, repl replacements, active int64) bool {
	classes := map[string][]*model.Profile{}
	var order []string
	for _, p := range scope {
		if _, gone := repl[p.ID]; gone {
			continue
		}
		key := model.DedupKey(repl.rewriteChain(p))
		if key == "" {
			continue
		}
		if _, seen := classes[key]; !seen {
			order = append(order, key)
		}
		classes[key] = append(classes[key], p)
	}
	found := false
	for _, key := range order {
		class := classes[key]
		keep := class[0]
		for _, p := range class {
			if p.ID == active {
				keep = p
			}
		}
		for _, p := range class {
			if p != keep {
				repl[p.ID] = keep.ID
				found = true
			}
		}
	}
	return found
}
