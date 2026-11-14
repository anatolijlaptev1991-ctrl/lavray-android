// SPDX-License-Identifier: GPL-3.0-only

// update_comp_rollback.go — Rollback для PRESETS и GEO (T-75/7.5): возвращает
// прошлую ревизию, которую сохранило применение (override.prev, geo.prev).
// Нет прошлой ревизии — FAILED_PRECONDITION; применение и откат взаимно
// исключаются (ABORTED).
package api

import (
	"errors"

	"connectrpc.com/connect"

	lavrayv1 "github.com/anatolijlaptev1991-ctrl/lavray-android/core/internal/gen/lavrayv1"
	"github.com/anatolijlaptev1991-ctrl/lavray-android/core/internal/updates"
)

// rollbackData возвращает прошлую ревизию наборов или гео-набора.
func (s *Server) rollbackData(c lavrayv1.UpdateComponent) (*connect.Response[lavrayv1.RollbackResponse], error) {
	if !s.upd.beginApply() {
		return nil, connect.NewError(connect.CodeAborted, errors.New("применение или откат данных уже идёт"))
	}
	defer s.upd.endApply()
	restored, restart, err := s.rollbackStep(c)
	if err != nil {
		return nil, s.dataFailure(c, err)
	}
	old := s.upd.stagedOf(c)
	s.upd.setCompStaged(c, nil) // скачанное относилось к прежнему состоянию данных
	s.cleanComponentStaging(old)
	resp := &lavrayv1.RollbackResponse{Success: true, NeedsProfileRestart: restart && s.sessionActive()}
	if restored != "" {
		resp.RestoredVersion = &restored
	}
	return connect.NewResponse(resp), nil
}

// rollbackStep откатывает данные; restored — название возвращённой ревизии,
// restart — менялись данные работающего профиля.
func (s *Server) rollbackStep(c lavrayv1.UpdateComponent) (restored string, restart bool, err error) {
	if c == compGeo {
		prev, _ := updates.GeoPrevious(s.geoDir())
		if err := updates.RollbackGeo(s.geoDir()); err != nil {
			return "", false, err
		}
		return geoLabelOf(prev), true, nil
	}
	before, err := s.builtinRevisions()
	if err != nil {
		return "", false, err
	}
	if err := updates.RollbackPresets(s.presetOverrideDir()); err != nil {
		return "", false, err
	}
	after, err := s.builtinRevisions()
	if err != nil {
		return "", false, err
	}
	changed := map[string]int{}
	for id, rev := range after {
		if before[id] != rev {
			changed[id] = rev
		}
	}
	return revString(changed), before[s.activePresetID()] != after[s.activePresetID()], nil
}

func geoLabelOf(g updates.GeoInstalled) string {
	if g.Sha256 == "" {
		return ""
	}
	return geoLabel(g.GeneratedAt, g.Sha256)
}
