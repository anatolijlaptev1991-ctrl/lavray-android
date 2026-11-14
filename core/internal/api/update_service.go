// SPDX-License-Identifier: GPL-3.0-only

// update_service.go — UpdateService: CheckUpdates и GetUpdateState поверх
// internal/updates (TUF-lite, UPDATE-SYSTEM v2.0). Загрузка — update_download.go,
// применение и откат — update_apply.go, офлайн-проверка файла — update_local.go.
package api

import (
	"context"
	"time"

	"connectrpc.com/connect"

	lavrayv1 "github.com/anatolijlaptev1991-ctrl/lavray-android/core/internal/gen/lavrayv1"
	"github.com/anatolijlaptev1991-ctrl/lavray-android/core/internal/updates"
)

// CheckUpdates — полная проверка цепочки root→timestamp→manifest. Ошибки:
// UNAVAILABLE — все источники недоступны (last_checked_at не меняется);
// FAILED_PRECONDITION с причиной — см. update_reasons.go.
func (s *Server) CheckUpdates(ctx context.Context, req *connect.Request[lavrayv1.CheckUpdatesRequest]) (*connect.Response[lavrayv1.CheckUpdatesResponse], error) {
	c, st, err := s.updateClient(req.Msg.Channel)
	if err != nil {
		return nil, updateErr(err)
	}
	res, err := c.Check(ctx)
	if err != nil {
		return nil, updateErr(err)
	}
	s.saveUpdateState(st)
	return connect.NewResponse(s.checkResponse(res, st, req.Msg.Components)), nil
}

func (s *Server) checkResponse(res *updates.Result, st *updates.State, want []lavrayv1.UpdateComponent) *lavrayv1.CheckUpdatesResponse {
	out := &lavrayv1.CheckUpdatesResponse{
		CurrentAppVersion: s.upd.appVersion(),
		LastCheckedAtMs:   st.LastCheckedAt,
	}
	if st.LastWorkingURL != "" {
		out.WorkingSources = []string{st.LastWorkingURL}
	}
	if res.Manifest != nil && res.Manifest.Core != nil {
		out.CoreVersion = res.Manifest.Core.Version
	}
	if res.Availability.Available && wantsApp(want) {
		mf := res.Manifest
		avail := mf.App.Version
		out.Available = append(out.Available, &lavrayv1.ComponentUpdate{
			Component:        lavrayv1.UpdateComponent_UPDATE_COMPONENT_APP,
			CurrentVersion:   s.upd.appVersion(),
			AvailableVersion: &avail,
			Notes:            mf.Notes,
			Manifest:         manifestToProto(mf, res.AssetKey),
			Mandatory:        optBool(mf.Mandatory),
		})
	}
	out.Available = append(out.Available, s.componentOffers(res, want)...)
	return out
}

func wantsApp(want []lavrayv1.UpdateComponent) bool {
	if len(want) == 0 {
		return true
	}
	for _, c := range want {
		if c == lavrayv1.UpdateComponent_UPDATE_COMPONENT_APP {
			return true
		}
	}
	return false
}

// manifestToProto — сводка манифеста для UI (без minisig: подпись проверяет ядро).
func manifestToProto(mf *updates.Manifest, assetKey string) *lavrayv1.UpdateManifest {
	out := &lavrayv1.UpdateManifest{
		Product: mf.Product, Serial: mf.Serial, ExpiresMs: mf.Expires.UnixMilli(),
		Version: mf.App.Version, Notes: mf.Notes, Mandatory: optBool(mf.Mandatory),
		Channel: lavrayv1.UpdateChannel_UPDATE_CHANNEL_STABLE,
	}
	if mf.Channel == updates.ChannelBeta {
		out.Channel = lavrayv1.UpdateChannel_UPDATE_CHANNEL_BETA
	}
	if mf.App.MinUpgradeFrom != "" {
		v := mf.App.MinUpgradeFrom
		out.MinUpgradeFrom = &v
	}
	if a, ok := mf.App.Assets[assetKey]; ok {
		out.Assets = map[string]*lavrayv1.UpdateAsset{assetKey: {Path: a.Path, Sha256: a.Sha256, Length: a.Length}}
	}
	return out
}

// GetUpdateState — состояние для экрана «Обновления»: когда была последняя
// успешная проверка, фаза загрузки, доступность отката, цепочка доверия.
func (s *Server) GetUpdateState(ctx context.Context, req *connect.Request[lavrayv1.GetUpdateStateRequest]) (*connect.Response[lavrayv1.UpdateStateResponse], error) {
	st := s.loadUpdateState()
	fresh := st.LastCheckedAt > 0 && time.Since(time.UnixMilli(st.LastCheckedAt)) < 7*24*time.Hour
	return connect.NewResponse(&lavrayv1.UpdateStateResponse{
		Components:      append([]*lavrayv1.ComponentState{s.appComponentState()}, s.componentStates()...),
		LastCheckedAtMs: st.LastCheckedAt,
		ChainState: &lavrayv1.ChainStateInfo{
			RootVersion:    int32(st.RootVersion),
			MaxSerial:      map[string]int64{"stable": st.SerialStable, "beta": st.SerialBeta},
			TimestampFresh: fresh,
		},
	}), nil
}
