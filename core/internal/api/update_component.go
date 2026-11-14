// SPDX-License-Identifier: GPL-3.0-only

// update_component.go — состояние компонента APP для GetUpdateState: фаза
// загрузки/применения и доступность отката. Откат выполняет оболочка прошлым
// установщиком из <данные>/updates/previous; ядро только читает его описание
// previous.json {version, sha256, file}, которое пишет оболочка.
package api

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"

	lavrayv1 "github.com/anatolijlaptev1991-ctrl/lavray-android/core/internal/gen/lavrayv1"
)

// previousInfo — описание сохранённого прошлого установщика (формат оболочки).
type previousInfo struct {
	Version string `json:"version"`
	Sha256  string `json:"sha256"`
	File    string `json:"file"`
}

func (s *Server) updatesDir() string   { return filepath.Join(s.st.Dir(), "updates") }
func (s *Server) stagingDir() string   { return filepath.Join(s.updatesDir(), "staging") }
func (s *Server) rootsDir() string     { return filepath.Join(s.updatesDir(), "roots") }
func (s *Server) previousDir() string  { return filepath.Join(s.updatesDir(), "previous") }
func (s *Server) ensureStaging() error { return os.MkdirAll(s.stagingDir(), 0o700) }

// previousInstaller: прошлый установщик есть, описан корректно и лежит на диске.
func (s *Server) previousInstaller() (previousInfo, bool) {
	dir := s.previousDir()
	raw, err := os.ReadFile(filepath.Join(dir, "previous.json"))
	if err != nil || len(raw) > 4096 {
		return previousInfo{}, false
	}
	var p previousInfo
	if json.Unmarshal(raw, &p) != nil || p.Version == "" || p.File == "" {
		return previousInfo{}, false
	}
	if p.File != filepath.Base(p.File) || strings.ContainsAny(p.File, `/\:`) {
		return previousInfo{}, false // имя без путей: за пределы каталога не выйти
	}
	if fi, err := os.Stat(filepath.Join(dir, p.File)); err != nil || !fi.Mode().IsRegular() || fi.Size() == 0 {
		return previousInfo{}, false
	}
	return p, true
}

func (s *Server) appComponentState() *lavrayv1.ComponentState {
	s.upd.mu.Lock()
	phase, errText, staged := s.upd.phase, s.upd.phaseErr, s.upd.staged
	s.upd.mu.Unlock()
	cs := &lavrayv1.ComponentState{
		Component: lavrayv1.UpdateComponent_UPDATE_COMPONENT_APP,
		Phase:     phase,
	}
	if staged != nil {
		v := staged.Version
		cs.StagedVersion = &v
	}
	if errText != "" {
		cs.Error = &errText
	}
	if p, ok := s.previousInstaller(); ok {
		cs.RollbackAvailable = true
		cs.PreviousVersion = &p.Version
	}
	return cs
}
