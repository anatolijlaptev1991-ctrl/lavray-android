// SPDX-License-Identifier: GPL-3.0-only

// update_comp_info.go — что сообщают CheckUpdates и GetUpdateState о наборах и
// гео-данных (T-75/7.5): доступные ревизии по подписанному манифесту и состояние
// компонентов (фаза, скачанный артефакт, доступность отката).
package api

import (
	"fmt"
	"sort"
	"strings"

	lavrayv1 "github.com/anatolijlaptev1991-ctrl/lavray-android/core/internal/gen/lavrayv1"
	"github.com/anatolijlaptev1991-ctrl/lavray-android/core/internal/updates"
)

// wantsComp: компонент запрошен (пустой список — все).
func wantsComp(want []lavrayv1.UpdateComponent, c lavrayv1.UpdateComponent) bool {
	if len(want) == 0 {
		return true
	}
	for _, w := range want {
		if w == c {
			return true
		}
	}
	return false
}

// revString — «lav-ru:3, lav-cn:5» по возрастанию id.
func revString(revs map[string]int) string {
	ids := make([]string, 0, len(revs))
	for id := range revs {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	parts := make([]string, len(ids))
	for i, id := range ids {
		parts[i] = fmt.Sprintf("%s:%d", id, revs[id])
	}
	return strings.Join(parts, ", ")
}

// geoLabel — как назвать гео-набор пользователю: дата среза, иначе начало sha256.
func geoLabel(generatedAt, sha string) string {
	if generatedAt != "" {
		return generatedAt
	}
	return sha[:min(12, len(sha))]
}

// componentOffers — предложения PRESETS и GEO из проверенного манифеста.
func (s *Server) componentOffers(res *updates.Result, want []lavrayv1.UpdateComponent) []*lavrayv1.ComponentUpdate {
	var out []*lavrayv1.ComponentUpdate
	if wantsComp(want, compPresets) {
		if o := s.presetsOffer(res); o != nil {
			out = append(out, o)
		}
	}
	if wantsComp(want, compGeo) {
		if o := s.geoOffer(res); o != nil {
			out = append(out, o)
		}
	}
	return out
}

func (s *Server) presetsOffer(res *updates.Result) *lavrayv1.ComponentUpdate {
	installed, err := s.builtinList()
	if err != nil {
		return nil
	}
	cur, next := map[string]int{}, map[string]int{}
	for _, o := range updates.PendingPresets(res.Manifest, installed) {
		cur[o.ID], next[o.ID] = o.Installed, o.Rev
	}
	if len(next) == 0 {
		return nil
	}
	avail := revString(next)
	return &lavrayv1.ComponentUpdate{Component: compPresets, CurrentVersion: revString(cur),
		AvailableVersion: &avail, Notes: res.Manifest.Notes}
}

func (s *Server) geoOffer(res *updates.Result) *lavrayv1.ComponentUpdate {
	have := updates.InstalledGeo(s.geoDir())
	sec := updates.PendingGeo(res.Manifest, have)
	if sec == nil {
		return nil
	}
	avail := geoLabel(sec.GeneratedAt, sec.Sha256)
	cur := ""
	if have.Sha256 != "" {
		cur = geoLabel(have.GeneratedAt, have.Sha256)
	}
	return &lavrayv1.ComponentUpdate{Component: compGeo, CurrentVersion: cur,
		AvailableVersion: &avail, Notes: res.Manifest.Notes}
}

// componentStates — состояния PRESETS и GEO для GetUpdateState.
func (s *Server) componentStates() []*lavrayv1.ComponentState {
	return []*lavrayv1.ComponentState{s.dataComponentState(compPresets), s.dataComponentState(compGeo)}
}

func (s *Server) dataComponentState(c lavrayv1.UpdateComponent) *lavrayv1.ComponentState {
	phase, errText, staged := s.upd.compSnapshot(c)
	cs := &lavrayv1.ComponentState{Component: c, Phase: phase}
	if staged != nil {
		v := staged.version
		cs.StagedVersion = &v
	}
	if errText != "" {
		cs.Error = &errText
	}
	prev, ok := s.previousLabel(c)
	cs.RollbackAvailable = ok
	if ok && prev != "" {
		cs.PreviousVersion = &prev
	}
	return cs
}

// previousLabel — как назвать прошлую ревизию ("" — поставка из бинарника) и есть ли она.
func (s *Server) previousLabel(c lavrayv1.UpdateComponent) (string, bool) {
	if c == compPresets {
		revs, ok := updates.PresetsPrevious(s.presetOverrideDir())
		return revString(revs), ok
	}
	prev, ok := updates.GeoPrevious(s.geoDir())
	if !ok || prev.Sha256 == "" {
		return "", ok
	}
	return geoLabel(prev.GeneratedAt, prev.Sha256), true
}
