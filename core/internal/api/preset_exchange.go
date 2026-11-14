// SPDX-License-Identifier: GPL-3.0-only

package api

// preset_exchange.go — RoutingService: обмен наборами файлом (T-80). Экспорт
// отдаёт JSON набора; импорт принимает набор LAVRay или маршрут NekoRay и всегда
// создаёт НОВЫЙ пользовательский набор — id, ревизию, признак builtin и источник
// из файла ядро не берёт (файл не может занять id встроенного набора).

import (
	"context"

	"connectrpc.com/connect"

	lavrayv1 "github.com/anatolijlaptev1991-ctrl/lavray-android/core/internal/gen/lavrayv1"
	"github.com/anatolijlaptev1991-ctrl/lavray-android/core/internal/presets"
)

// importedFallbackName — имя набора, если в файле его нет.
const importedFallbackName = "Imported preset"

// ExportPreset отдаёт файл набора (встроенного или своего) и имя файла без расширения.
func (s *Server) ExportPreset(ctx context.Context, req *connect.Request[lavrayv1.ExportPresetRequest]) (*connect.Response[lavrayv1.ExportPresetResponse], error) {
	p, ok, err := s.findPreset(req.Msg.Id)
	if err != nil {
		return nil, mapErr(err)
	}
	if !ok {
		return nil, notFound("набор %q", req.Msg.Id)
	}
	data, err := presets.ExportJSON(p)
	if err != nil {
		return nil, mapErr(err)
	}
	name := p.ID
	if !p.Builtin {
		name = safeFileStem(presets.Slugify(presetDisplayName(p)))
	}
	return connect.NewResponse(&lavrayv1.ExportPresetResponse{Json: string(data), SuggestedName: name}), nil
}

// safeFileStem — имя файла без расширения, пригодное для Windows: зарезервированные
// имена устройств (con, nul, com1…) получают приставку, иначе «Сохранить как» их отвергнет.
func safeFileStem(stem string) string {
	reserved := stem == "con" || stem == "prn" || stem == "aux" || stem == "nul"
	if len(stem) == 4 && (stem[:3] == "com" || stem[:3] == "lpt") && stem[3] >= '1' && stem[3] <= '9' {
		reserved = true
	}
	if reserved {
		return "preset-" + stem
	}
	return stem
}

// ImportPreset создаёт пользовательский набор из файла (набор LAVRay или маршрут NekoRay).
func (s *Server) ImportPreset(ctx context.Context, req *connect.Request[lavrayv1.ImportPresetRequest]) (*connect.Response[lavrayv1.ImportPresetResponse], error) {
	p, warnings, err := presets.DetectAndParse([]byte(req.Msg.Json), "")
	if err != nil {
		return nil, mapErr(err)
	}
	out, err := s.insertImported(p)
	if err != nil {
		return nil, mapErr(err)
	}
	return connect.NewResponse(&lavrayv1.ImportPresetResponse{Preset: out, Warnings: warnings}), nil
}

// insertImported очищает всё, что файл не вправе задавать, и вставляет набор
// (PlanInsert выдаёт id и ревизию 1, проверяет набор и лимиты).
func (s *Server) insertImported(p *presets.Preset) (*lavrayv1.RoutingPreset, error) {
	in := p.Clone()
	in.Builtin, in.SourceID, in.SourceRevision = false, "", 0
	if presetDisplayName(in) == "" || len(in.Name) == 0 {
		in.Name = presets.Localized{"en": importedFallbackName}
	}
	var made *presets.Preset
	err := s.presetRepo.Modify(func(list []*presets.Preset) ([]*presets.Preset, error) {
		plan, err := PlanInsert(list, in)
		if err != nil {
			return nil, err
		}
		made = plan.Preset
		return plan.List, nil
	})
	if err != nil {
		return nil, err
	}
	return s.presetProto(made)
}
