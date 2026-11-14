// SPDX-License-Identifier: GPL-3.0-only

package api

// conn_list.go — отбор, порядок и страницы списка соединений (T-79): общее для
// ListConnections и WatchConnections.

import (
	"sort"
	"strings"

	lavrayv1 "github.com/anatolijlaptev1991-ctrl/lavray-android/core/internal/gen/lavrayv1"
	"github.com/anatolijlaptev1991-ctrl/lavray-android/core/internal/traffic"
)

const (
	// maxConnPage — больше соединений в одном ответе не отдаётся.
	maxConnPage = 5000
	// defaultConnPage — размер страницы, если limit не задан (контракт Page).
	defaultConnPage = 100
)

// connFilter — подстроки без учёта регистра; пустая не отбирает ничего.
type connFilter struct{ outbound, destination string }

func newConnFilter(outbound, destination *string) connFilter {
	return connFilter{
		outbound:    strings.ToLower(optStr(outbound)),
		destination: strings.ToLower(optStr(destination)),
	}
}

// match: тег исходящего содержит outbound, адрес назначения или имя из
// сниффинга содержит destination.
func (f connFilter) match(c traffic.ConnInfo) bool {
	if f.outbound != "" && !strings.Contains(strings.ToLower(c.Outbound), f.outbound) {
		return false
	}
	if f.destination == "" {
		return true
	}
	return strings.Contains(strings.ToLower(c.Destination), f.destination) ||
		strings.Contains(strings.ToLower(c.Domain), f.destination)
}

// sortedActive — подходящие активные соединения, сначала новые.
func sortedActive(src traffic.Source, f connFilter) []traffic.ConnInfo {
	out := filterConns(src.Active(), f)
	sort.Slice(out, func(i, j int) bool {
		if out[i].StartMs != out[j].StartMs {
			return out[i].StartMs > out[j].StartMs
		}
		return out[i].ID < out[j].ID
	})
	return out
}

// listConns — активные (сначала новые), затем недавно закрытые (сначала
// закрывшиеся последними).
func listConns(src traffic.Source, f connFilter) []traffic.ConnInfo {
	closed := filterConns(src.Closed(), f)
	sort.Slice(closed, func(i, j int) bool {
		if closed[i].EndMs != closed[j].EndMs {
			return closed[i].EndMs > closed[j].EndMs
		}
		return closed[i].ID < closed[j].ID
	})
	return append(sortedActive(src, f), closed...)
}

func filterConns(in []traffic.ConnInfo, f connFilter) []traffic.ConnInfo {
	out := make([]traffic.ConnInfo, 0, len(in))
	for _, c := range in {
		if f.match(c) {
			out = append(out, c)
		}
	}
	return out
}

// connPage проверяет страницу запроса и возвращает смещение и размер. Без
// страницы или с limit = 0 — defaultConnPage; отрицательные значения и размер
// больше maxConnPage — INVALID_ARGUMENT.
func connPage(p *lavrayv1.Page) (offset, limit int, err error) {
	if p == nil {
		return 0, defaultConnPage, nil
	}
	if p.Offset < 0 {
		return 0, 0, invalidArg("page.offset не может быть отрицательным")
	}
	if p.Limit < 0 || p.Limit > maxConnPage {
		return 0, 0, invalidArg("page.limit должен быть от 0 до %d", maxConnPage)
	}
	limit = int(p.Limit)
	if limit == 0 {
		limit = defaultConnPage
	}
	return int(min(p.Offset, 1<<31-1)), limit, nil
}

// pageOfConns вырезает страницу; смещение за концом списка — пустая страница.
func pageOfConns(items []traffic.ConnInfo, offset, limit int) []traffic.ConnInfo {
	if offset >= len(items) {
		return nil
	}
	return items[offset:min(len(items), offset+limit)]
}
