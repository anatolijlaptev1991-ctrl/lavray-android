// SPDX-License-Identifier: GPL-3.0-only

package api

// preset_repo.go — хранилище пользовательских наборов: один JSON-массив в
// meta['routing.custom_presets']. Чтение-изменение-запись идёт под мьютексом
// (presetRepo.Modify), поэтому два параллельных UpdatePreset не затирают друг
// друга; проверку ревизии делает вызывающий внутри fn.

import (
	"encoding/json"
	"fmt"
	"strings"
	"sync"

	"github.com/anatolijlaptev1991-ctrl/lavray-android/core/internal/presets"
	"github.com/anatolijlaptev1991-ctrl/lavray-android/core/internal/store"
)

// metaCustomPresets — ключ meta со списком пользовательских наборов (JSON []presets.Preset).
const metaCustomPresets = "routing.custom_presets"

// presetRepo — пользовательские наборы поверх store.Store.
type presetRepo struct {
	mu sync.Mutex
	st *store.Store
}

func newPresetRepo(st *store.Store) *presetRepo { return &presetRepo{st: st} }

// read читает список (вызывать под mu). Повреждённая запись — DATA_LOSS:
// молча принять её за «наборов нет» значило бы затереть её следующей записью.
func (r *presetRepo) read() ([]*presets.Preset, error) {
	raw, ok, err := r.st.GetMeta(metaCustomPresets)
	if err != nil {
		return nil, err
	}
	if !ok || strings.TrimSpace(raw) == "" {
		return nil, nil
	}
	var out []*presets.Preset
	if err := json.Unmarshal([]byte(raw), &out); err != nil {
		return nil, presetsDataLoss("повреждена запись пользовательских наборов (meta %s): %v", metaCustomPresets, err)
	}
	for i, p := range out {
		if p == nil {
			return nil, presetsDataLoss("повреждена запись пользовательских наборов (meta %s): элемент %d пуст", metaCustomPresets, i)
		}
	}
	return out, nil
}

// List — снимок пользовательских наборов (свежая копия: правки её не затрагивают).
func (r *presetRepo) List() ([]*presets.Preset, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.read()
}

// Modify атомарно читает список, отдаёт его fn и записывает результат.
// Ошибка fn (в том числе ABORTED/NOT_FOUND) ничего не пишет. Результат больше
// MaxAllCustomBytes отвергается (RESOURCE_EXHAUSTED).
func (r *presetRepo) Modify(fn func(list []*presets.Preset) ([]*presets.Preset, error)) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	cur, err := r.read()
	if err != nil {
		return err
	}
	next, err := fn(cur)
	if err != nil {
		return err
	}
	b, err := encodePresets(next)
	if err != nil {
		return err
	}
	return r.st.SetMeta(metaCustomPresets, b)
}

// encodePresets — значение meta для списка наборов; больше MaxAllCustomBytes
// отвергается (RESOURCE_EXHAUSTED).
func encodePresets(list []*presets.Preset) (string, error) {
	if list == nil {
		list = []*presets.Preset{}
	}
	b, err := json.Marshal(list)
	if err != nil {
		return "", err
	}
	if len(b) > presets.MaxAllCustomBytes {
		return "", fmt.Errorf("%w: наборы занимают %d байт, предел %d", presets.ErrLimit, len(b), presets.MaxAllCustomBytes)
	}
	return string(b), nil
}

// WithLock выполняет fn под мьютексом репозитория, отдав ей текущий список.
// Запись fn делает сама (перенос из NekoBox пишет наборы в той же транзакции,
// что профили и группы): пока fn работает, никто другой наборы не меняет.
func (r *presetRepo) WithLock(fn func(current []*presets.Preset) error) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	cur, err := r.read()
	if err != nil {
		return err
	}
	return fn(cur)
}

// Locked выполняет fn над снимком списка под замком репозитория, ничего не
// записывая. Всё, что должно быть согласовано с правкой наборов (активация:
// проверить набор и записать активный id), делается здесь, а не между
// отдельными чтением и записью. fn не должна обращаться к репозиторию.
func (r *presetRepo) Locked(fn func(list []*presets.Preset) error) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	list, err := r.read()
	if err != nil {
		return err
	}
	return fn(list)
}

// indexOfPreset — позиция набора с id в списке или -1.
func indexOfPreset(list []*presets.Preset, id string) int {
	for i, p := range list {
		if p.ID == id {
			return i
		}
	}
	return -1
}

// activeOrGlobal — активный набор; если его id больше не существует (гонка с
// удалением, правка данных), работает встроенный «global».
func (s *Server) activeOrGlobal() (*presets.Preset, error) {
	p, found, err := s.findPreset(s.activePresetID())
	if err != nil || found {
		return p, err
	}
	p, _, err = s.builtinPreset("global")
	return p, err
}

// builtinList — встроенные наборы с переопределениями, доставленными апдейтером
// (<данные>/presets/override, T-75/7.5): набор той же id с более высокой ревизией.
func (s *Server) builtinList() ([]*presets.Preset, error) {
	return presets.BuiltinPresetsWith(s.presetOverrideDir())
}

// isBuiltinID — id принадлежит встроенному набору (состав не зависит от переопределений).
func isBuiltinID(id string) (bool, error) {
	list, err := presets.BuiltinPresets()
	if err != nil {
		return false, err
	}
	for _, p := range list {
		if p.ID == id {
			return true, nil
		}
	}
	return false, nil
}

// builtinPreset — встроенный набор по id (false — такого нет).
func (s *Server) builtinPreset(id string) (*presets.Preset, bool, error) {
	list, err := s.builtinList()
	if err != nil {
		return nil, false, err
	}
	for _, p := range list {
		if p.ID == id {
			return p, true, nil
		}
	}
	return nil, false, nil
}

// findPreset — встроенный или пользовательский набор по id.
func (s *Server) findPreset(id string) (*presets.Preset, bool, error) {
	if id == "" {
		return nil, false, nil
	}
	if p, ok, err := s.builtinPreset(id); err != nil || ok {
		return p, ok, err
	}
	custom, err := s.presetRepo.List()
	if err != nil {
		return nil, false, err
	}
	if i := indexOfPreset(custom, id); i >= 0 {
		return custom[i], true, nil
	}
	return nil, false, nil
}
