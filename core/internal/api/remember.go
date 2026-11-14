// SPDX-License-Identifier: GPL-3.0-only

package api

// remember.go — «запомнить последний профиль» (FR-UI-014, FR-UI-186, хвосты A):
// успешный Connect по profile_id записывает remember.id и remember.modes.
// Запись ведётся всегда; remember.enable управляет только автоподключением при
// старте службы (headless_glue.go: ConnectLast).

import (
	"strconv"
	"strings"

	lavrayv1 "github.com/anatolijlaptev1991-ctrl/lavray-android/core/internal/gen/lavrayv1"
	"github.com/anatolijlaptev1991-ctrl/lavray-android/core/internal/logs"
)

const (
	settingRememberID    = "remember.id"
	settingRememberModes = "remember.modes"
)

// rememberConnect запоминает профиль и режим успешного подключения. Inline-
// профиль не запоминается: его нет в базе, подключать потом нечего. Сбой записи
// подключение не отменяет — только предупреждение в журнале.
func (s *Server) rememberConnect(in *lavrayv1.ConnectRequest, status *lavrayv1.CoreStatus) {
	if in.ProfileId == nil || status == nil {
		return
	}
	writes := []settingWrite{{settingRememberID, strconv.FormatInt(in.GetProfileId(), 10)}}
	if mode := rememberModeName(status.GetMode()); mode != "" {
		writes = append(writes, settingWrite{settingRememberModes, `["` + mode + `"]`})
	}
	if err := s.writeSettings(writes); err != nil {
		s.logs.Logf(logs.LevelWarn, logs.SourceCore, "не удалось запомнить профиль: %v", err)
	}
}

// rememberModeName — имя режима в remember.modes (формат NekoBox spmode2).
func rememberModeName(mode lavrayv1.ConnectMode) string {
	switch mode {
	case lavrayv1.ConnectMode_CONNECT_MODE_TUN:
		return "tun"
	case lavrayv1.ConnectMode_CONNECT_MODE_SYSTEM_PROXY:
		return "system_proxy"
	}
	return ""
}

// rememberedMode — последний режим профиля (remember.modes, перенос §5.4;
// формат NekoBox — список "system_proxy"/"tun"; пусто = не запомнен).
func (s *Server) rememberedMode() lavrayv1.ConnectMode {
	for _, v := range s.settingStringList(settingRememberModes) {
		switch strings.TrimSpace(v) {
		case "tun", "vpn":
			return lavrayv1.ConnectMode_CONNECT_MODE_TUN
		case "system_proxy", "proxy":
			return lavrayv1.ConnectMode_CONNECT_MODE_SYSTEM_PROXY
		}
	}
	return lavrayv1.ConnectMode_CONNECT_MODE_UNSPECIFIED
}
