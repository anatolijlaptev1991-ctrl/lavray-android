// SPDX-License-Identifier: GPL-3.0-only

package api

// log_pump.go — насос живых строк LogService.Subscribe: антифлуд
// (max_lines_per_second) и сводка пропущенных строк. В proto LogLine нет поля
// для счётчика, поэтому сводка идёт обычной строкой журнала (warn, source
// core): так клиент видит потерю там же, где видит сами строки.

import (
	"context"
	"fmt"
	"time"

	"github.com/anatolijlaptev1991-ctrl/lavray-android/core/internal/logs"
)

// pumpLogs пересылает строки sub.C в send, пока ctx не отменён или канал не
// закрыт. gate отбрасывает сверх лимита; о потерянном (лимит и переполнение
// канала подписчика) сообщает сводка: перед первой прошедшей строкой после
// всплеска либо по tick, когда всплеск затих (секунда без отбросов).
func pumpLogs(ctx context.Context, sub *logs.Subscription, gate *logs.FloodGate, now func() time.Time,
	tick <-chan time.Time, keep func(logs.Line) bool, send func(logs.Line) error) error {
	var seenOverflow uint64
	report := func(passed bool) error {
		t := now()
		n := gate.TakeDropped(t, passed)
		ov := sub.Dropped()
		n, seenOverflow = n+int(ov-seenOverflow), ov
		if n == 0 {
			return nil
		}
		sum := logs.Line{
			TsMs: t.UnixMilli(), Level: logs.LevelWarn, Source: logs.SourceCore,
			Message: fmt.Sprintf("журнал: пропущено строк: %d (антифлуд, лимит строк в секунду)", n),
		}
		if keep != nil && !keep(sum) { // сводка проходит те же фильтры, что и строки
			return nil
		}
		return send(sum)
	}
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-tick:
			if err := report(false); err != nil {
				return err
			}
		case l, ok := <-sub.C:
			if !ok {
				return nil
			}
			if !gate.Allow(now()) {
				continue
			}
			if err := report(true); err != nil {
				return err
			}
			if err := send(l); err != nil {
				return err
			}
		}
	}
}
