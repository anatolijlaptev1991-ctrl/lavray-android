// SPDX-License-Identifier: GPL-3.0-only

package nekoimport

import (
	"encoding/json"
	"fmt"
)

// format.go — структуры формата NekoBox/nekoray (db/Database.cpp) и разбор
// байтов файла. Чтение файлов с диска — limits.go (reader).

// nekoEntity — profiles/<id>.json (ProxyEntity, db/Database.cpp:208).
type nekoEntity struct {
	Type    string          `json:"type"`
	ID      int64           `json:"id"`
	Gid     int64           `json:"gid"`
	Yc      *int32          `json:"yc"` // 0 = не тестировали; <0 unavailable; >0 мс
	Report  string          `json:"report"`
	Bean    json.RawMessage `json:"bean"`
	Traffic struct {
		DL int64 `json:"dl"`
		UL int64 `json:"ul"`
	} `json:"traffic"`
}

// nekoBean — объединение полей всех bean-ов (fmt/*.hpp); неизвестные поля
// игнорируются (как JsonStore::FromJson — прямая совместимость).
// Пустые строки NekoBox в JSON не пишет (JsonStore::ToJson), отсутствие
// поля = пустое значение.
type nekoBean struct {
	// AbstractBean (fmt/AbstractBean.cpp)
	V    int    `json:"_v"`
	Name string `json:"name"`
	Addr string `json:"addr"`
	Port int    `json:"port"`
	CCfg string `json:"c_cfg"`
	COut string `json:"c_out"`

	// SocksHttpBean
	SocksType int    `json:"v"` // 5 socks5 | 4 socks4 | -80 http
	Username  string `json:"username"`
	Password  string `json:"password"`

	// ShadowSocksBean
	Method string `json:"method"`
	Pass   string `json:"pass"`
	Plugin string `json:"plugin"`
	Uot    int    `json:"uot"`

	// VMessBean
	VMessID string `json:"id"` // uuid
	Aid     int    `json:"aid"`
	Sec     string `json:"sec"`

	// TrojanVLESSBean (пароль в общем поле Pass)
	Flow string `json:"flow"`

	// QUICBean (hysteria2 | tuic)
	ObfsPassword   string `json:"obfsPassword"`
	UploadMbps     int    `json:"uploadMbps"`
	DownloadMbps   int    `json:"downloadMbps"`
	HopIntervalSec int    `json:"hopInterval"`
	HopPort        string `json:"hopPort"`
	TUICUUID       string `json:"uuid"`
	Congestion     string `json:"congestionControl"`
	UDPRelayMode   string `json:"udpRelayMode"`
	ZeroRtt        bool   `json:"zeroRttHandshake"`
	Heartbeat      string `json:"heartbeat"`
	Uos            bool   `json:"uos"`
	AllowInsecure  bool   `json:"allowInsecure"`
	QUICSNI        string `json:"sni"`
	ALPN           string `json:"alpn"`
	CaText         string `json:"caText"`
	DisableSni     bool   `json:"disableSni"`

	// NaiveBean
	Protocol     string `json:"protocol"`
	ExtraHeaders string `json:"extra_headers"`
	Certificate  string `json:"certificate"`
	InsecureConc int    `json:"insecure_concurrency"`
	DisableLog   bool   `json:"disable_log"`

	// CustomBean
	Core         string   `json:"core"`
	Command      []string `json:"cmd"`
	ConfigSimple string   `json:"cs"`
	ConfigSuffix string   `json:"cs_suffix"`
	MappingPort  int      `json:"mapping_port"`
	SocksPort    int      `json:"socks_port"`

	// ChainBean
	List []int64 `json:"list"`

	// Группа старого формата (профиль с type=group)
	URL            string  `json:"url"`
	Info           string  `json:"info"`
	Lastup         int64   `json:"lastup"`
	Order          []int64 `json:"order"`
	Archive        *bool   `json:"archive"`
	SkipAutoUpdate *bool   `json:"skip_auto_update"`
	FrontProxyID   *int64  `json:"front_proxy_id"`

	Stream *nekoStream `json:"stream"`
}

// nekoStream — V2rayStreamSettings (fmt/V2RayStreamSettings.hpp).
type nekoStream struct {
	Network        string `json:"net"`
	Security       string `json:"sec"`
	PacketEncoding string `json:"pac_enc"`
	Path           string `json:"path"`
	Host           string `json:"host"`
	HeaderType     string `json:"h_type"`
	SNI            string `json:"sni"`
	ALPN           string `json:"alpn"`
	Certificate    string `json:"cert"`
	UTLS           string `json:"utls"`
	Insecure       bool   `json:"insecure"`
	EdName         string `json:"ed_name"`
	EdLen          int    `json:"ed_len"`
	Pbk            string `json:"pbk"`
	Sid            string `json:"sid"`
	Spx            string `json:"spx"`
	MuxS           int    `json:"mux_s"`
}

// nekoGroup — groups/<id>.json (db/Group.hpp, Database.cpp:306).
type nekoGroup struct {
	ID                  int64   `json:"id"`
	FrontProxyID        int64   `json:"front_proxy_id"` // -1 = нет
	Archive             bool    `json:"archive"`
	SkipAutoUpdate      bool    `json:"skip_auto_update"`
	Name                string  `json:"name"`
	Order               []int64 `json:"order"`
	URL                 string  `json:"url"`
	Info                string  `json:"info"`
	Lastup              int64   `json:"lastup"`
	ManuallyColumnWidth bool    `json:"manually_column_width"`
	ColumnWidth         []int   `json:"column_width"`
}

// decodeNekoGroup разбирает groups/<id>.json. Отсутствующий front_proxy_id
// означает «нет» (-1): без этого он читался бы как 0 — профиль с id 0.
func decodeNekoGroup(b []byte) (*nekoGroup, error) {
	g := nekoGroup{FrontProxyID: -1}
	if err := json.Unmarshal(b, &g); err != nil {
		return nil, fmt.Errorf("invalid JSON: %w", err)
	}
	return &g, nil
}

func decodeNekoEntity(b []byte) (*nekoEntity, error) {
	var ent nekoEntity
	if err := json.Unmarshal(b, &ent); err != nil {
		return nil, fmt.Errorf("invalid JSON: %w", err)
	}
	return &ent, nil
}
