// SPDX-License-Identifier: GPL-3.0-only

package nekoimport

import (
	"fmt"

	"github.com/anatolijlaptev1991-ctrl/lavray-android/core/internal/model"
	"github.com/anatolijlaptev1991-ctrl/lavray-android/core/internal/presets"
)

// plan.go — типы переноса. Plan — что лежит в каталоге NekoBox (Scan, без
// записи и без обращения к базе); Resolved (remap.go) — что из этого станет в
// базе; Report — итог для пользователя. Один и тот же код строит Report для
// сухого прогона и для настоящего переноса, поэтому числа совпадают.

// Skipped — файл, не перенесённый в хранилище (битый JSON, неизвестный тип,
// расхождение id и пр.). Отчёт о потерях — не тихо (FR-UI-260).
type Skipped struct {
	File   string `json:"file"`
	ID     int64  `json:"id,omitempty"`
	Type   string `json:"type,omitempty"`
	Reason string `json:"reason"`
}

// Report — сводка переноса: что перенесено, что пропущено.
type Report struct {
	GroupsImported   int            `json:"groups_imported"`
	OldFmtGroups     int            `json:"old_format_groups"` // группы из profiles/ type=group
	ProfilesByType   map[string]int `json:"profiles_by_type"`  // перенесено по типам
	AlreadyPresent   int            `json:"already_present"`   // уже перенесено раньше (повторный перенос)
	Renumbered       int            `json:"renumbered"`        // id сменён из-за коллизии
	Skipped          []Skipped      `json:"skipped,omitempty"` // не перенесено (с причинами)
	Warnings         []string       `json:"warnings,omitempty"`
	RoutesFound      int            `json:"routes_found"`
	RoutesImported   int            `json:"routes_imported"`
	SettingsImported []string       `json:"settings_imported,omitempty"` // ключи настроек LAVRay
	DryRun           bool           `json:"dry_run"`
	// ActiveRoute — id набора LAVRay, которым стал активный маршрут NekoBox
	// (пусто — не найден или не переносится).
	ActiveRoute string `json:"nekobox_active_route,omitempty"`
}

func newReport() Report { return Report{ProfilesByType: map[string]int{}} }

// ProfilesImported — всего перенесённых профилей (сумма по типам).
func (r *Report) ProfilesImported() int {
	n := 0
	for _, c := range r.ProfilesByType {
		n += c
	}
	return n
}

func (r *Report) skip(file string, id int64, typ, reason string) {
	r.Skipped = append(r.Skipped, Skipped{File: file, ID: id, Type: typ, Reason: reason})
}

func (r *Report) warn(format string, args ...any) {
	r.Warnings = append(r.Warnings, fmt.Sprintf(format, args...))
}

// clone — копия отчёта, не делящая срезы и карту с исходным.
func (r Report) clone() Report {
	r.ProfilesByType = map[string]int{}
	r.Skipped = append([]Skipped(nil), r.Skipped...)
	r.Warnings = append([]string(nil), r.Warnings...)
	r.SettingsImported = append([]string(nil), r.SettingsImported...)
	return r
}

// PlanGroup — группа каталога NekoBox.
type PlanGroup struct {
	NekoID     int64
	Key        string
	Group      *model.Group // ID == NekoID; FrontProxyID не задан (см. FrontProxy)
	FrontProxy int64        // id профиля NekoBox или -1
	// Synthetic — группы нет в источнике: перенос создаёт «Imported» для
	// профилей каталога без групп. Id ей всегда выдаётся заново.
	Synthetic bool
}

// PlanProfile — профиль каталога NekoBox.
type PlanProfile struct {
	NekoID      int64
	NekoGroupID int64
	Key         string
	Profile     *model.Profile // ID == NekoID, GroupID == NekoGroupID
}

// PlanRoute — маршрут routes_box/ (или старого routes/), уже разобранный в набор.
type PlanRoute struct {
	Name     string
	File     string // относительный путь для отчёта
	Key      string
	Preset   *presets.Preset // без итогового id: его выдаёт api.PlanInsert
	Warnings []string
}

// RefKind — на что ссылается значение настройки NekoBox.
type RefKind uint8

const (
	RefNone    RefKind = iota
	RefProfile         // id профиля NekoBox: после переноса заменяется локальным
	RefGroup           // id группы NekoBox
)

// PlanSetting — настройка LAVRay, полученная из groups/nekobox.json.
type PlanSetting struct {
	Key   string // ключ LAVRay
	JSON  string // значение как JSON (тип — из дескриптора)
	Ref   RefKind
	RefID int64 // id NekoBox при Ref != RefNone
}

// Plan — разобранный каталог NekoBox. Строится без записи и без базы.
type Plan struct {
	Dir      string
	Groups   []PlanGroup   // в порядке создания: pm.json, затем по id
	Profiles []PlanProfile // в порядке вставки (порядок отображения групп)
	Routes   []PlanRoute
	Settings []PlanSetting
	// ActiveRoute — имя активного маршрута NekoBox (active_routing).
	ActiveRoute string
	// Report — пропуски и предупреждения разбора; Remap достраивает остальное.
	Report Report
}
