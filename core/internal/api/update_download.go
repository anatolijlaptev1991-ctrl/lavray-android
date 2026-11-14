// SPDX-License-Identifier: GPL-3.0-only

// update_download.go — DownloadUpdates: проверка цепочки, загрузка установщика
// в <данные>/updates/staging (докачка, sha256, minisign) и события прогресса.
// Порядок событий: progress (total) … progress … verified{staged_path, sha256}
// либо verify_failed{reason} / failed. Параллельная загрузка — ABORTED.
package api

import (
	"context"
	"errors"
	"time"

	"connectrpc.com/connect"

	lavrayv1 "github.com/anatolijlaptev1991-ctrl/lavray-android/core/internal/gen/lavrayv1"
	"github.com/anatolijlaptev1991-ctrl/lavray-android/core/internal/updates"
)

// progressEvery — не чаще одного события прогресса за интервал (кроме итогового).
const progressEvery = 150 * time.Millisecond

func (s *Server) DownloadUpdates(ctx context.Context, req *connect.Request[lavrayv1.DownloadUpdatesRequest], stream *connect.ServerStream[lavrayv1.UpdateEvent]) error {
	targets, ok := downloadTargets(req.Msg.Components)
	if !ok {
		return connect.NewError(connect.CodeUnimplemented, errors.New("скачивание поддерживается для приложения, наборов и гео-данных"))
	}
	channel := lavrayv1.UpdateChannel_UPDATE_CHANNEL_STABLE
	if s.settingStr("update.channel") == updates.ChannelBeta {
		channel = lavrayv1.UpdateChannel_UPDATE_CHANNEL_BETA
	}
	c, st, err := s.updateClient(channel)
	if err != nil {
		return updateErr(err)
	}
	if !s.upd.beginDownload(targets) {
		return connect.NewError(connect.CodeAborted, errors.New("загрузка уже идёт"))
	}
	defer s.upd.endDownload()
	res, err := c.Check(ctx)
	if err != nil {
		s.checkFailedPhases(targets, err)
		return updateErr(err)
	}
	s.saveUpdateState(st)
	if err := s.ensureStaging(); err != nil {
		return mapErr(err)
	}
	return s.runDownloads(ctx, c, res, targets, stream)
}

func (s *Server) runDownload(ctx context.Context, c *updates.Client, res *updates.Result, stream *connect.ServerStream[lavrayv1.UpdateEvent]) error {
	dctx, cancel := context.WithCancel(ctx)
	defer cancel()
	asset := res.Manifest.App.Assets[res.AssetKey]
	send := func(ev *lavrayv1.UpdateEvent) bool {
		ev.TsMs = time.Now().UnixMilli()
		if stream.Send(ev) != nil {
			cancel() // клиент отключился: загрузка не нужна, .part сохраняется для докачки
			return false
		}
		return true
	}
	send(progressEvent(0, asset.Length, 0))
	started, last := time.Now(), time.Time{}
	staged, err := c.Download(dctx, res, s.stagingDir(), func(done, total int64) {
		if done < total && time.Since(last) < progressEvery {
			return
		}
		last = time.Now()
		send(progressEvent(done, total, float64(done)/max(time.Since(started).Seconds(), 0.001)))
	})
	if err != nil {
		return s.downloadFailed(ctx, err, send)
	}
	verify, _ := res.ArtifactVerifierFor(asset)
	s.upd.setStaged(&stagedUpdate{Staged: *staged, verify: verify})
	send(&lavrayv1.UpdateEvent{Kind: &lavrayv1.UpdateEvent_Verified{Verified: &lavrayv1.VerificationPassed{
		Component: lavrayv1.UpdateComponent_UPDATE_COMPONENT_APP, Version: staged.Version,
		StagedPath: staged.Path, Sha256: staged.Sha256,
	}}})
	return nil
}

// downloadFailed: отмена — ошибка вызова; нарушение целостности — событие
// verify_failed (файл уже удалён загрузчиком); прочее — failed (сеть, можно повторить).
func (s *Server) downloadFailed(ctx context.Context, err error, send func(*lavrayv1.UpdateEvent) bool) error {
	if ctx.Err() != nil {
		s.upd.setPhase(lavrayv1.ComponentState_PHASE_IDLE, "")
		return mapErr(ctx.Err())
	}
	s.upd.setPhase(lavrayv1.ComponentState_PHASE_FAILED, err.Error())
	if reason := verifyFailReason(err); reason != "" {
		send(&lavrayv1.UpdateEvent{Kind: &lavrayv1.UpdateEvent_VerifyFailed{VerifyFailed: &lavrayv1.VerificationFailed{
			Component: lavrayv1.UpdateComponent_UPDATE_COMPONENT_APP, Reason: reason}}})
		return nil
	}
	send(&lavrayv1.UpdateEvent{Kind: &lavrayv1.UpdateEvent_Failed{Failed: &lavrayv1.UpdateFailed{
		Component: lavrayv1.UpdateComponent_UPDATE_COMPONENT_APP, Code: "network",
		Message: err.Error(), Recoverable: errors.Is(err, updates.ErrNetwork),
	}}})
	return nil
}

func progressEvent(done, total int64, bps float64) *lavrayv1.UpdateEvent {
	return &lavrayv1.UpdateEvent{Kind: &lavrayv1.UpdateEvent_Progress{Progress: &lavrayv1.DownloadProgress{
		Component:       lavrayv1.UpdateComponent_UPDATE_COMPONENT_APP,
		DownloadedBytes: done, TotalBytes: total, Bps: bps,
	}}}
}
