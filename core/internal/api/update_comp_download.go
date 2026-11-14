// SPDX-License-Identifier: GPL-3.0-only

// update_comp_download.go — загрузка пресетов и гео-набора в
// <данные>/updates/staging/components (T-75/7.5). Артефакт берётся с зеркала по
// пути из подписанного манифеста и сверяется по длине и sha256 из него же;
// применение (update_comp_apply.go) перечитывает файл и сверяет заново.
package api

import (
	"context"
	"errors"
	"time"

	"connectrpc.com/connect"

	lavrayv1 "github.com/anatolijlaptev1991-ctrl/lavray-android/core/internal/gen/lavrayv1"
	"github.com/anatolijlaptev1991-ctrl/lavray-android/core/internal/updates"
)

// dlTargets — какие компоненты просит скачать клиент.
type dlTargets struct{ app, presets, geo bool }

// downloadTargets разбирает components запроса: пусто — только приложение (как
// раньше); ядро и веб-панель скачивать пока нельзя (ok=false).
func downloadTargets(comps []lavrayv1.UpdateComponent) (dlTargets, bool) {
	if len(comps) == 0 {
		return dlTargets{app: true}, true
	}
	var t dlTargets
	for _, c := range comps {
		switch c {
		case lavrayv1.UpdateComponent_UPDATE_COMPONENT_APP:
			t.app = true
		case compPresets:
			t.presets = true
		case compGeo:
			t.geo = true
		default:
			return dlTargets{}, false
		}
	}
	return t, true
}

// runDownloads качает запрошенные компоненты по очереди; нечего качать — пропускает.
func (s *Server) runDownloads(ctx context.Context, c *updates.Client, res *updates.Result, t dlTargets, stream *connect.ServerStream[lavrayv1.UpdateEvent]) error {
	did := false
	if t.app && res.Availability.Available {
		did = true
		if err := s.runDownload(ctx, c, res, stream); err != nil {
			return err
		}
	}
	send := eventSender(stream)
	for _, step := range []struct {
		want bool
		comp lavrayv1.UpdateComponent
		run  func(context.Context, *updates.Result, func(*lavrayv1.UpdateEvent) bool) (bool, error)
	}{{t.presets, compPresets, s.downloadPresets}, {t.geo, compGeo, s.downloadGeo}} {
		if !step.want {
			continue
		}
		fetched, err := step.run(ctx, res, send)
		did = did || fetched
		if err != nil {
			return err
		}
		if !fetched {
			s.upd.settleComp(step.comp) // нечего качать: фаза возвращается к устойчивой
		}
	}
	if !did {
		if t.app {
			s.upd.setPhase(lavrayv1.ComponentState_PHASE_IDLE, "")
		}
		return reasonError(ReasonNoUpdate, "нет проверенного обновления (%s)", res.Availability.Reason)
	}
	return nil
}

// checkFailedPhases: проверка цепочки не удалась — приложение помечается сбоем, а
// данные возвращаются к устойчивой фазе (они не скачивались).
func (s *Server) checkFailedPhases(t dlTargets, err error) {
	if t.app {
		s.upd.setPhase(lavrayv1.ComponentState_PHASE_FAILED, err.Error())
	}
	if t.presets {
		s.upd.settleComp(compPresets)
	}
	if t.geo {
		s.upd.settleComp(compGeo)
	}
}

// eventSender — отправка события с отметкой времени; false — клиент отключился.
func eventSender(stream *connect.ServerStream[lavrayv1.UpdateEvent]) func(*lavrayv1.UpdateEvent) bool {
	return func(ev *lavrayv1.UpdateEvent) bool {
		ev.TsMs = time.Now().UnixMilli()
		return stream.Send(ev) == nil
	}
}

func compProgressEvent(c lavrayv1.UpdateComponent, done, total int64, bps float64) *lavrayv1.UpdateEvent {
	return &lavrayv1.UpdateEvent{Kind: &lavrayv1.UpdateEvent_Progress{Progress: &lavrayv1.DownloadProgress{
		Component: c, DownloadedBytes: done, TotalBytes: total, Bps: bps}}}
}

func verifiedEvent(c lavrayv1.UpdateComponent, version string, f stagedFile) *lavrayv1.UpdateEvent {
	return &lavrayv1.UpdateEvent{Kind: &lavrayv1.UpdateEvent_Verified{Verified: &lavrayv1.VerificationPassed{
		Component: c, Version: version, StagedPath: f.path, Sha256: f.sha256}}}
}

// compDownloadFailed: отмена — ошибка вызова; нарушение целостности — verify_failed;
// прочее — failed (сеть, можно повторить).
func (s *Server) compDownloadFailed(ctx context.Context, c lavrayv1.UpdateComponent, err error, send func(*lavrayv1.UpdateEvent) bool) error {
	if ctx.Err() != nil {
		s.upd.settleComp(c)
		return mapErr(ctx.Err())
	}
	s.upd.setCompPhase(c, lavrayv1.ComponentState_PHASE_FAILED, err.Error())
	if reason := componentFailReason(err); reason != "" {
		send(&lavrayv1.UpdateEvent{Kind: &lavrayv1.UpdateEvent_VerifyFailed{VerifyFailed: &lavrayv1.VerificationFailed{Component: c, Reason: reason}}})
		return nil
	}
	send(&lavrayv1.UpdateEvent{Kind: &lavrayv1.UpdateEvent_Failed{Failed: &lavrayv1.UpdateFailed{
		Component: c, Code: "network", Message: err.Error(), Recoverable: errors.Is(err, updates.ErrNetwork)}}})
	return nil
}
