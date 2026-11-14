// SPDX-License-Identifier: GPL-3.0-only

// routing_service.go — RoutingService: список наборов (встроенные + пользовательские),
// чтение, активация. Правка пользовательских наборов — preset_service.go.
package api

import (
	"context"

	"connectrpc.com/connect"
	lavrayv1 "github.com/anatolijlaptev1991-ctrl/lavray-android/core/internal/gen/lavrayv1"
	"github.com/anatolijlaptev1991-ctrl/lavray-android/core/internal/presets"
)

// presetProto — набор → proto с вычисленным source_updated.
func (s *Server) presetProto(p *presets.Preset) (*lavrayv1.RoutingPreset, error) {
	rev, err := s.builtinRevisions()
	if err != nil {
		return nil, err
	}
	return presetOut(p, rev), nil
}

// allPresets — встроенные наборы, затем пользовательские (порядок карточек в UI).
func (s *Server) allPresets() ([]*presets.Preset, error) {
	list, err := s.builtinList()
	if err != nil {
		return nil, err
	}
	custom, err := s.presetRepo.List()
	if err != nil {
		return nil, err
	}
	return append(list, custom...), nil
}

func (s *Server) ListPresets(ctx context.Context, req *connect.Request[lavrayv1.ListPresetsRequest]) (*connect.Response[lavrayv1.ListPresetsResponse], error) {
	list, err := s.allPresets()
	if err != nil {
		return nil, mapErr(err)
	}
	rev, err := s.builtinRevisions()
	if err != nil {
		return nil, mapErr(err)
	}
	out := make([]*lavrayv1.RoutingPreset, 0, len(list))
	cur, active := s.activePresetID(), "global" // id, которого нет в списке, читается как global
	for _, p := range list {
		out = append(out, presetOut(p, rev))
		if p.ID == cur {
			active = p.ID
		}
	}
	return connect.NewResponse(&lavrayv1.ListPresetsResponse{Presets: out, ActivePresetId: active}), nil
}

func (s *Server) GetPreset(ctx context.Context, req *connect.Request[lavrayv1.GetPresetRequest]) (*connect.Response[lavrayv1.RoutingPreset], error) {
	p, ok, err := s.findPreset(req.Msg.Id)
	if err != nil {
		return nil, mapErr(err)
	}
	if !ok {
		return nil, notFound("пресет %q", req.Msg.Id)
	}
	out, err := s.presetProto(p)
	if err != nil {
		return nil, mapErr(err)
	}
	return connect.NewResponse(out), nil
}

func (s *Server) ActivatePreset(ctx context.Context, req *connect.Request[lavrayv1.ActivatePresetRequest]) (*connect.Response[lavrayv1.ActivatePresetResponse], error) {
	// Проверка набора и запись активного id — под замком репозитория: иначе
	// DeletePreset между ними оставил бы активным уже удалённый набор.
	err := s.presetRepo.Locked(func(list []*presets.Preset) error {
		p, err := s.lookupPreset(list, req.Msg.Id)
		if err != nil {
			return err
		}
		if err := p.Validate(); err != nil {
			return err
		}
		// F2 (аудит №7): активный пресет — в settings (единственный источник,
		// который читает activePresetID при сборке конфига), не в meta.
		return s.setActivePreset(p.ID)
	})
	if err != nil {
		return nil, mapErr(err)
	}
	needsRestart := s.inst.Status().State != instStateIdle
	return connect.NewResponse(&lavrayv1.ActivatePresetResponse{NeedsProfileRestart: needsRestart}), nil
}

func (s *Server) GetActivePreset(ctx context.Context, req *connect.Request[lavrayv1.GetActivePresetRequest]) (*connect.Response[lavrayv1.RoutingPreset], error) {
	p, err := s.activeOrGlobal()
	if err != nil {
		return nil, mapErr(err)
	}
	out, err := s.presetProto(p)
	if err != nil {
		return nil, mapErr(err)
	}
	return connect.NewResponse(out), nil
}
