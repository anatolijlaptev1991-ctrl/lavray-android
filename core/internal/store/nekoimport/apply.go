// SPDX-License-Identifier: GPL-3.0-only

package nekoimport

import (
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/anatolijlaptev1991-ctrl/lavray-android/core/internal/store"
)

// apply.go — Apply: запись результата Remap одной транзакцией. Все чтения
// сделаны заранее (State), внутри InTx только запись: ошибка на любом шаге
// откатывает группы, профили, import_map, настройки и наборы вместе.

// ErrConflict — база изменилась между чтением State и записью (параллельная
// запись заняла id): перенос не выполнен, его можно повторить (ABORTED).
var ErrConflict = errors.New("база изменилась во время переноса")

// Apply записывает res в базу. presetsJSON — новое значение
// meta['routing.custom_presets'] (пусто — наборы не меняются); res.PresetRows и
// res.Settings заполняет вызывающий слой.
func Apply(s *store.Store, res *Resolved, presetsJSON string) error {
	err := s.InTx(func(tx *store.Store) error { return applyAll(tx, res, presetsJSON) })
	if err != nil && isRace(err) {
		return fmt.Errorf("%w: %v", ErrConflict, err)
	}
	return err
}

func isRace(err error) bool {
	msg := err.Error()
	return strings.Contains(msg, "UNIQUE constraint failed") || strings.Contains(msg, "PRIMARY KEY")
}

func applyAll(tx *store.Store, res *Resolved, presetsJSON string) error {
	if err := applyEntities(tx, res); err != nil {
		return err
	}
	for _, s := range res.Settings {
		if err := tx.SetSetting(s.Key, s.JSON); err != nil {
			return fmt.Errorf("nekoimport: setting %s: %w", s.Key, err)
		}
	}
	if presetsJSON != "" {
		if err := tx.SetMeta(store.MetaCustomPresets, presetsJSON); err != nil {
			return fmt.Errorf("nekoimport: presets: %w", err)
		}
	}
	// import_map — последним: сбой в любой записи откатывает всё выше.
	return applyMapRows(tx, res)
}

// applyEntities: группы, профили, затем front_proxy_id (FK на profiles) и
// отметки выданных id.
func applyEntities(tx *store.Store, res *Resolved) error {
	var maxGroup, maxProfile int64
	for _, ng := range res.Groups {
		g := *ng.Group
		if err := tx.ImportGroup(&g); err != nil {
			return fmt.Errorf("nekoimport: create group %d: %w", g.ID, err)
		}
		maxGroup = max(maxGroup, g.ID)
	}
	for _, np := range res.Profiles {
		p := *np.Profile
		if err := tx.ImportProfile(&p); err != nil {
			return fmt.Errorf("nekoimport: create profile %d: %w", p.ID, err)
		}
		maxProfile = max(maxProfile, p.ID)
	}
	for _, f := range res.Fronts {
		v := f.ProfileID
		if err := tx.SetGroupFrontProxy(f.GroupID, &v); err != nil { // принимает и группу с id 0
			return fmt.Errorf("nekoimport: set front proxy of group %d: %w", f.GroupID, err)
		}
	}
	if len(res.Groups) > 0 {
		if err := tx.RaiseIDFloor("groups", maxGroup); err != nil {
			return err
		}
	}
	if len(res.Profiles) > 0 {
		return tx.RaiseIDFloor("profiles", maxProfile)
	}
	return nil
}

// applyMapRows записывает import_map: что перенесено и под каким id.
func applyMapRows(tx *store.Store, res *Resolved) error {
	put := func(kind, key string, id int64) error {
		return tx.PutImportMap(store.ImportMapRow{
			Source: SourceNekoBox, Kind: kind, SourceKey: key, LocalID: strconv.FormatInt(id, 10)})
	}
	for _, ng := range res.Groups {
		if err := put(KindGroup, ng.Key, ng.Group.ID); err != nil {
			return err
		}
	}
	for _, np := range res.Profiles {
		if err := put(KindProfile, np.Key, np.Profile.ID); err != nil {
			return err
		}
	}
	for _, row := range res.PresetRows {
		if err := tx.PutImportMap(row); err != nil {
			return err
		}
	}
	return nil
}
