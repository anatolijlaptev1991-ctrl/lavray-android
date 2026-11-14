// SPDX-License-Identifier: GPL-3.0-only

package api

// migration_settings.go — настройки из NekoBox проходят тот же валидатор, что и
// SetSettings (тип по дескриптору, диапазоны, списки допустимых значений):
// негодное значение не записывается, а попадает в предупреждения.

import (
	"errors"
	"fmt"

	"connectrpc.com/connect"

	"github.com/anatolijlaptev1991-ctrl/lavray-android/core/internal/store/nekoimport"
)

// planSettings оставляет в res.Settings только допустимые значения и записывает
// ключи в отчёт. include == false — настройки не переносятся вовсе.
func planSettings(res *nekoimport.Resolved, include bool) {
	rep := &res.Report
	if !include {
		res.Settings = nil
		return
	}
	var ok []nekoimport.PlanSetting
	for _, s := range res.Settings {
		d, known := descriptorMap[s.Key]
		if !known || d.Internal {
			rep.Warnings = append(rep.Warnings, fmt.Sprintf("настройка %s не перенесена: такого ключа нет в LAVRay", s.Key))
			continue
		}
		if err := validateSetting(s.Key, d, typedSettingValue(s.Key, s.JSON)); err != nil {
			rep.Warnings = append(rep.Warnings, fmt.Sprintf("настройка %s не перенесена: %s", s.Key, errMessage(err)))
			continue
		}
		ok = append(ok, s)
		rep.SettingsImported = append(rep.SettingsImported, s.Key)
	}
	res.Settings = ok
}

// errMessage — текст ошибки Connect без кода («invalid_argument: …»).
func errMessage(err error) string {
	var ce *connect.Error
	if errors.As(err, &ce) {
		return ce.Message()
	}
	return err.Error()
}
