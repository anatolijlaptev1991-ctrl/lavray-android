// SPDX-License-Identifier: GPL-3.0-only

package api

// core_service.go — CoreService (W6): Connect/Disconnect/RestartProfile/
// GetStatus/SubscribeEvents/GetRuntimeConfig/Shutdown. Конфиг sing-box ядро
// собирает само (cfgbuild); UI передаёт только идентификатор профиля и режим.
//
// Валидаторы Connect: FR-UI-279 (inbound из LAN требует авторизации),
// GATEWAY_*-режимы на десктопе → INVALID_ARGUMENT (шлюз — OpenWrt, W8),
// naive/custom-external — за флагом enable_external_cores (решение №5).

import (
	"context"
	"errors"

	"connectrpc.com/connect"

	"github.com/anatolijlaptev1991-ctrl/lavray-android/core/internal/cfgbuild"
	"github.com/anatolijlaptev1991-ctrl/lavray-android/core/internal/gen/lavrayv1"
	"github.com/anatolijlaptev1991-ctrl/lavray-android/core/internal/inst"
	"github.com/anatolijlaptev1991-ctrl/lavray-android/core/internal/logs"
	"github.com/anatolijlaptev1991-ctrl/lavray-android/core/internal/model"
)

// ---- Connect / Disconnect / Restart ----

// startRunLocked — общая логика запуска Connect и RestartProfile: сборка конфига,
// валидаторы, старт менеджера. Возвращает статус и предупреждения сборки.
func (s *Server) startRunLocked(ctx context.Context, profile *model.Profile, mode lavrayv1.ConnectMode) (*lavrayv1.CoreStatus, []string, error) {
	// Гейт внешних ядер (решение №5): naive/custom-external off по умолчанию.
	if profile.Type == model.TypeNaive || profile.Type == model.TypeCustomExternal {
		if !s.settingBool("enable_external_cores") {
			return nil, nil, failedPrecondition("тип %s требует внешнего ядра: включите настройку enable_external_cores (риск — THREAT-MODEL)", profile.Type)
		}
	}

	// Режим: UNSPECIFIED → последний запомненный (remember.modes; W6 —
	// системный прокси как безопасный дефолт), GATEWAY_* — только OpenWrt.
	if mode == lavrayv1.ConnectMode_CONNECT_MODE_UNSPECIFIED {
		if remembered := s.rememberedMode(); remembered != lavrayv1.ConnectMode_CONNECT_MODE_UNSPECIFIED {
			mode = remembered
		} else {
			mode = lavrayv1.ConnectMode_CONNECT_MODE_SYSTEM_PROXY
		}
	}
	if err := s.checkGatewayMode(mode); err != nil { // gateway_mode.go
		return nil, nil, err
	}

	// Режим TUN без прав: понятная причина вместо сырой ошибки sing-box
	// (подключение из MCP не может показать диалог перезапуска от администратора).
	if mode == lavrayv1.ConnectMode_CONNECT_MODE_TUN && s.tunCheck != nil {
		if err := s.tunCheck(); err != nil {
			return nil, nil, failedPrecondition("%v", err)
		}
	}

	st := s.buildSettings(mode == lavrayv1.ConnectMode_CONNECT_MODE_TUN)
	s.applyGateway(st, mode)
	var warnings []string
	if mode != lavrayv1.ConnectMode_CONNECT_MODE_TUN {
		// T-55: в TUN mixed-слушатель остаётся (cfgbuild/inbounds.go), и
		// не-loopback адрес без авторизации отвергает уже сборка конфига.
		if err := s.validateInboundLAN(st.Inbound); err != nil {
			return nil, nil, err
		}
	}

	var group *model.Group
	if profile.GroupID != 0 {
		g, err := s.st.GetGroup(profile.GroupID)
		if err != nil {
			return nil, nil, err
		}
		group = g
	}
	preset, err := s.activePreset()
	if err != nil {
		return nil, nil, err
	}

	result, err := cfgbuild.Build(cfgbuild.BuildRequest{
		Profile:  profile,
		Group:    group,
		Settings: st,
		Lookup:   s.st.GetProfile,
		Preset:   preset,
	})
	if err != nil {
		return nil, nil, err
	}
	warnings = append(warnings, s.buildWarnings(result, mode)...)
	configJSON, err := cfgbuild.MarshalIndent(result.Config)
	if err != nil {
		return nil, nil, err
	}

	modeStr := runModeString(mode)
	// Итог проверки связи прежнего запуска к новому не относится.
	s.resetHealth()
	startCtx, cancel := context.WithTimeout(ctx, inst.StartTimeout) // LNB-06
	defer cancel()
	err = s.inst.Start(startCtx, inst.RunInfo{
		ProfileID:   profile.ID,
		GroupID:     profile.GroupID,
		ProfileName: profile.Name,
		Mode:        modeStr,
		ConfigJSON:  configJSON,
	})
	if err != nil {
		// T-55: занятый порт входа — понятное сообщение вместо сырого bind-текста.
		if friendly := explainStartError(err, st.Inbound.Port); friendly != nil {
			s.logs.Logf(logs.LevelError, logs.SourceCore, "старт инстанса: %v", err)
			return nil, nil, friendly
		}
		return nil, nil, err
	}
	// Секреты конфига никогда не логируются (LNB-01/28): журнал получает
	// только факт запуска; полный конфиг — GetRuntimeConfig (redacted).
	s.logs.Logf(logs.LevelInfo, logs.SourceCore, "инстанс запущен: профиль %d (%s), режим %s", profile.ID, profile.Type, modeStr)
	s.startHealthCheck() // 0.2.1: «Подключено» ещё не значит «трафик идёт»
	s.beginAccounting()  // T-79: сессия и учёт трафика (traffic_accountant.go)
	return s.coreStatus(), warnings, nil
}

// Connect — FR-UI-032. Ровно один источник профиля: profile_id или inline.
func (s *Server) Connect(ctx context.Context, req *connect.Request[lavrayv1.ConnectRequest]) (*connect.Response[lavrayv1.ConnectResponse], error) {
	in := req.Msg
	profile, inlineWarns, err := s.connectProfile(in) // connect_profile.go
	if err != nil {
		return nil, err
	}

	if in.GetValidateOnly() {
		// dry-run: собрать и провалидировать, не стартуя (редактор/диагностика)
		built, err := s.dryRunBuild(profile, in.GetMode())
		if err != nil {
			return nil, err
		}
		warnings := append([]string{"validate_only: конфиг собран и валиден, инстанс не запущен"}, inlineWarns...)
		warnings = append(warnings, s.buildWarnings(built, in.GetMode())...)
		return connect.NewResponse(&lavrayv1.ConnectResponse{
			Status:   s.coreStatus(),
			Warnings: warnings,
		}), nil
	}

	s.auto.noteUserOp()
	status, warnings, err := s.startRun(ctx, profile, in.GetMode())
	if err != nil {
		if errors.Is(err, inst.ErrAlreadyRunning) {
			return nil, failedPrecondition("соединение уже активно: используйте Disconnect или RestartProfile")
		}
		return nil, err
	}
	s.rememberConnect(in, status) // remember.go: профиль и режим для Ctrl+D и автоподключения
	return connect.NewResponse(&lavrayv1.ConnectResponse{Status: status, Warnings: append(inlineWarns, warnings...)}), nil
}

// dryRunBuild — сборка без запуска (валидация профиля/настроек).
func (s *Server) dryRunBuild(profile *model.Profile, mode lavrayv1.ConnectMode) (*cfgbuild.BuildResult, error) {
	if profile.Type == model.TypeNaive || profile.Type == model.TypeCustomExternal {
		if !s.settingBool("enable_external_cores") {
			return nil, failedPrecondition("тип %s требует внешнего ядра (enable_external_cores)", profile.Type)
		}
	}
	if err := s.checkGatewayMode(mode); err != nil {
		return nil, err
	}
	st := s.buildSettings(mode == lavrayv1.ConnectMode_CONNECT_MODE_TUN)
	s.applyGateway(st, mode)
	if mode != lavrayv1.ConnectMode_CONNECT_MODE_TUN {
		if err := s.validateInboundLAN(st.Inbound); err != nil {
			return nil, err
		}
	}
	var group *model.Group
	if profile.GroupID != 0 {
		g, err := s.st.GetGroup(profile.GroupID)
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
		Profile: profile, Group: group, Settings: st, Lookup: s.st.GetProfile, Preset: preset,
	})
}

// RestartProfile — FR-UI-020: тот же профиль и режим, конфиг пересобирается.
func (s *Server) RestartProfile(ctx context.Context, req *connect.Request[lavrayv1.RestartProfileRequest]) (*connect.Response[lavrayv1.ConnectResponse], error) {
	st := s.inst.Status()
	if st.Run == nil {
		return nil, failedPrecondition("перезапуск требует активного соединения (инстанс не запущен)")
	}
	profileID := st.Run.ProfileID
	if profileID == 0 {
		return nil, failedPrecondition("перезапуск inline-профиля невозможен: инстанс запущен без сохранённого профиля")
	}
	profile, err := s.st.GetProfile(profileID)
	if err != nil {
		return nil, err
	}
	mode := connectModeOf(st.Run.Mode)

	// T-28: новый конфиг собирается и проверяется ДО остановки. Ошибка сборки
	// (например, адрес LAN без пароля) уходит клиенту, а работающее соединение
	// остаётся нетронутым.
	if _, err := s.dryRunBuild(profile, mode); err != nil {
		return nil, err
	}
	s.auto.noteUserOp()
	stopCtx, cancel := context.WithTimeout(ctx, inst.StopTimeout)
	defer cancel()
	if err := s.inst.Stop(stopCtx); err != nil {
		return nil, err
	}
	status, warnings, err := s.startRun(ctx, profile, mode)
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(&lavrayv1.ConnectResponse{Status: status, Warnings: warnings}), nil
}

// ============================================================================
// Статус и события
// ============================================================================

func (s *Server) GetStatus(ctx context.Context, req *connect.Request[lavrayv1.GetStatusRequest]) (*connect.Response[lavrayv1.CoreStatus], error) {
	return connect.NewResponse(s.coreStatus()), nil
}

// SubscribeEvents — поток CoreEvent. Первый кадр — снимок (контракт);
// далее — каждое изменение состояния FSM. Фильтр event_kinds: пока
// поддерживается только "state_changed" (W6); неизвестные фильтры — все.
func (s *Server) SubscribeEvents(ctx context.Context, req *connect.Request[lavrayv1.SubscribeEventsRequest], stream *connect.ServerStream[lavrayv1.CoreEvent]) error {
	// первый кадр — полный CoreStatus
	if err := stream.Send(&lavrayv1.CoreEvent{
		TsMs: nowMs(),
		Kind: &lavrayv1.CoreEvent_StateChanged{StateChanged: s.coreStatus()},
	}); err != nil {
		return err
	}
	ch, cancel := s.inst.Subscribe()
	defer cancel()
	healthCh, healthCancel := s.subscribeHealth()
	defer healthCancel()
	for {
		select {
		case <-ctx.Done():
			return nil // клиент ушёл — тихое завершение стрима
		case <-healthCh:
			// Итог проверки связи изменился: свежий статус (health_error) и,
			// если трафик не идёт, предупреждение с машинным кодом.
			if err := stream.Send(&lavrayv1.CoreEvent{
				TsMs: nowMs(),
				Kind: &lavrayv1.CoreEvent_StateChanged{StateChanged: s.coreStatus()},
			}); err != nil {
				return err
			}
			if warn := s.healthWarningEvent(); warn != nil {
				if err := stream.Send(warn); err != nil {
					return err
				}
			}
		case ev, ok := <-ch:
			if !ok {
				// Shutdown — сообщаем о скором завершении ядра
				_ = stream.Send(&lavrayv1.CoreEvent{
					TsMs: nowMs(),
					Kind: &lavrayv1.CoreEvent_ShutdownImminent{ShutdownImminent: "shutdown"},
				})
				return nil
			}
			cs := s.statusFromInst(ev.Status)
			if err := stream.Send(&lavrayv1.CoreEvent{
				TsMs: nowMs(),
				Kind: &lavrayv1.CoreEvent_StateChanged{StateChanged: cs},
			}); err != nil {
				return err
			}
		}
	}
}

// statusFromInst — CoreStatus из снимка менеджера (без перечитывания
// настроек — их снимок может меняться независимо от FSM).
func (s *Server) statusFromInst(st inst.Status) *lavrayv1.CoreStatus {
	cs := s.coreStatus()
	cs.State = coreStateOf(st.State)
	cs.ActiveProfileId = nil
	cs.ActiveGroupId = nil
	cs.ActiveProfileName = nil
	cs.StartedAtMs = nil
	cs.Mode = connectModeOf("")
	if st.Run != nil {
		if st.Run.ProfileID != 0 {
			cs.ActiveProfileId = int64p(st.Run.ProfileID)
		}
		if st.Run.GroupID != 0 {
			cs.ActiveGroupId = int64p(st.Run.GroupID)
		}
		if st.Run.ProfileName != "" {
			cs.ActiveProfileName = strp(st.Run.ProfileName)
		}
		cs.Mode = connectModeOf(st.Run.Mode)
		if st.Run.StartedAtMs != 0 {
			cs.StartedAtMs = int64p(st.Run.StartedAtMs)
		}
	}
	if st.LastError != "" {
		cs.LastError = strp(st.LastError)
	} else {
		cs.LastError = nil
	}
	return cs
}

// GetRuntimeConfig — конфиг текущего/последнего запуска, redacted (LNB-28:
// без паролей/uuid/токенов; в логи конфиг не пишется никогда — LNB-01).
func (s *Server) GetRuntimeConfig(ctx context.Context, req *connect.Request[lavrayv1.GetRuntimeConfigRequest]) (*connect.Response[lavrayv1.GetRuntimeConfigResponse], error) {
	st := s.inst.Status()
	if len(st.LastConfig) == 0 {
		return nil, notFound("конфиг ещё не собирался (инстанс ни разу не запускался)")
	}
	resp := &lavrayv1.GetRuntimeConfigResponse{SingBoxJson: logs.Redact(string(st.LastConfig))}
	if st.Run != nil && st.Run.ProfileID != 0 {
		resp.ProfileId = int64p(st.Run.ProfileID)
	}
	return connect.NewResponse(resp), nil
}

// Shutdown — AUDIT §7.1.2: graceful instance.Close() ДО выхода; сам выход из
// процесса — вызывающая сторона (OnShutdown; headless W8).
func (s *Server) Shutdown(ctx context.Context, req *connect.Request[lavrayv1.ShutdownRequest]) (*connect.Response[lavrayv1.Empty], error) {
	s.logs.Logf(logs.LevelInfo, logs.SourceCore, "Shutdown: graceful-остановка (reason=%d)", int(req.Msg.GetReason()))
	s.resetHealth()
	stopCtx, cancel := context.WithTimeout(ctx, inst.StopTimeout)
	defer cancel()
	err := s.inst.Shutdown(stopCtx)
	s.endAccounting() // T-79: счётчики и конец сессии записываются до выхода из процесса
	if err != nil {
		s.logs.Logf(logs.LevelError, logs.SourceCore, "Shutdown: остановка инстанса: %v", err)
		return nil, err
	}
	if s.OnShutdown != nil {
		s.OnShutdown(ctx)
	}
	return connect.NewResponse(&lavrayv1.Empty{}), nil
}
