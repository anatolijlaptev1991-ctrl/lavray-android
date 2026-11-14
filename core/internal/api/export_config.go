// SPDX-License-Identifier: GPL-3.0-only

package api

// export_config.go — ExportConfig (FR-UI-196, FR-UI-039): конфиг sing-box
// профиля для сохранения в файл. Конфиг отдаётся БЕЗ маскирования (в нём пароли
// и ключи — UI предупреждает), поэтому процедура закрыта агентам (agent_table.go)
// и ничего не пишет в журнал.

import (
	"context"

	"connectrpc.com/connect"

	"github.com/anatolijlaptev1991-ctrl/lavray-android/core/internal/cfgbuild"
	lavrayv1 "github.com/anatolijlaptev1991-ctrl/lavray-android/core/internal/gen/lavrayv1"
	"github.com/anatolijlaptev1991-ctrl/lavray-android/core/internal/model"
)

// ExportConfig собирает конфиг профиля: PURPOSE_RUN — рабочий (входящие и
// пользовательские правила), PURPOSE_TEST — тестовый (без входящих и
// пользовательских правил). Профиля нет — NOT_FOUND; внешнее ядро — FAILED_
// PRECONDITION; конфиг не собирается — код сборки (INVALID_ARGUMENT).
func (s *Server) ExportConfig(ctx context.Context, req *connect.Request[lavrayv1.ExportConfigRequest]) (*connect.Response[lavrayv1.ExportConfigResponse], error) {
	p, err := s.st.GetProfile(req.Msg.ProfileId)
	if err != nil {
		return nil, mapErr(err)
	}
	if p.Type == model.TypeNaive || p.Type == model.TypeCustomExternal {
		return nil, failedPrecondition("профиль работает через внешнее ядро: конфиг sing-box для него не собирается")
	}
	res, err := s.exportBuild(p, req.Msg.Purpose == lavrayv1.ExportConfigRequest_PURPOSE_TEST)
	if err != nil {
		return nil, mapErr(err)
	}
	raw, err := cfgbuild.MarshalIndent(res.Config)
	if err != nil {
		return nil, mapErr(err)
	}
	return connect.NewResponse(&lavrayv1.ExportConfigResponse{
		SingBoxJson: string(raw),
		IsRawConfig: model.IsInternalFull(p), // «свой полный конфиг» — как есть
	}), nil
}

// exportBuild — сборка для экспорта: настройки и набор правил те же, что у
// запуска (режим без TUN), но без auto_detect_interface (ForExport); для
// теста ещё и без входящих (ForTest).
func (s *Server) exportBuild(p *model.Profile, forTest bool) (*cfgbuild.BuildResult, error) {
	var group *model.Group
	if p.GroupID != 0 {
		g, err := s.st.GetGroup(p.GroupID)
		if err != nil {
			return nil, err
		}
		group = g
	}
	preset, err := s.activePreset()
	if err != nil {
		return nil, err
	}
	return cfgbuild.Build(cfgbuild.BuildRequest{
		Profile:   p,
		Group:     group,
		Settings:  s.buildSettings(false),
		Lookup:    s.st.GetProfile,
		Preset:    preset,
		ForTest:   forTest,
		ForExport: true,
	})
}
