// SPDX-License-Identifier: GPL-3.0-only

package api

// preset_service.go — RoutingService: создание и сброс пользовательских наборов
// (T-80). Встроенные наборы только для чтения: правка идёт через копию
// (CreatePreset с builtin_source_id), а ResetBuiltinPreset возвращает копию к
// исходному набору. Правка и удаление — preset_update.go. Чтение-изменение-запись
// — под мьютексом presetRepo.

import (
	"context"
	"strings"

	"connectrpc.com/connect"

	lavrayv1 "github.com/anatolijlaptev1991-ctrl/lavray-android/core/internal/gen/lavrayv1"
	"github.com/anatolijlaptev1991-ctrl/lavray-android/core/internal/presets"
)

// copySuffix — приписка к имени копии, пока клиент не переименовал её.
const copySuffix = " (copy)"

// CreatePreset создаёт пользовательский набор: копию (builtin_source_id — id
// встроенного или своего набора) либо набор из значений (preset). Ровно одно
// из двух; id выдаёт ядро.
func (s *Server) CreatePreset(ctx context.Context, req *connect.Request[lavrayv1.CreatePresetRequest]) (*connect.Response[lavrayv1.RoutingPreset], error) {
	m := req.Msg
	src := strings.TrimSpace(optStr(m.BuiltinSourceId))
	if (src != "") == (m.Preset != nil) {
		return nil, invalidArg("укажите ровно одно: builtin_source_id (копия набора) или preset (значения)")
	}
	var made *presets.Preset
	err := s.presetRepo.Modify(func(list []*presets.Preset) ([]*presets.Preset, error) {
		in := presetFromProtoOrNil(m.Preset)
		if src != "" {
			base, err := s.lookupPreset(list, src)
			if err != nil {
				return nil, err
			}
			in = presets.NewCustomFrom(base, "")
			in.Name = presets.Localized{"en": copyName(presetDisplayName(base))}
		}
		plan, err := PlanInsert(list, in)
		if err != nil {
			return nil, err
		}
		made = plan.Preset
		return plan.List, nil
	})
	return s.presetReply(made, err)
}

func presetFromProtoOrNil(r *lavrayv1.RoutingPreset) *presets.Preset {
	if r == nil {
		return nil
	}
	return presetFromProto(r)
}

// presetReply — набор → ответ RPC (либо ошибка в коде Connect).
func (s *Server) presetReply(p *presets.Preset, err error) (*connect.Response[lavrayv1.RoutingPreset], error) {
	if err != nil {
		return nil, mapErr(err)
	}
	out, err := s.presetProto(p)
	if err != nil {
		return nil, mapErr(err)
	}
	return connect.NewResponse(out), nil
}

// copyName — имя копии «<имя> (copy)», укладывающееся в предел имени.
func copyName(name string) string {
	keep := presets.MaxNameRunes - len([]rune(copySuffix))
	if r := []rune(name); len(r) > keep {
		name = string(r[:keep])
	}
	return name + copySuffix
}

// lookupPreset — встроенный набор или набор из list; иначе NOT_FOUND.
func (s *Server) lookupPreset(list []*presets.Preset, id string) (*presets.Preset, error) {
	if p, ok, err := s.builtinPreset(id); err != nil || ok {
		return p, err
	}
	if i := indexOfPreset(list, id); i >= 0 {
		return list[i], nil
	}
	return nil, notFound("набор %q", id)
}

// editableIndex — позиция пользовательского набора в list. Встроенный —
// FAILED_PRECONDITION (только чтение), неизвестный — NOT_FOUND.
func editableIndex(list []*presets.Preset, id string) (int, error) {
	if ok, err := isBuiltinID(id); err != nil {
		return -1, err
	} else if ok {
		return -1, failedPrecondition("встроенный набор %q только для чтения: сделайте копию", id)
	}
	i := indexOfPreset(list, id)
	if i < 0 {
		return -1, notFound("набор %q", id)
	}
	return i, nil
}

// restartNeeded — активный набор изменён при работающем соединении.
func (s *Server) restartNeeded(id string) bool {
	return id == s.activePresetID() && s.inst.Status().State != instStateIdle
}

// ResetBuiltinPreset возвращает копию встроенного набора к исходному: списки,
// процессные правила, custom-правила и DNS берутся у источника, имя остаётся.
// Ревизия копии растёт, запись об источнике обновляется.
func (s *Server) ResetBuiltinPreset(ctx context.Context, req *connect.Request[lavrayv1.ResetBuiltinPresetRequest]) (*connect.Response[lavrayv1.RoutingPreset], error) {
	id := req.Msg.Id
	var reset *presets.Preset
	err := s.presetRepo.Modify(func(list []*presets.Preset) ([]*presets.Preset, error) {
		i, err := editableIndex(list, id)
		if err != nil {
			return nil, err
		}
		stored := list[i]
		src, ok, err := s.builtinPreset(stored.SourceID)
		if err != nil {
			return nil, err
		}
		if stored.SourceID == "" || !ok {
			return nil, failedPrecondition("у набора %q нет встроенного источника, сбрасывать нечего", id)
		}
		fresh := presets.NewCustomFrom(src, id)
		next := stored.Clone()
		next.Routing, next.DNS = fresh.Routing, fresh.DNS
		next.SourceID, next.SourceRevision = src.ID, src.Revision
		// custom-правила встроенного — {"rules":[…]}: в хранимый вид, как при записи.
		if err := next.PrepareUser(); err != nil {
			return nil, &presetInvalidError{err}
		}
		next.Revision = stored.Revision + 1
		reset, list[i] = next, next
		return list, nil
	})
	return s.presetReply(reset, err)
}
