// SPDX-License-Identifier: GPL-3.0-only

package api

// preset_view.go — как набор виден клиенту: имя для показа и признак
// source_updated (T-80). Остальная конвертация — preset_convert.go.

import (
	"github.com/anatolijlaptev1991-ctrl/lavray-android/core/internal/gen/lavrayv1"
	"github.com/anatolijlaptev1991-ctrl/lavray-android/core/internal/presets"
)

// presetDisplayName — имя пользовательского набора для показа: en, затем первый
// из остальных языков в порядке поставки, затем id.
func presetDisplayName(p *presets.Preset) string {
	if name := p.Name["en"]; name != "" {
		return name
	}
	for _, lang := range []string{"ru", "zh", "es", "de"} {
		if name := p.Name[lang]; name != "" {
			return name
		}
	}
	return p.ID
}

// presetOut — набор → proto с вычисленным source_updated: ревизия встроенного
// источника выше той, с которой снята копия (rev — builtinRevisions).
func presetOut(p *presets.Preset, rev map[string]int) *lavrayv1.RoutingPreset {
	r := presetToProto(p)
	if cur, ok := rev[p.SourceID]; ok && p.SourceID != "" && cur > p.SourceRevision {
		r.SourceUpdated = true
	}
	return r
}

// builtinRevisions — действующие ревизии встроенных наборов по id (с переопределениями).
func (s *Server) builtinRevisions() (map[string]int, error) {
	list, err := s.builtinList()
	if err != nil {
		return nil, err
	}
	out := make(map[string]int, len(list))
	for _, p := range list {
		out[p.ID] = p.Revision
	}
	return out, nil
}
