// SPDX-License-Identifier: GPL-3.0-only

package api

// migration_routes.go — маршруты NekoBox → пользовательские наборы. Вставку
// считает PlanInsert (T-80): он выдаёт id `custom:neko-<slug>`, проверяет набор и
// предел 100 наборов; список после очередной вставки передаётся в следующую.

import (
	"fmt"

	"github.com/anatolijlaptev1991-ctrl/lavray-android/core/internal/presets"
	"github.com/anatolijlaptev1991-ctrl/lavray-android/core/internal/store"
	"github.com/anatolijlaptev1991-ctrl/lavray-android/core/internal/store/nekoimport"
)

// planRoutes добавляет в res счётчики и строки import_map для маршрутов и
// возвращает новое значение meta['routing.custom_presets'] ("" — наборы не
// меняются). Негодный маршрут и превышение предела попадают в Skipped и
// остальные не останавливают; общий размер сверх предела — ошибка всего переноса.
func planRoutes(res *nekoimport.Resolved, plan *nekoimport.Plan, st nekoimport.State,
	current []*presets.Preset, include bool) (string, error) {
	rep := &res.Report
	if !include || len(plan.Routes) == 0 {
		return "", nil
	}
	rows := st.Rows(nekoimport.KindPreset)
	list, idByName, changed := current, map[string]string{}, false
	for _, r := range plan.Routes {
		if id, ok := rows[r.Key]; ok && indexOfPreset(list, id) >= 0 {
			rep.AlreadyPresent++ // перенесён раньше, локальная копия на месте
			idByName[r.Name] = id
			continue
		}
		in := r.Preset.Clone()
		in.Builtin, in.SourceID, in.SourceRevision = false, "", 0
		ins, err := PlanInsert(list, in)
		if err != nil {
			rep.Skipped = append(rep.Skipped, nekoimport.Skipped{File: r.File, Type: "route", Reason: err.Error()})
			continue
		}
		list, changed = ins.List, true
		idByName[r.Name] = ins.Preset.ID
		rep.RoutesImported++
		for _, w := range r.Warnings {
			rep.Warnings = append(rep.Warnings, fmt.Sprintf("маршрут %q: %s", r.Name, w))
		}
		res.PresetRows = append(res.PresetRows, store.ImportMapRow{
			Source: nekoimport.SourceNekoBox, Kind: nekoimport.KindPreset, SourceKey: r.Key, LocalID: ins.Preset.ID})
	}
	rep.ActiveRoute = idByName[plan.ActiveRoute]
	if !changed {
		return "", nil
	}
	return encodePresets(list)
}
