// SPDX-License-Identifier: GPL-3.0-only

package nekoimport

import (
	"sort"

	"github.com/anatolijlaptev1991-ctrl/lavray-android/core/internal/model"
)

// scan.go — Scan: разбор каталога NekoBox без записи. Базу и сеть не трогает,
// исходный каталог не меняет (TestScanDoesNotWrite).

// scanner хранит состояние одного разбора.
type scanner struct {
	rd   *reader
	plan *Plan
}

// Scan разбирает каталог config NekoBox: группы, профили, маршруты, настройки.
func Scan(dir string) (*Plan, error) { return scanWith(dir, DefaultLimits) }

func scanWith(dir string, lim Limits) (*Plan, error) {
	sc := &scanner{rd: newReader(dir, lim), plan: &Plan{Dir: dir, Report: newReport()}}
	tabOrder := sc.readTabOrder()
	raw, err := sc.readGroups()
	if err != nil {
		return nil, err
	}
	items, err := sc.readProfiles(raw)
	if err != nil {
		return nil, err
	}
	groups := groupsOf(raw)
	order := orderGroups(tabOrder, groups)
	if len(order) == 0 && len(items) > 0 {
		order = sc.addFallbackGroup(groups, raw)
	}
	for _, gid := range order {
		sc.plan.Groups = append(sc.plan.Groups, *groups[gid])
	}
	sc.plan.Profiles = sc.orderProfiles(order, raw, items, groups)
	if err := sc.scanRoutes(); err != nil {
		return nil, err
	}
	if err := sc.scanSettings(); err != nil {
		return nil, err
	}
	return sc.plan, nil
}

// orderGroups — порядок создания групп: pm.json, затем не перечисленные по id.
func orderGroups(tab []int64, groups map[int64]*PlanGroup) []int64 {
	order := make([]int64, 0, len(groups))
	seen := map[int64]bool{}
	for _, gid := range tab {
		if groups[gid] != nil && !seen[gid] {
			order = append(order, gid)
			seen[gid] = true
		}
	}
	rest := make([]int64, 0, len(groups))
	for gid := range groups {
		if !seen[gid] {
			rest = append(rest, gid)
		}
	}
	sort.Slice(rest, func(i, j int) bool { return rest[i] < rest[j] })
	return append(order, rest...)
}

// syntheticGroupID — NekoID служебной группы «Imported» (id NekoBox ≥ 0).
const syntheticGroupID int64 = -1

// addFallbackGroup: профили есть, групп нет — их примет группа «Imported».
func (sc *scanner) addFallbackGroup(groups map[int64]*PlanGroup, raw map[int64]*nekoGroup) []int64 {
	g := &model.Group{ID: syntheticGroupID, Name: "Imported", Kind: model.GroupBasic}
	groups[syntheticGroupID] = &PlanGroup{
		NekoID: syntheticGroupID, Key: SourceKey(syntheticGroupID, "group", g.Name),
		Group: g, FrontProxy: -1, Synthetic: true,
	}
	raw[syntheticGroupID] = &nekoGroup{ID: syntheticGroupID}
	sc.plan.Report.warn("no groups found; created fallback group %q", g.Name)
	return []int64{syntheticGroupID}
}

// orderProfiles — профили в порядке вставки: по группам, внутри группы сначала
// порядок order группы, затем хвост по id; профили без группы — в первую группу.
// Профиль попадает в план ровно один раз: запись order относится к группе только
// если gid профиля совпадает с этой группой (в order чужой группы он игнорируется).
func (sc *scanner) orderProfiles(order []int64, raw map[int64]*nekoGroup, items map[int64]*PlanProfile, groups map[int64]*PlanGroup) []PlanProfile {
	var out []PlanProfile
	placed := map[int64]bool{}
	add := func(pp *PlanProfile, gid int64) {
		placed[pp.NekoID] = true
		cp := *pp
		cp.NekoGroupID = gid
		out = append(out, cp)
	}
	for _, gid := range order {
		for _, pid := range raw[gid].Order {
			if it, ok := items[pid]; ok && !placed[pid] && it.NekoGroupID == gid {
				add(it, gid)
			}
		}
		var tail []int64
		for pid, it := range items {
			if it.NekoGroupID == gid && !placed[pid] {
				tail = append(tail, pid)
			}
		}
		sort.Slice(tail, func(i, j int) bool { return tail[i] < tail[j] })
		for _, pid := range tail {
			add(items[pid], gid)
		}
	}
	if len(order) == 0 {
		return out
	}
	var orphans []int64
	for pid, it := range items {
		if groups[it.NekoGroupID] == nil {
			orphans = append(orphans, pid)
		}
	}
	sort.Slice(orphans, func(i, j int) bool { return orphans[i] < orphans[j] })
	for _, pid := range orphans {
		it := items[pid]
		sc.plan.Report.warn("profile %d: group %d missing; imported into group %q", pid, it.NekoGroupID, groups[order[0]].Group.Name)
		add(it, order[0])
	}
	return out
}
