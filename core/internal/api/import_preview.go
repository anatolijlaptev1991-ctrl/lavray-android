// SPDX-License-Identifier: GPL-3.0-only

package api

// import_preview.go — ответ предпросмотра импорта (ImportContent): разобранные
// профили без id, предупреждения о custom-полях и строки журнала «код: текст»,
// по которым UI выбирает перевод. Вынесено из profile_service.go (трещотка).

import (
	"fmt"
	"strings"

	"github.com/anatolijlaptev1991-ctrl/lavray-android/core/internal/cfgbuild"
	lavrayv1 "github.com/anatolijlaptev1991-ctrl/lavray-android/core/internal/gen/lavrayv1"
	"github.com/anatolijlaptev1991-ctrl/lavray-android/core/internal/model"
	"github.com/anatolijlaptev1991-ctrl/lavray-android/core/internal/subs/parser"
)

// previewImportResponse собирает ответ по результату разбора. Заодно ставит
// страну профилям без неё (FR-UI-008): ядро определяет ISO-код по имени
// (флаг/код/название) или TLD — UI показывает ключи сгруппированными.
func previewImportResponse(content string, res *parser.Result) *lavrayv1.ImportContentResponse {
	for _, p := range res.Profiles {
		if p.Geo == "" {
			p.Geo = model.DetectGeo(p.Name, p.Address)
		}
	}
	parsed := make([]*lavrayv1.Profile, 0, len(res.Profiles))
	var warns []*lavrayv1.CustomFieldWarning
	for i, p := range res.Profiles {
		pp := profileToProto(p)
		pp.Id = 0 // preview: не сохранён
		parsed = append(parsed, pp)
		if len(res.Warnings) > 0 {
			warns = append(warns, &lavrayv1.CustomFieldWarning{
				ProfileIndex:  int32(i),
				DroppedFields: customFieldsDropped(p),
				Preview:       strpIf(strings.Join(res.Warnings, "; ")),
			})
		}
	}
	return &lavrayv1.ImportContentResponse{
		Kind:                contentKindOf(content, res.Format),
		ParsedProfiles:      parsed,
		CustomFieldWarnings: warns,
		Log:                 unsupportedImportLog(res.Profiles),
	}
}

// unsupportedImportLog — по строке на профиль, который ядро не соберёт
// (сейчас — XHTTP): «xhttp_unsupported: …». В строке только имя профиля из
// ссылки, секретов (пароль, UUID) нет.
func unsupportedImportLog(list []*model.Profile) []string {
	var out []string
	for _, p := range list {
		if reason := cfgbuild.UnsupportedReason(p); reason != "" {
			out = append(out, fmt.Sprintf("%s: профиль «%s» использует транспорт, который ядро пока не поддерживает; подключиться к нему не получится", reason, p.Name))
		}
	}
	return out
}
