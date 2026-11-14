// SPDX-License-Identifier: GPL-3.0-only

package api

// migration_run.go — перенос из NekoBox: Scan → LoadState → Remap → наборы и
// настройки → Apply. Один и тот же код ведёт сухой прогон и настоящий перенос,
// поэтому числа предпросмотра совпадают с отчётом переноса. Им пользуются
// MigrationService и подкоманда `lavray-core import-nekobox`.

import (
	"sync"

	"github.com/anatolijlaptev1991-ctrl/lavray-android/core/internal/presets"
	"github.com/anatolijlaptev1991-ctrl/lavray-android/core/internal/store"
	"github.com/anatolijlaptev1991-ctrl/lavray-android/core/internal/store/nekoimport"
)

// NekoRunOptions — что переносить и пишет ли перенос в базу.
type NekoRunOptions struct {
	Routes   bool // routes_box/ → пользовательские наборы
	Settings bool // groups/nekobox.json → настройки
	DryRun   bool // посчитать и не писать
}

// migrator — перенос поверх хранилища и репозитория наборов.
type migrator struct {
	st      *store.Store
	presets *presetRepo
	// busy — один перенос за раз (importMu): второй получает ABORTED.
	busy sync.Mutex
}

func newMigrator(st *store.Store, repo *presetRepo) *migrator {
	return &migrator{st: st, presets: repo}
}

// run переносит каталог path (config или каталог установки) и возвращает отчёт.
func (m *migrator) run(path string, o NekoRunOptions) (*nekoimport.Report, error) {
	if !m.busy.TryLock() {
		return nil, abortedImport()
	}
	defer m.busy.Unlock()
	dir, err := validateNekoPath(path)
	if err != nil {
		return nil, err
	}
	plan, err := nekoimport.Scan(dir)
	if err != nil {
		return nil, mapMigrationErr(err)
	}
	st, err := nekoimport.LoadState(m.st)
	if err != nil {
		return nil, mapMigrationErr(err)
	}
	res := nekoimport.Remap(plan, st)
	res.Report.DryRun = o.DryRun
	res.Report.RoutesFound = len(plan.Routes)
	planSettings(res, o.Settings)
	// Наборы читаются и пишутся под мьютексом репозитория: параллельный
	// UpdatePreset не затрёт запись переноса.
	err = m.presets.WithLock(func(current []*presets.Preset) error {
		presetsJSON, err := planRoutes(res, plan, st, current, o.Routes)
		if err != nil || o.DryRun {
			return err
		}
		return nekoimport.Apply(m.st, res, presetsJSON)
	})
	if err != nil {
		return nil, mapMigrationErr(err)
	}
	return &res.Report, nil
}

// RunNekoBoxImport — перенос для командной строки: без сервера, со своим
// репозиторием наборов. Приложение при этом не должно работать с тем же каталогом
// данных: его память о наборах и настройках устареет до перезапуска.
func RunNekoBoxImport(st *store.Store, path string, o NekoRunOptions) (*nekoimport.Report, error) {
	return newMigrator(st, newPresetRepo(st)).run(path, o)
}
