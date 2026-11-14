// SPDX-License-Identifier: GPL-3.0-only

package api

// stream_convert.go — StreamSettings: контракт lavray.v1 ↔ каноническая модель.
// Вынесено из convert.go (размерная трещотка).

import (
	"github.com/anatolijlaptev1991-ctrl/lavray-android/core/internal/gen/lavrayv1"
	"github.com/anatolijlaptev1991-ctrl/lavray-android/core/internal/model"
)

func streamToModel(st *lavrayv1.StreamSettings) *model.StreamSettings {
	if st == nil {
		return nil
	}
	m := &model.StreamSettings{
		HeaderType:        optStr(st.HeaderType),
		Path:              st.Path,
		Host:              st.Host,
		WSEarlyDataHeader: optStr(st.WsEarlyDataHeader),
		WSEarlyDataMax:    int(optInt32(st.WsEarlyDataMax)),
		SNI:               st.Sni,
		ALPN:              st.Alpn,
		AllowInsecure:     st.AllowInsecure,
		Certificate:       optStr(st.Certificate),
		UTLSFingerprint:   st.UtlsFingerprint,
		RealityPublicKey:  optStr(st.RealityPublicKey),
		RealityShortID:    optStr(st.RealityShortId),
		RealitySPX:        optStr(st.RealitySpx),
		XHTTPMode:         st.XhttpMode,
		XHTTPExtra:        st.XhttpExtra,
	}
	switch st.Network {
	case lavrayv1.StreamSettings_NETWORK_TCP, lavrayv1.StreamSettings_NETWORK_UNSPECIFIED:
		m.Network = "" // UNSPECIFIED = tcp (дефолт) — в модели пусто
	case lavrayv1.StreamSettings_NETWORK_HTTP:
		m.Network = "http"
	case lavrayv1.StreamSettings_NETWORK_WS:
		m.Network = "ws"
	case lavrayv1.StreamSettings_NETWORK_GRPC:
		m.Network = "grpc"
	case lavrayv1.StreamSettings_NETWORK_HTTP_UPGRADE:
		m.Network = "httpupgrade"
	case lavrayv1.StreamSettings_NETWORK_QUIC:
		m.Network = "quic"
	case lavrayv1.StreamSettings_NETWORK_XHTTP:
		m.Network = "xhttp"
	}
	switch st.Security {
	case lavrayv1.StreamSettings_SECURITY_TLS:
		m.Security = "tls"
	case lavrayv1.StreamSettings_SECURITY_REALITY:
		m.Security = "reality"
	}
	switch st.PacketEncoding {
	case lavrayv1.StreamSettings_PACKET_ENCODING_PACKET_ADDR:
		m.PacketEncoding = "packetaddr"
	case lavrayv1.StreamSettings_PACKET_ENCODING_XUDP:
		m.PacketEncoding = "xudp"
	}
	switch st.Multiplex {
	case lavrayv1.StreamSettings_MULTIPLEX_ON:
		m.Multiplex = 1
	case lavrayv1.StreamSettings_MULTIPLEX_OFF:
		m.Multiplex = 2
	}
	return m
}

func streamToProto(m *model.StreamSettings) *lavrayv1.StreamSettings {
	if m == nil {
		return nil
	}
	p := &lavrayv1.StreamSettings{
		HeaderType:        strpIf(m.HeaderType),
		Path:              m.Path,
		Host:              m.Host,
		WsEarlyDataHeader: strpIf(m.WSEarlyDataHeader),
		Sni:               m.SNI,
		Alpn:              m.ALPN,
		AllowInsecure:     m.AllowInsecure,
		Certificate:       strpIf(m.Certificate),
		UtlsFingerprint:   m.UTLSFingerprint,
		RealityPublicKey:  strpIf(m.RealityPublicKey),
		RealityShortId:    strpIf(m.RealityShortID),
		RealitySpx:        strpIf(m.RealitySPX),
		XhttpMode:         m.XHTTPMode,
		XhttpExtra:        m.XHTTPExtra,
	}
	if m.WSEarlyDataMax > 0 {
		p.WsEarlyDataMax = int32p(int32(m.WSEarlyDataMax))
	}
	switch m.Network {
	case "", "tcp":
		p.Network = lavrayv1.StreamSettings_NETWORK_TCP
	case "http":
		p.Network = lavrayv1.StreamSettings_NETWORK_HTTP
	case "ws":
		p.Network = lavrayv1.StreamSettings_NETWORK_WS
	case "grpc":
		p.Network = lavrayv1.StreamSettings_NETWORK_GRPC
	case "httpupgrade":
		p.Network = lavrayv1.StreamSettings_NETWORK_HTTP_UPGRADE
	case "quic":
		p.Network = lavrayv1.StreamSettings_NETWORK_QUIC
	case "xhttp":
		p.Network = lavrayv1.StreamSettings_NETWORK_XHTTP
	}
	switch m.Security {
	case "tls":
		p.Security = lavrayv1.StreamSettings_SECURITY_TLS
	case "reality":
		p.Security = lavrayv1.StreamSettings_SECURITY_REALITY
	}
	switch m.PacketEncoding {
	case "packetaddr":
		p.PacketEncoding = lavrayv1.StreamSettings_PACKET_ENCODING_PACKET_ADDR
	case "xudp":
		p.PacketEncoding = lavrayv1.StreamSettings_PACKET_ENCODING_XUDP
	}
	switch m.Multiplex {
	case 1:
		p.Multiplex = lavrayv1.StreamSettings_MULTIPLEX_ON
	case 2:
		p.Multiplex = lavrayv1.StreamSettings_MULTIPLEX_OFF
	}
	return p
}
