// SPDX-License-Identifier: GPL-3.0-only

package api

// disconnect.go — CoreService.Disconnect (FR-UI-033): остановка с таймаутом
// LNB-06 и финальные счётчики сессии (T-79).

import (
	"context"

	"connectrpc.com/connect"

	"github.com/anatolijlaptev1991-ctrl/lavray-android/core/internal/gen/lavrayv1"
	"github.com/anatolijlaptev1991-ctrl/lavray-android/core/internal/inst"
	"github.com/anatolijlaptev1991-ctrl/lavray-android/core/internal/logs"
)

// Disconnect останавливает инстанс и возвращает final_stats — итоги сессии
// (collect_stats = false пропускает их: быстрая остановка). Учёт трафика
// закрывается и когда остановка не удалась: инстанс упал, а счётчики, набранные
// до этого, пропасть не должны.
func (s *Server) Disconnect(ctx context.Context, req *connect.Request[lavrayv1.DisconnectRequest]) (*connect.Response[lavrayv1.DisconnectResponse], error) {
	s.auto.noteUserOp()
	s.resetHealth()
	// Запуск, источник и сессия учёта берутся до остановки: потом менеджер их уже
	// не отдаёт, а счётчики источника читаются и после остановки. Завершается
	// именно эта сессия, а не та, что мог открыть Connect, успевший между ними.
	run, src, sess := s.inst.Status().Run, s.inst.Traffic(), s.currentSession()
	stopCtx, cancel := context.WithTimeout(ctx, inst.StopTimeout)
	defer cancel()
	err := s.inst.Stop(stopCtx)
	s.endSession(sess)
	if err != nil {
		return nil, err
	}
	s.logs.Logf(logs.LevelInfo, logs.SourceCore, "инстанс остановлен")
	resp := &lavrayv1.DisconnectResponse{}
	if src != nil && (req.Msg.CollectStats == nil || *req.Msg.CollectStats) {
		resp.FinalStats = finalSnapshot(run, src)
	}
	return connect.NewResponse(resp), nil
}
