// SPDX-License-Identifier: GPL-3.0-only

// Package nekoimport — перенос каталога NekoBox/NekoRay в LAVRay
// (core-port-plan §5.3, FR-UI-277, T-78).
//
// Читает <config_dir>/groups/*.json и <config_dir>/profiles/*.json в формате
// ui/db/Database.cpp (LoadManager): groups/pm.json — порядок вкладок,
// groups/nekobox.json — DataStore (настройки, settings_map.go),
// groups/<id>.json — группы, profiles/<id>.json — ProxyEntity
// {type,id,gid,yc,report,bean,traffic}, routes_box/<имя> — маршруты. Старый
// формат, где группа хранилась профилем с type=group, тоже принимается.
//
// Перенос идёт в четыре шага, каждый — отдельный модуль:
//
//	Scan      — разбор каталога без записи и без базы (scan.go);
//	LoadState — чтение того, что уже есть в базе (state.go);
//	Remap     — итоговые id, переписанные цепочки и front_proxy (remap.go);
//	Apply     — запись одной транзакцией (apply.go).
//
// Исходный каталог только читается. Id NekoBox сохраняются, пока свободны;
// занятый id получает новый. Повторный перенос идемпотентен по import_map
// (identity.go). Неизвестные типы и битые файлы попадают в отчёт (Skipped), не
// прерывая остальных.
package nekoimport

import "github.com/anatolijlaptev1991-ctrl/lavray-android/core/internal/store"

// Import переносит профили и группы каталога configDir в хранилище s. Маршруты и
// настройки переносит слой API (у него есть репозиторий наборов и валидатор).
func Import(s *store.Store, configDir string) (*Report, error) {
	plan, err := Scan(configDir)
	if err != nil {
		return nil, err
	}
	st, err := LoadState(s)
	if err != nil {
		return nil, err
	}
	res := Remap(plan, st)
	if err := Apply(s, res, ""); err != nil {
		return nil, err
	}
	return &res.Report, nil
}
