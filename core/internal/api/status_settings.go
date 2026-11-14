// SPDX-License-Identifier: GPL-3.0-only

package api

// status_settings.go — Inbound/Tun для CoreStatus без полного buildSettings.
// GetStatus опрашивается раз в секунду (иногда дважды): прежние два вызова
// buildSettings давали по ~25 SQL-чтений каждый ради шести нужных значений.

import lavrayv1 "github.com/anatolijlaptev1991-ctrl/lavray-android/core/internal/gen/lavrayv1"

// statusInboundTun читает только значения ответа, с теми же умолчаниями
// (settingStr/settingInt), что и buildSettings. Tun.Enabled — всегда false,
// как у buildSettings(false): включение TUN определяется режимом подключения.
func (s *Server) statusInboundTun() (*lavrayv1.InboundInfo, *lavrayv1.TunInfo) {
	in := &lavrayv1.InboundInfo{
		Listen: s.settingStr("inbound.address"),
		Port:   int32(s.settingInt("inbound.port")),
	}
	in.AuthEnabled = s.settingStr("inbound.auth.user") != "" && s.settingStr("inbound.auth.pass") != ""
	tun := &lavrayv1.TunInfo{}
	if name := s.settingStr("tun.interface_name"); name != "" {
		tun.InterfaceName = strp(name)
	}
	return in, tun
}
