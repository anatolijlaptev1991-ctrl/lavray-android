// SPDX-License-Identifier: GPL-3.0-only

// presets_apply.go — применение обновлённых встроенных наборов (UPDATE-SYSTEM
// §8, T-75/7.5). Набор приходит как файл, чей sha256 пинит подписанный манифест;
// принимается только если: id встроенный, ревизия файла равна ревизии манифеста
// и выше установленной, набор проходит presets.Validate. Весь набор файлов
// проверяется до записи; запись — каталогом переопределений целиком
// (<данные>/presets/override), с прошлой ревизией в override.prev для отката.
// Пользовательские копии не затрагиваются: они живут в базе, а не здесь.
package updates

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"github.com/anatolijlaptev1991-ctrl/lavray-android/core/internal/presets"
)

// MaxPresetBytes — предел размера файла набора из обновления.
const MaxPresetBytes = presets.MaxImportBytes

// MaxPresetRev — потолок ревизии набора в манифесте и в файле набора.
const MaxPresetRev = presets.MaxRevision

// PresetFile — скачанный файл набора id (sha256 ещё не проверен).
type PresetFile struct {
	ID   string
	Data []byte
}

// PresetChange — смена действующей ревизии набора.
type PresetChange struct {
	ID       string
	From, To int
}

// PresetOffer — предложение обновить набор: ревизия манифеста выше установленной.
type PresetOffer struct {
	ID        string
	Installed int
	Rev       int
}

// ApplyPresets проверяет все файлы и записывает их переопределениями в dir.
// Любая ошибка проверки или записи оставляет dir как было.
func ApplyPresets(dir string, mf *Manifest, files []PresetFile) ([]PresetChange, error) {
	if len(files) == 0 {
		return nil, fmt.Errorf("%w: нет файлов наборов", ErrPresetRejected)
	}
	installed, err := presets.BuiltinPresetsWith(dir)
	if err != nil {
		return nil, err
	}
	accepted, changes, err := vetPresets(mf, installed, files)
	if err != nil {
		return nil, err
	}
	CleanSwapLeftovers(dir)
	if err := writeOverrides(dir, installed, accepted); err != nil {
		return nil, err
	}
	return changes, nil
}

// vetPresets проверяет файлы; возвращает id → содержимое и список смен ревизий.
func vetPresets(mf *Manifest, installed []*presets.Preset, files []PresetFile) (map[string][]byte, []PresetChange, error) {
	cur := make(map[string]int, len(installed))
	for _, p := range installed {
		cur[p.ID] = p.Revision
	}
	accepted := make(map[string][]byte, len(files))
	var changes []PresetChange
	for _, f := range files {
		rev, ok := cur[f.ID]
		if !ok {
			return nil, nil, fmt.Errorf("%w: %q не встроенный набор", ErrPresetRejected, f.ID)
		}
		if _, dup := accepted[f.ID]; dup {
			return nil, nil, fmt.Errorf("%w: %q передан дважды", ErrPresetRejected, f.ID)
		}
		if err := vetPreset(mf, f, rev); err != nil {
			return nil, nil, err
		}
		accepted[f.ID] = f.Data
		changes = append(changes, PresetChange{ID: f.ID, From: rev, To: int(mf.Presets[f.ID].Rev)})
	}
	sort.Slice(changes, func(i, j int) bool { return changes[i].ID < changes[j].ID })
	return accepted, changes, nil
}

// vetPreset проверяет один файл: пин манифеста, размер, схема, согласие id/ревизии.
func vetPreset(mf *Manifest, f PresetFile, installedRev int) error {
	entry, ok := mf.Presets[f.ID]
	if !ok {
		return fmt.Errorf("%w: %q нет в подписанном манифесте", ErrPresetRejected, f.ID)
	}
	if len(f.Data) > MaxPresetBytes {
		return fmt.Errorf("%w: %q больше %d байт", ErrPresetRejected, f.ID, MaxPresetBytes)
	}
	if err := CheckSha256(f.Data, entry.Sha256); err != nil {
		return err
	}
	p, err := presets.LoadPreset(f.Data)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrPresetRejected, err)
	}
	if p.ID != f.ID || !p.Builtin || int64(p.Revision) != entry.Rev {
		return fmt.Errorf("%w: %q: id, builtin или ревизия файла не согласованы с манифестом", ErrPresetRejected, f.ID)
	}
	if p.Revision <= installedRev {
		return fmt.Errorf("%w: %q: rev %d при установленной %d", ErrPresetNotNewer, f.ID, p.Revision, installedRev)
	}
	return nil
}

// writeOverrides собирает новый каталог (действующие переопределения + принятые
// файлы) рядом с dir и подменяет им dir.
func writeOverrides(dir string, installed []*presets.Preset, accepted map[string][]byte) error {
	next, err := NewSiblingDir(dir)
	if err != nil {
		return err
	}
	if err := fillOverrides(dir, next, installed, accepted); err != nil {
		_ = os.RemoveAll(next)
		return err
	}
	syncDir(next)
	// Каталог переопределений должен существовать: тогда его прежнее (пустое)
	// состояние уходит в .prev и откат первого применения возвращает встроенные наборы.
	if err := os.MkdirAll(dir, 0o700); err != nil {
		_ = os.RemoveAll(next)
		return err
	}
	if err := SwapDir(dir, next); err != nil {
		_ = os.RemoveAll(next)
		return err
	}
	return nil
}

func fillOverrides(dir, next string, installed []*presets.Preset, accepted map[string][]byte) error {
	embedded, err := presets.BuiltinPresets()
	if err != nil {
		return err
	}
	base := make(map[string]int, len(embedded))
	for _, p := range embedded {
		base[p.ID] = p.Revision
	}
	for _, p := range installed {
		data, fresh := accepted[p.ID]
		if !fresh {
			if p.Revision <= base[p.ID] {
				continue // действует встроенный набор: переопределения нет
			}
			var rerr error
			if data, rerr = os.ReadFile(filepath.Join(dir, p.ID+".json")); rerr != nil {
				return rerr
			}
		}
		if err := writeFileSynced(filepath.Join(next, p.ID+".json"), data); err != nil {
			return err
		}
	}
	return nil
}

// RollbackPresets возвращает прошлую ревизию переопределений. ErrNoPrevious —
// применений не было или откат уже выполнен.
func RollbackPresets(dir string) error { return RollbackDir(dir) }
