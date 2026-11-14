// SPDX-License-Identifier: GPL-3.0-only

package nekoimport

import (
	"fmt"

	"github.com/anatolijlaptev1991-ctrl/lavray-android/core/internal/model"
)

// fillQUIC — naive, hysteria2, tuic (без V2Ray-транспорта).
func fillQUIC(p *model.Profile, typ string, b *nekoBean) {
	switch typ {
	case "naive":
		p.Type = model.TypeNaive
		p.Detail = &model.NaiveDetail{
			Username:            b.Username,
			Password:            b.Password,
			Proto:               b.Protocol,
			ExtraHeaders:        b.ExtraHeaders,
			SNI:                 b.QUICSNI,
			Certificate:         b.Certificate,
			InsecureConcurrency: b.InsecureConc,
			DisableLogs:         b.DisableLog,
		}
	case "hysteria2":
		p.Type = model.TypeHysteria2
		hop := ""
		if b.HopIntervalSec > 0 {
			hop = fmt.Sprintf("%ds", b.HopIntervalSec) // int-секунды → Duration-строка (FR-UI-268)
		}
		p.Detail = &model.Hysteria2Detail{
			Password:      b.Password, // QUICBean пишет ключ «password»
			ObfsPassword:  b.ObfsPassword,
			UploadMbps:    b.UploadMbps,
			DownloadMbps:  b.DownloadMbps,
			ServerPorts:   b.HopPort,
			HopInterval:   hop,
			SNI:           b.QUICSNI,
			ALPN:          b.ALPN,
			CA:            b.CaText,
			AllowInsecure: b.AllowInsecure,
			DisableSNI:    b.DisableSni,
		}
	case "tuic":
		p.Type = model.TypeTUIC
		p.Detail = &model.TUICDetail{
			UUID:              b.TUICUUID,
			Password:          b.Password, // QUICBean пишет ключ «password»
			CongestionControl: b.Congestion,
			UDPRelayMode:      b.UDPRelayMode,
			ZeroRttHandshake:  b.ZeroRtt,
			Heartbeat:         b.Heartbeat,
			SNI:               b.QUICSNI,
			ALPN:              b.ALPN,
			CA:                b.CaText,
			AllowInsecure:     b.AllowInsecure,
			DisableSNI:        b.DisableSni,
			UDPOverStream:     b.Uos,
		}
	}
}
