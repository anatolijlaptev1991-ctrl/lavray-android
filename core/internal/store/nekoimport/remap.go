// SPDX-License-Identifier: GPL-3.0-only

package nekoimport

import (
	"strconv"

	"github.com/anatolijlaptev1991-ctrl/lavray-android/core/internal/model"
	"github.com/anatolijlaptev1991-ctrl/lavray-android/core/internal/store"
)

// remap.go — Remap: из Plan и State получает то, что станет в базе. Чистая
// функция: ничего не читает и не пишет. Закрывает тихий пропуск: профиль
// NekoBox, чей id занят другим профилем LAVRay, больше не считается «уже
// перенесённым», а получает новый id; ссылки цепочек и front_proxy_id
// переписываются на итоговые id.

// NewGroup — группа к созданию (с итоговым id).
type NewGroup struct {
	NekoID int64
	Key    string
	Group  *model.Group
}

// NewProfile — профиль к созданию (итоговые id, группа и состав цепочки).
type NewProfile struct {
	NekoID  int64
	Key     string
	Profile *model.Profile
}

// FrontLink — front_proxy_id группы после вставки профилей (FK на profiles).
type FrontLink struct{ GroupID, ProfileID int64 }

// Resolved — итог Remap и вход Apply.
type Resolved struct {
	Groups     []NewGroup
	Profiles   []NewProfile
	Fronts     []FrontLink
	GroupMap   map[int64]int64 // id NekoBox → локальный id (новые и уже перенесённые)
	ProfileMap map[int64]int64
	Settings   []PlanSetting        // ссылки заменены локальными id
	PresetRows []store.ImportMapRow // строки import_map наборов; заполняет слой API
	Report     Report
}

// Remap выдаёт id и собирает отчёт. Plan не меняется.
func Remap(p *Plan, st State) *Resolved {
	r := &Resolved{GroupMap: map[int64]int64{}, ProfileMap: map[int64]int64{}, Report: p.Report.clone()}
	r.Report.OldFmtGroups = p.Report.OldFmtGroups
	r.resolveGroups(p, st)
	r.resolveProfiles(p, st)
	r.rewriteChains()
	r.resolveFronts(p)
	r.resolveSettings(p)
	return r
}

func (r *Resolved) resolveGroups(p *Plan, st State) {
	// Группы: отметка выданных id лежит в зарезервированном диапазоне Default, на
	// id группы ссылается лишь настройка ui.current_group (Remap её переписывает),
	// поэтому занятость определяется существованием, как раньше.
	alloc := newIDAlloc(st.GroupIDs, st.GroupFloor, groupIDsOf(p.Groups), false)
	for _, pg := range p.Groups {
		if id, ok := st.localNumeric(KindGroup, pg.Key); ok && st.GroupIDs[id] {
			r.GroupMap[pg.NekoID] = id
			r.Report.AlreadyPresent++
			continue
		}
		id, changed := alloc.assign(pg.NekoID, pg.Synthetic)
		if changed {
			r.Report.Renumbered++
		}
		g := *pg.Group
		g.ID, g.FrontProxyID = id, nil
		r.GroupMap[pg.NekoID] = id
		r.Groups = append(r.Groups, NewGroup{NekoID: pg.NekoID, Key: pg.Key, Group: &g})
		r.Report.GroupsImported++
	}
}

func (r *Resolved) resolveProfiles(p *Plan, st State) {
	alloc := newIDAlloc(st.ProfileIDs, st.ProfileFloor, profileIDsOf(p.Profiles), true)
	for _, pp := range p.Profiles {
		if id, ok := st.localNumeric(KindProfile, pp.Key); ok && st.ProfileIDs[id] {
			r.ProfileMap[pp.NekoID] = id
			r.Report.AlreadyPresent++
			continue
		}
		id, changed := alloc.assign(pp.NekoID, false)
		if changed {
			r.Report.Renumbered++
		}
		cp := *pp.Profile
		cp.ID, cp.GroupID = id, r.GroupMap[pp.NekoGroupID]
		r.ProfileMap[pp.NekoID] = id
		r.Profiles = append(r.Profiles, NewProfile{NekoID: pp.NekoID, Key: pp.Key, Profile: &cp})
		r.Report.ProfilesByType[string(cp.Type)]++
	}
}

// rewriteChains заменяет состав цепочек итоговыми id. Звено, которого нет среди
// профилей каталога, отбрасывается с предупреждением: оставленный как есть id
// указал бы на чужой профиль LAVRay с тем же номером.
func (r *Resolved) rewriteChains() {
	for _, np := range r.Profiles {
		cd, ok := np.Profile.Detail.(*model.ChainDetail)
		if !ok {
			continue
		}
		ids := make([]int64, 0, len(cd.ProfileIDs))
		for _, member := range cd.ProfileIDs {
			if local, found := r.ProfileMap[member]; found {
				ids = append(ids, local)
				continue
			}
			r.Report.warn("chain %q: member %d is not in the NekoBox config; dropped", np.Profile.Name, member)
		}
		copied := *cd
		copied.ProfileIDs = ids
		np.Profile.Detail = &copied
	}
}

// resolveFronts: front_proxy_id новых групп → итоговый id профиля.
func (r *Resolved) resolveFronts(p *Plan) {
	created := map[int64]bool{}
	for _, ng := range r.Groups {
		created[ng.NekoID] = true
	}
	for _, pg := range p.Groups {
		if !created[pg.NekoID] || pg.FrontProxy < 0 {
			continue
		}
		local, ok := r.ProfileMap[pg.FrontProxy]
		if !ok {
			r.Report.warn("group %q: front_proxy_id %d references missing profile; dropped", pg.Group.Name, pg.FrontProxy)
			continue
		}
		r.Fronts = append(r.Fronts, FrontLink{GroupID: r.GroupMap[pg.NekoID], ProfileID: local})
	}
}

// resolveSettings заменяет id NekoBox в значениях настроек локальными.
func (r *Resolved) resolveSettings(p *Plan) {
	for _, s := range p.Settings {
		if s.Ref == RefNone || s.RefID < 0 { // -1 = «ничего не выбрано» в обеих программах
			r.Settings = append(r.Settings, s)
			continue
		}
		m := r.ProfileMap
		if s.Ref == RefGroup {
			m = r.GroupMap
		}
		local, ok := m[s.RefID]
		if !ok {
			r.Report.warn("setting %s not transferred: NekoBox record %d was not transferred", s.Key, s.RefID)
			continue
		}
		s.JSON = strconv.FormatInt(local, 10)
		r.Settings = append(r.Settings, s)
	}
}
