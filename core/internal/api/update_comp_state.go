// SPDX-License-Identifier: GPL-3.0-only

// update_comp_state.go — состояние доставки компонентов-данных (PRESETS, GEO,
// T-75/7.5): фаза, скачанный и проверенный артефакт в staging, замок применения.
// Все поля защищает updaterState.mu.
package api

import (
	"path/filepath"

	lavrayv1 "github.com/anatolijlaptev1991-ctrl/lavray-android/core/internal/gen/lavrayv1"
	"github.com/anatolijlaptev1991-ctrl/lavray-android/core/internal/presets"
	"github.com/anatolijlaptev1991-ctrl/lavray-android/core/internal/updates"
)

const (
	compPresets = lavrayv1.UpdateComponent_UPDATE_COMPONENT_PRESETS
	compGeo     = lavrayv1.UpdateComponent_UPDATE_COMPONENT_GEO
)

// stagedFile — файл компонента в <данные>/updates/staging/components.
type stagedFile struct {
	id     string // id набора; у гео пусто
	path   string
	sha256 string
}

// stagedComp — проверенный при загрузке артефакт компонента. Манифест сохранён
// тот, по которому артефакт сверен: применение берёт из него ревизии и sha256.
type stagedComp struct {
	manifest *updates.Manifest
	version  string
	files    []stagedFile
}

// compSlot — состояние одного компонента-данных.
type compSlot struct {
	phase  lavrayv1.ComponentState_Phase
	err    string
	staged *stagedComp
}

// slot — ячейка компонента (под mu); nil — это не компонент-данные.
func (u *updaterState) slot(c lavrayv1.UpdateComponent) *compSlot {
	switch c {
	case compPresets:
		return &u.presets
	case compGeo:
		return &u.geo
	}
	return nil
}

// setCompPhase меняет фазу компонента (APP — общим полем фазы).
func (u *updaterState) setCompPhase(c lavrayv1.UpdateComponent, p lavrayv1.ComponentState_Phase, errText string) {
	if sl := u.slot(c); sl != nil {
		u.mu.Lock()
		sl.phase, sl.err = p, errText
		u.mu.Unlock()
		return
	}
	u.setPhase(p, errText)
}

// setCompStaged запоминает скачанный артефакт (nil — забыть): фаза DOWNLOADED либо IDLE.
func (u *updaterState) setCompStaged(c lavrayv1.UpdateComponent, st *stagedComp) {
	u.mu.Lock()
	defer u.mu.Unlock()
	sl := u.slot(c)
	if sl == nil {
		return
	}
	sl.staged, sl.err = st, ""
	sl.phase = lavrayv1.ComponentState_PHASE_IDLE
	if st != nil {
		sl.phase = lavrayv1.ComponentState_PHASE_DOWNLOADED
	}
}

// settleComp возвращает фазу компонента к устойчивой: скачано либо простой.
func (u *updaterState) settleComp(c lavrayv1.UpdateComponent) {
	u.mu.Lock()
	defer u.mu.Unlock()
	if sl := u.slot(c); sl != nil {
		sl.phase, sl.err = lavrayv1.ComponentState_PHASE_IDLE, ""
		if sl.staged != nil {
			sl.phase = lavrayv1.ComponentState_PHASE_DOWNLOADED
		}
	}
}

// compSnapshot — фаза, ошибка и скачанный артефакт компонента.
func (u *updaterState) compSnapshot(c lavrayv1.UpdateComponent) (lavrayv1.ComponentState_Phase, string, *stagedComp) {
	u.mu.Lock()
	defer u.mu.Unlock()
	sl := u.slot(c)
	if sl == nil || sl.phase == lavrayv1.ComponentState_PHASE_UNSPECIFIED {
		var st *stagedComp
		if sl != nil {
			st = sl.staged
		}
		return lavrayv1.ComponentState_PHASE_IDLE, "", st
	}
	return sl.phase, sl.err, sl.staged
}

// stagedOf — скачанный артефакт компонента (nil — нет).
func (u *updaterState) stagedOf(c lavrayv1.UpdateComponent) *stagedComp {
	_, _, st := u.compSnapshot(c)
	return st
}

// beginApply занимает замок применения и отката данных; false — уже занят.
func (u *updaterState) beginApply() bool {
	u.mu.Lock()
	defer u.mu.Unlock()
	if u.applying {
		return false
	}
	u.applying = true
	return true
}

func (u *updaterState) endApply() {
	u.mu.Lock()
	u.applying = false
	u.mu.Unlock()
}

func (s *Server) geoDir() string            { return filepath.Join(s.st.Dir(), "geo") }
func (s *Server) presetOverrideDir() string { return presets.OverrideDir(s.st.Dir()) }
func (s *Server) compStagingDir() string    { return filepath.Join(s.stagingDir(), "components") }
