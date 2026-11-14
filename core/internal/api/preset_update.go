// SPDX-License-Identifier: GPL-3.0-only

package api

// preset_update.go — RoutingService: правка и удаление пользовательских наборов
// (T-80). Конфликт ревизий — ABORTED; встроенный набор — FAILED_PRECONDITION.

import (
	"context"
	"encoding/json"

	"connectrpc.com/connect"

	lavrayv1 "github.com/anatolijlaptev1991-ctrl/lavray-android/core/internal/gen/lavrayv1"
	"github.com/anatolijlaptev1991-ctrl/lavray-android/core/internal/presets"
)

// UpdatePreset записывает правку пользовательского набора. Устаревшая ревизия —
// ABORTED (клиент перечитывает набор). Идентичность, источник и признак
// builtin берутся из хранилища, из запроса — только значения.
func (s *Server) UpdatePreset(ctx context.Context, req *connect.Request[lavrayv1.UpdatePresetRequest]) (*connect.Response[lavrayv1.UpdatePresetResponse], error) {
	m := req.Msg
	if m.Preset == nil {
		return nil, invalidArg("preset обязателен")
	}
	id := m.Preset.Id
	var saved *presets.Preset
	err := s.presetRepo.Modify(func(list []*presets.Preset) ([]*presets.Preset, error) {
		i, err := editableIndex(list, id)
		if err != nil {
			return nil, err
		}
		stored := list[i]
		if int32(stored.Revision) != m.ExpectedRevision {
			return nil, abortedRevision(id, stored.Revision, int(m.ExpectedRevision))
		}
		next, err := mergeUpdate(stored, m.Preset)
		if err != nil {
			return nil, err
		}
		saved, list[i] = next, next
		return list, nil
	})
	if err != nil {
		return nil, mapErr(err)
	}
	out, err := s.presetProto(saved)
	if err != nil {
		return nil, mapErr(err)
	}
	return connect.NewResponse(&lavrayv1.UpdatePresetResponse{
		Preset: out, NewRevision: out.Revision, NeedsProfileRestart: s.restartNeeded(id),
	}), nil
}

// mergeUpdate накладывает значения запроса на сохранённый набор: ревизия +1,
// имя не из запроса, а прежнее (все переводы), если клиент его не менял; DNS не
// присланный клиентом остаётся прежним.
func mergeUpdate(stored *presets.Preset, r *lavrayv1.RoutingPreset) (*presets.Preset, error) {
	next := presetFromProto(r)
	next.ID, next.Builtin = stored.ID, false
	next.SourceID, next.SourceRevision = stored.SourceID, stored.SourceRevision
	next.Description, next.MaintainerNotes = stored.Description, stored.MaintainerNotes
	if r.GetDisplayName() == presetDisplayName(stored) {
		next.Name = stored.Name
	}
	if r.GetDns() == nil {
		next.DNS = stored.DNS
	}
	if err := next.PrepareUser(); err != nil {
		return nil, &presetInvalidError{err}
	}
	next.Revision = stored.Revision + 1
	return next, nil
}

// DeletePreset удаляет пользовательский набор. Если он активен, активным
// становится «global» (переключение — до удаления: при сбое набор остаётся, а
// активный всегда существует); при работающем соединении нужен перезапуск.
func (s *Server) DeletePreset(ctx context.Context, req *connect.Request[lavrayv1.DeletePresetRequest]) (*connect.Response[lavrayv1.DeletePresetResponse], error) {
	id := req.Msg.Id
	resp := &lavrayv1.DeletePresetResponse{}
	err := s.presetRepo.Modify(func(list []*presets.Preset) ([]*presets.Preset, error) {
		i, err := editableIndex(list, id)
		if err != nil {
			return nil, err
		}
		if s.activePresetID() == id {
			resp.NeedsProfileRestart = s.restartNeeded(id)
			if err := s.setActivePreset("global"); err != nil {
				return nil, err
			}
			resp.NewActiveId = "global"
		}
		return append(list[:i:i], list[i+1:]...), nil
	})
	if err != nil {
		return nil, mapErr(err)
	}
	if err := s.dropDNSChoice(id); err != nil {
		return nil, mapErr(err)
	}
	return connect.NewResponse(resp), nil
}

// setActivePreset записывает id активного набора (JSON-строкой, как ActivatePreset).
func (s *Server) setActivePreset(id string) error {
	encoded, err := json.Marshal(id)
	if err != nil {
		return err
	}
	return s.st.SetSetting(settingActivePreset, string(encoded))
}
