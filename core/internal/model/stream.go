// SPDX-License-Identifier: GPL-3.0-only

package model

// StreamSettings — V2Ray Stream Settings (порт fmt/V2RayStreamSettings.hpp;
// FR-UI-090a–090l). Используется типами socks/http/ss/vmess/vless/trojan.
type StreamSettings struct {
	Network    string `json:"network,omitempty"`     // tcp(дефолт)|http|ws|grpc|httpupgrade|quic
	HeaderType string `json:"header_type,omitempty"` // tcp: none|http
	Path       string `json:"path,omitempty"`
	Host       string `json:"host,omitempty"`

	WSEarlyDataHeader string `json:"ws_early_data_header,omitempty"` // FR-UI-090c
	WSEarlyDataMax    int    `json:"ws_early_data_max,omitempty"`

	Security        string `json:"security,omitempty"` // "" | tls | reality (reality — при непустом pbk, FR-UI-090i)
	SNI             string `json:"sni,omitempty"`
	ALPN            string `json:"alpn,omitempty"` // через запятую
	AllowInsecure   bool   `json:"allow_insecure,omitempty"`
	Certificate     string `json:"certificate,omitempty"`
	UTLSFingerprint string `json:"utls_fingerprint,omitempty"` // "" = глобальный дефолт

	RealityPublicKey string `json:"reality_public_key,omitempty"` // pbk — включает REALITY
	RealityShortID   string `json:"reality_short_id,omitempty"`   // sid
	RealitySPX       string `json:"reality_spx,omitempty"`        // зарезервировано (FR-UI-267)

	PacketEncoding string `json:"packet_encoding,omitempty"` // "" | packetaddr | xudp
	Multiplex      int    `json:"multiplex,omitempty"`       // 0 default | 1 on | 2 off

	// XHTTP (Xray): ядро транспорта не имеет, настройки хранятся ради круга
	// «ссылка → профиль → ссылка» (proto xhttp_mode / xhttp_extra).
	XHTTPMode  string `json:"xhttp_mode,omitempty"`  // auto | packet-up | stream-up | stream-one
	XHTTPExtra string `json:"xhttp_extra,omitempty"` // сырой JSON параметра extra
}
