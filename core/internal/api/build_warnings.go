// SPDX-License-Identifier: GPL-3.0-only

package api

// build_warnings.go — замечания сборки конфига для ответа Connect и журнала:
// предупреждение режима TUN и то, что сборка пропустила, не прерывая запуск
// (например, негодная устаревшая настройка routing.custom_global).

import (
	"github.com/anatolijlaptev1991-ctrl/lavray-android/core/internal/cfgbuild"
	lavrayv1 "github.com/anatolijlaptev1991-ctrl/lavray-android/core/internal/gen/lavrayv1"
	"github.com/anatolijlaptev1991-ctrl/lavray-android/core/internal/logs"
)

// buildWarnings — предупреждения по собранному конфигу; замечания сборки
// дополнительно пишутся в журнал ядра (уровень warn).
func (s *Server) buildWarnings(built *cfgbuild.BuildResult, mode lavrayv1.ConnectMode) []string {
	out := tunMixedWarning(built.Config, mode) // T-55: по факту конфига
	for _, w := range built.Warnings {
		s.logs.Logf(logs.LevelWarn, logs.SourceCore, "сборка конфига: %s", w)
		out = append(out, w)
	}
	return out
}
