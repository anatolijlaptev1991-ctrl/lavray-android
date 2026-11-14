// SPDX-License-Identifier: GPL-3.0-only

package cfgbuild

import "strings"

// validLogLevels — уровни журнала sing-box (option.LogOptions.Level). Любое
// другое значение sing-box отвергает при старте («unknown log level»), поэтому
// настройка log.level нормализуется здесь, а не доходит до раннера.
var validLogLevels = map[string]bool{
	"trace": true, "debug": true, "info": true, "warn": true,
	"error": true, "fatal": true, "panic": true,
}

// normalizeLogLevel приводит настройку log.level к допустимому уровню:
// регистр и пробелы игнорируются, пустое и неизвестное значение — "info".
func normalizeLogLevel(level string) string {
	l := strings.ToLower(strings.TrimSpace(level))
	if validLogLevels[l] {
		return l
	}
	return "info"
}
