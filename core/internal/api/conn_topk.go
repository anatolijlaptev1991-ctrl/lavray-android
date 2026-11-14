// SPDX-License-Identifier: GPL-3.0-only

package api

// conn_topk.go — выбор первых k соединений в порядке «сначала новые» без
// полной сортировки (R0-D): кадр WatchConnections отдаёт не больше
// active_page_limit строк, а весь список нужен ему только ради id.

import (
	"sort"

	"github.com/anatolijlaptev1991-ctrl/lavray-android/core/internal/traffic"
)

// connBefore — порядок списка соединений: новее раньше, при равенстве меньший id.
func connBefore(a, b *traffic.ConnInfo) bool {
	if a.StartMs != b.StartMs {
		return a.StartMs > b.StartMs
	}
	return a.ID < b.ID
}

// firstConns возвращает первые k элементов списка, отсортированного по
// connBefore, — тот же результат, что sort + срез, за O(n log k). Порядок
// входа не меняется; k <= 0 даёт пустой результат.
func firstConns(in []traffic.ConnInfo, k int) []traffic.ConnInfo {
	if k <= 0 || len(in) == 0 {
		return nil
	}
	var top []traffic.ConnInfo
	if len(in) <= k {
		top = append(make([]traffic.ConnInfo, 0, len(in)), in...)
	} else {
		top = worstHeapTop(in, k)
	}
	sort.Slice(top, func(i, j int) bool { return connBefore(&top[i], &top[j]) })
	return top
}

// worstHeapTop держит кучу из k лучших; в корне — худший из них.
func worstHeapTop(in []traffic.ConnInfo, k int) []traffic.ConnInfo {
	h := append(make([]traffic.ConnInfo, 0, k), in[:k]...)
	for i := k/2 - 1; i >= 0; i-- {
		siftWorst(h, i)
	}
	for i := k; i < len(in); i++ {
		if connBefore(&in[i], &h[0]) {
			h[0] = in[i]
			siftWorst(h, 0)
		}
	}
	return h
}

func siftWorst(h []traffic.ConnInfo, i int) {
	for {
		worst, l, r := i, 2*i+1, 2*i+2
		if l < len(h) && connBefore(&h[worst], &h[l]) {
			worst = l
		}
		if r < len(h) && connBefore(&h[worst], &h[r]) {
			worst = r
		}
		if worst == i {
			return
		}
		h[i], h[worst] = h[worst], h[i]
		i = worst
	}
}
