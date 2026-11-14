// SPDX-License-Identifier: GPL-3.0-only

package api

import (
	"encoding/json"

	"github.com/anatolijlaptev1991-ctrl/lavray-android/core/internal/backup"
	lavrayv1 "github.com/anatolijlaptev1991-ctrl/lavray-android/core/internal/gen/lavrayv1"
	"github.com/anatolijlaptev1991-ctrl/lavray-android/core/internal/store"
)

// maxAppVersionLen — предел строки версии приложения в сводке.
const maxAppVersionLen = 64

// summaryDoc — JSON-сводка внутри копии (backup.Pack). Числа дублируют
// содержимое снимка, но источником истины при показе служит сам снимок
// (store.InspectCopy); из сводки берутся то, чего в БД нет: версия
// приложения и время создания.
type summaryDoc struct {
	AppVersion    string `json:"app_version"`
	SchemaVersion int64  `json:"schema_version"`
	CreatedAtMs   int64  `json:"created_at_ms"`
	Groups        int    `json:"groups"`
	Profiles      int    `json:"profiles"`
	Subscriptions int    `json:"subscriptions"`
	Presets       int    `json:"presets"`
	Settings      int    `json:"settings"`
}

// newSummaryDoc собирает сводку создаваемой копии по содержимому снимка.
func newSummaryDoc(appVersion string, createdAtMs int64, info store.CopyInfo) summaryDoc {
	st := info.Stats
	return summaryDoc{
		AppVersion: appVersion, SchemaVersion: info.SchemaVersion, CreatedAtMs: createdAtMs,
		Groups: st.Groups, Profiles: st.Profiles, Subscriptions: st.Subscriptions,
		Presets: st.Presets, Settings: st.Settings,
	}
}

// summaryProto — BackupSummary из сводки.
func (d summaryDoc) summaryProto() *lavrayv1.BackupSummary {
	return &lavrayv1.BackupSummary{
		Groups: int32(d.Groups), Profiles: int32(d.Profiles), Subscriptions: int32(d.Subscriptions),
		Presets: int32(d.Presets), Settings: int32(d.Settings),
		AppVersion: d.AppVersion, SchemaVersion: d.SchemaVersion, CreatedAtMs: d.CreatedAtMs,
	}
}

// withContent заменяет числа и версию схемы данными реального снимка.
func (d summaryDoc) withContent(info store.CopyInfo) summaryDoc {
	return newSummaryDoc(d.AppVersion, d.CreatedAtMs, info)
}

// parseSummaryDoc разбирает сводку из тела копии; пустая или битая — ErrCorrupt.
func parseSummaryDoc(raw []byte) (summaryDoc, error) {
	var d summaryDoc
	if err := json.Unmarshal(raw, &d); err != nil {
		return summaryDoc{}, backup.ErrCorrupt
	}
	// версия приложения показывается в окне: длинная строка из чужой копии не нужна
	if len(d.AppVersion) > maxAppVersionLen || d.CreatedAtMs < 0 || d.SchemaVersion < 0 {
		return summaryDoc{}, backup.ErrCorrupt
	}
	return d, nil
}

// liveSummary — сводка по текущему содержимому хранилища (после восстановления).
func liveSummary(base summaryDoc, st store.Stats, schemaVersion int64) *lavrayv1.BackupSummary {
	return base.withContent(store.CopyInfo{Stats: st, SchemaVersion: schemaVersion}).summaryProto()
}
