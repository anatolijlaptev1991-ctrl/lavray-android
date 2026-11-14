// SPDX-License-Identifier: GPL-3.0-only

// lifecycle.go — остановка инстанса при выходе процесса ядра (T-52).
package api

import (
	"context"

	"github.com/anatolijlaptev1991-ctrl/lavray-android/core/internal/inst"
)

// StopInstance останавливает sing-box (TUN, маршруты, inbounds) и закрывает
// менеджер: Connect после этого невозможен. Тот же путь, что у RPC Shutdown
// (AUDIT §7.1.2: Close до выхода), для вызывающего вне RPC — выход по EOF
// stdin, по сигналу и после самого RPC. Ограничен inst.StopTimeout;
// повторный вызов — no-op. Сессия учёта трафика (T-79) завершается после
// остановки: итог и время конца попадают в базу до выхода процесса.
func (s *Server) StopInstance(ctx context.Context) error {
	stopCtx, cancel := context.WithTimeout(ctx, inst.StopTimeout)
	defer cancel()
	err := s.inst.Shutdown(stopCtx)
	s.shutdownGateway() // правила nft шлюза снимаются до выхода процесса (gateway_mode.go)
	s.endAccounting()
	return err
}
