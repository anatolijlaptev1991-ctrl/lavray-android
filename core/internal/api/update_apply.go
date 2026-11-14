// SPDX-License-Identifier: GPL-3.0-only

// update_apply.go — ApplyUpdates и Rollback. Приложение (APP) устанавливает
// оболочка (NSIS /S, UAC), ядро перед этим ещё раз проверяет установщик на диске
// и сообщает ready_for_restart. Откат тоже за оболочкой: ядро не трогает ни
// файлы, ни монотонные serial (T-17).
package api

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"time"

	"connectrpc.com/connect"

	lavrayv1 "github.com/anatolijlaptev1991-ctrl/lavray-android/core/internal/gen/lavrayv1"
	"github.com/anatolijlaptev1991-ctrl/lavray-android/core/internal/inst"
	"github.com/anatolijlaptev1991-ctrl/lavray-android/core/internal/updates"
)

func (s *Server) ApplyUpdates(ctx context.Context, req *connect.Request[lavrayv1.ApplyUpdatesRequest], stream *connect.ServerStream[lavrayv1.UpdateEvent]) error {
	comps := req.Msg.Components
	if len(comps) == 0 {
		comps = []lavrayv1.UpdateComponent{lavrayv1.UpdateComponent_UPDATE_COMPONENT_APP}
	}
	for _, comp := range comps {
		switch comp {
		case lavrayv1.UpdateComponent_UPDATE_COMPONENT_APP:
			if err := s.applyApp(req.Msg.ActiveSessionConfirmed, stream); err != nil {
				return err
			}
		case lavrayv1.UpdateComponent_UPDATE_COMPONENT_CORE:
			return failedPrecondition("ядро обновляется вместе с приложением: отдельная доставка ядра не включена")
		case compPresets, compGeo:
			if err := s.applyData(comp, stream); err != nil {
				return err
			}
		default:
			return connect.NewError(connect.CodeUnimplemented, errors.New("применение этого компонента ещё не реализовано"))
		}
	}
	return nil
}

// sessionActive: туннель поднимается, работает или останавливается.
func (s *Server) sessionActive() bool { return s.inst.Status().State != inst.StateIdle }

func (s *Server) applyApp(confirmed bool, stream *connect.ServerStream[lavrayv1.UpdateEvent]) error {
	staged := s.upd.stagedCopy()
	if staged == nil {
		return reasonError(ReasonNotStaged, "сначала скачайте обновление")
	}
	if s.sessionActive() && !confirmed {
		return reasonError(ReasonActiveSession, "активна VPN-сессия: подтвердите установку, соединение будет прервано")
	}
	send := func(k *lavrayv1.UpdateEvent) error { k.TsMs = time.Now().UnixMilli(); return stream.Send(k) }
	if err := s.reverifyStaged(staged); err != nil {
		s.upd.setStaged(nil)
		s.upd.setPhase(lavrayv1.ComponentState_PHASE_FAILED, err.Error())
		reason := verifyFailReason(err)
		if reason == "" {
			reason = ReasonBadSignature
		}
		return send(&lavrayv1.UpdateEvent{Kind: &lavrayv1.UpdateEvent_VerifyFailed{VerifyFailed: &lavrayv1.VerificationFailed{
			Component: lavrayv1.UpdateComponent_UPDATE_COMPONENT_APP, Reason: reason}}})
	}
	s.upd.setPhase(lavrayv1.ComponentState_PHASE_READY_FOR_RESTART, "")
	if err := send(&lavrayv1.UpdateEvent{Kind: &lavrayv1.UpdateEvent_Verified{Verified: &lavrayv1.VerificationPassed{
		Component: lavrayv1.UpdateComponent_UPDATE_COMPONENT_APP, Version: staged.Version,
		StagedPath: staged.Path, Sha256: staged.Sha256}}}); err != nil {
		return err
	}
	return send(&lavrayv1.UpdateEvent{Kind: &lavrayv1.UpdateEvent_ReadyForRestart{ReadyForRestart: "app"}})
}

// reverifyStaged — повторная проверка перед установкой (анти-TOCTOU): путь внутри
// staging, sha256 файла на диске совпадает с проверенным при загрузке, подпись minisign.
func (s *Server) reverifyStaged(st *stagedUpdate) error {
	p, err := updates.InsideStaging(s.stagingDir(), st.Path)
	if err != nil {
		return err
	}
	f, err := os.Open(p)
	if err != nil {
		return fmt.Errorf("%w: %v", updates.ErrNotStaged, err)
	}
	defer f.Close()
	sum, digest := sha256.New(), updates.NewBlake2b()
	if _, err := io.Copy(io.MultiWriter(sum, digest), f); err != nil {
		return err
	}
	if subtle.ConstantTimeCompare([]byte(hex.EncodeToString(sum.Sum(nil))), []byte(st.Sha256)) != 1 {
		return fmt.Errorf("%w: файл в staging изменён после загрузки", updates.ErrHashMismatch)
	}
	var d [updates.DigestSize]byte
	copy(d[:], digest.Sum(nil))
	return st.verify(d)
}

// Rollback: откат приложения выполняет оболочка прошлым установщиком (команда
// update_rollback), поэтому ядро отвечает FAILED_PRECONDITION и ничего не меняет:
// update.serial_* — монотонные счётчики, сброс снял бы защиту от downgrade (T-17).
func (s *Server) Rollback(ctx context.Context, req *connect.Request[lavrayv1.RollbackRequest]) (*connect.Response[lavrayv1.RollbackResponse], error) {
	switch req.Msg.Component {
	case lavrayv1.UpdateComponent_UPDATE_COMPONENT_UNSPECIFIED:
		return nil, invalidArg("component обязателен")
	case lavrayv1.UpdateComponent_UPDATE_COMPONENT_APP:
		return nil, failedPrecondition("откат выполняет оболочка (update_rollback): ядро не меняет файлы приложения")
	case compPresets, compGeo:
		return s.rollbackData(req.Msg.Component)
	}
	return nil, connect.NewError(connect.CodeUnimplemented, errors.New("откат этого компонента ещё не реализован"))
}
