// SPDX-License-Identifier: GPL-3.0-only

package api

// log_filter.go — фильтр строк журнала по запросу SubscribeLogs.

import (
	"strings"

	lavrayv1 "github.com/anatolijlaptev1991-ctrl/lavray-android/core/internal/gen/lavrayv1"
	"github.com/anatolijlaptev1991-ctrl/lavray-android/core/internal/logs"
)

// containsAny — message содержит любое из слов без учёта регистра.
func containsAny(message string, words []string) bool {
	low := strings.ToLower(message)
	for _, w := range words {
		if strings.Contains(low, strings.ToLower(w)) {
			return true
		}
	}
	return false
}

// logFilter — условие запроса: min_level, sources, exclude/include_keywords.
func logFilter(msg *lavrayv1.SubscribeLogsRequest) func(logs.Line) bool {
	minLevel := lavrayv1.LogLevel_LOG_LEVEL_TRACE
	if msg.MinLevel != nil {
		minLevel = *msg.MinLevel
	}
	srcSet := map[string]bool{}
	for _, src := range msg.Sources {
		srcSet[src] = true
	}
	return func(l logs.Line) bool {
		if logLevelToProto(l.Level) < minLevel {
			return false
		}
		if len(srcSet) > 0 && !srcSet[l.Source] {
			return false
		}
		if len(msg.ExcludeKeywords) > 0 && containsAny(l.Message, msg.ExcludeKeywords) {
			return false
		}
		return len(msg.IncludeKeywords) == 0 || containsAny(l.Message, msg.IncludeKeywords)
	}
}
