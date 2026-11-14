// SPDX-License-Identifier: GPL-3.0-only

package presets

// override.go — переопределения встроенных наборов, доставленные апдейтером
// (UPDATE-SYSTEM §8, T-75/7.5). Файл <данные>/presets/override/<id>.json
// заменяет встроенный набор с тем же id, только если его ревизия выше. Список
// id берётся из встроенных наборов, а не из имён файлов: переопределение не
// может добавить набор, занять чужой id или превратить пользовательский набор
// во встроенный. Негодный файл молча пропускается — побеждает встроенный набор,
// запуск из-за него не ломается.

import (
	"os"
	"path/filepath"
)

// maxOverrideBytes — предел размера одного файла переопределения.
const maxOverrideBytes = MaxImportBytes

// OverrideDir — каталог переопределений наборов в каталоге данных.
func OverrideDir(dataDir string) string {
	return filepath.Join(dataDir, "presets", "override")
}

// BuiltinPresetsWith возвращает встроенные наборы с переопределениями из dir
// (пустой dir — как BuiltinPresets). Порядок и состав те же, что у встроенных.
func BuiltinPresetsWith(dir string) ([]*Preset, error) {
	list, err := BuiltinPresets()
	if err != nil || dir == "" {
		return list, err
	}
	for i, base := range list {
		if o, ok := loadOverride(dir, base); ok {
			list[i] = o
		}
	}
	return list, nil
}

// loadOverride читает переопределение base; false — файла нет или он негоден.
func loadOverride(dir string, base *Preset) (*Preset, bool) {
	path := filepath.Join(dir, base.ID+".json")
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() || info.Size() > maxOverrideBytes {
		return nil, false
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, false
	}
	o, err := LoadPreset(raw)
	if err != nil || o.ID != base.ID || !o.Builtin || o.Revision <= base.Revision {
		return nil, false
	}
	return o, true
}
