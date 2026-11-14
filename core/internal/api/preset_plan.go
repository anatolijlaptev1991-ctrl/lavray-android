// SPDX-License-Identifier: GPL-3.0-only

package api

// preset_plan.go — PlanInsert: расчёт вставки нового пользовательского набора.
// Чистая функция: ничего не читает из хранилища и не пишет в него, поэтому её
// используют и CreatePreset/ImportPreset, и перенос из NekoBox (T-78): тот
// вставляет пачку наборов, передавая в existing результат предыдущего вызова
// (InsertPlan.List), а запись делает одной транзакцией сам.

import (
	"fmt"

	"github.com/anatolijlaptev1991-ctrl/lavray-android/core/internal/presets"
)

// InsertPlan — итог PlanInsert.
type InsertPlan struct {
	// Preset — вставляемый набор: копия входного с присвоенным id.
	Preset *presets.Preset
	// List — весь список пользовательских наборов после вставки (новый срез;
	// existing не меняется).
	List []*presets.Preset
}

// PlanInsert считает вставку набора in в список existing.
//
// Что делает:
//   - присваивает свободный id `custom:<slug>` по имени набора; id из in игнорируется
//     и занять id встроенного набора или уже существующего не может;
//   - ставит builtin=false, ревизию 1, текущую версию схемы и $schema;
//   - приводит набор к хранимому виду и проверяет его (Preset.PrepareUser).
//
// Чего не делает: источник копии (SourceID, SourceRevision) не трогает — его
// решает вызывающий (копия встроенного сохраняет, импорт из файла очищает).
//
// Ошибки: presets.ErrLimit — уже MaxCustomPresets наборов (RESOURCE_EXHAUSTED);
// *presetInvalidError — набор не прошёл проверку (INVALID_ARGUMENT).
func PlanInsert(existing []*presets.Preset, in *presets.Preset) (InsertPlan, error) {
	if len(existing) >= presets.MaxCustomPresets {
		return InsertPlan{}, fmt.Errorf("%w: уже %d наборов из %d", presets.ErrLimit, len(existing), presets.MaxCustomPresets)
	}
	reserved, err := presets.BuiltinPresets()
	if err != nil {
		return InsertPlan{}, err
	}
	taken := func(id string) bool {
		if indexOfPreset(existing, id) >= 0 {
			return true
		}
		for _, b := range reserved {
			if b.ID == id {
				return true
			}
		}
		return false
	}
	p := in.Clone()
	p.ID = presets.MintID(presetDisplayName(p), taken)
	p.Builtin = false
	p.Revision = 1
	p.Version, p.Schema = presets.SchemaVersion, presets.SchemaRef
	if err := p.PrepareUser(); err != nil {
		return InsertPlan{}, &presetInvalidError{err}
	}
	list := make([]*presets.Preset, 0, len(existing)+1)
	list = append(list, existing...)
	return InsertPlan{Preset: p, List: append(list, p)}, nil
}
