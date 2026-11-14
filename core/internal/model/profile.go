// SPDX-License-Identifier: GPL-3.0-only

// Package model — каноническая модель данных LAVRay: bean-иерархия профилей
// (порт fmt/*Bean.hpp из nekoray, см. docs/stage3/core-port-plan.md §5),
// группы и скелет карты настроек.
//
// JSON-представление Profile — канонический JSON, хранящийся в
// profiles.data (core-port-plan §5.2). Имена полей — snake_case, для
// типоспецифичных частей взяты имена контракта proto/lavray.proto
// (в частности hy2: server_ports/hop_interval — FR-UI-268, LNB-35).
package model

import (
	"encoding/json"
	"fmt"
)

// ProfileType — тип профиля (proto enum ProfileType). В БД и каноническом
// JSON хранится строкой ('socks'...'chain', core-port-plan §5.2).
type ProfileType string

const (
	TypeSOCKS          ProfileType = "socks"
	TypeHTTP           ProfileType = "http"
	TypeShadowsocks    ProfileType = "shadowsocks"
	TypeTrojan         ProfileType = "trojan"
	TypeVMess          ProfileType = "vmess"
	TypeVLESS          ProfileType = "vless"
	TypeNaive          ProfileType = "naive" // только за флагом enable_external_cores
	TypeHysteria2      ProfileType = "hysteria2"
	TypeTUIC           ProfileType = "tuic"
	TypeCustomOutbound ProfileType = "custom_outbound" // CustomBean core=internal
	TypeCustomConfig   ProfileType = "custom_config"   // CustomBean core=internal-full
	TypeCustomExternal ProfileType = "custom_external" // CustomBean core=external — только за флагом
	TypeChain          ProfileType = "chain"
)

// TestResult — результат тестов профиля. LatencyMs: nil = не тестировали,
// < 0 = unavailable, > 0 = латентность (семантика «yc» NekoBox сохранена
// для миграции).
type TestResult struct {
	LatencyMs  *int32 `json:"latency_ms,omitempty"`
	FullReport string `json:"full_report,omitempty"` // JSON FullTestReport
	TestedAtMs *int64 `json:"tested_at_ms,omitempty"`
	LastError  string `json:"last_error,omitempty"`
}

// TrafficCounters — накопленный трафик профиля (FR-UI-211).
type TrafficCounters struct {
	UplinkBytes   int64 `json:"uplink_bytes"`
	DownlinkBytes int64 `json:"downlink_bytes"`
}

// Profile — общий каркас профиля (AbstractBean + ProxyEntity из nekoray) +
// типоспецифичная часть Detail (ровно одна, как oneof в контракте).
type Profile struct {
	ID      int64       `json:"id"`
	GroupID int64       `json:"group_id"`
	Type    ProfileType `json:"type"`
	Name    string      `json:"name"`
	Address string      `json:"address"` // пробелы вырезаются (FR-UI-083)
	Port    int         `json:"port"`    // 0 невалиден (LNB-15)

	Favorite bool     `json:"favorite,omitempty"` // метаданные LAVRay (REQUIREMENTS §20.18)
	Tags     []string `json:"tags,omitempty"`
	Notes    string   `json:"notes,omitempty"`

	// Manual — профиль добавлен пользователем вручную в группу-подписку
	// (T-51, B1-35): обновление подписки его не сопоставляет, не удаляет и не
	// учитывает в пороге «удаление > 50 %». В proto не отдаётся: флаг ставит
	// ядро (CreateProfile / ImportContent / MoveProfiles в группу-подписку),
	// UpdateProfile его сохраняет.
	Manual bool `json:"manual,omitempty"`

	// Geo — страна/регион сервера: ISO 3166-1 alpha-2 (нижний регистр, "jp",
	// "nl", …) либо "" если неизвестна. Заполняется импортёром/подпиской из
	// имени (флаги, коды стран) и служит группировкой «по странам» в UI.
	Geo string `json:"geo,omitempty"`

	TestResult TestResult      `json:"test_result,omitempty"`
	Traffic    TrafficCounters `json:"traffic"`

	// Точки расширения конфига (FR-UI-084/085/256), применяются после подтверждения:
	// CustomHash — хэш подтверждённого содержимого, флаг вычисляется (customhash.go).
	CustomOutboundJSON    string `json:"custom_outbound_json,omitempty"`
	CustomConfigJSON      string `json:"custom_config_json,omitempty"`
	CustomFieldsConfirmed bool   `json:"custom_fields_confirmed,omitempty"`
	CustomHash            string `json:"-"`

	Stream *StreamSettings `json:"stream,omitempty"`

	// Detail сериализуется вручную (по полю type) — см. MarshalJSON.
	Detail Detail `json:"-"`

	// LegacyBean — исходный bean JSON NekoBox; хранится в profiles.legacy_bean
	// на время миграции/верификации (core-port-plan §5.3); в канонический JSON
	// профиля не входит.
	LegacyBean string `json:"-"`

	CreatedAtMs int64 `json:"created_at_ms,omitempty"`
	UpdatedAtMs int64 `json:"updated_at_ms,omitempty"`
}

// Detail — типоспецифичная часть профиля (bean-иерархия nekoray).
type Detail interface {
	isProfileDetail()
}

// SocksHTTPDetail — SocksHttpBean (socks4/socks5/http), FR-UI-091.
type SocksHTTPDetail struct {
	Proto    string `json:"proto,omitempty"` // socks4 | socks5 | http
	Username string `json:"username,omitempty"`
	Password string `json:"password,omitempty"`
}

// ShadowsocksDetail — ShadowSocksBean, FR-UI-092.
type ShadowsocksDetail struct {
	Method     string `json:"method,omitempty"`
	Password   string `json:"password,omitempty"`
	Plugin     string `json:"plugin,omitempty"`       // «plugin;arg=val;…»
	UDPOverTCP int    `json:"udp_over_tcp,omitempty"` // 0 off | 1 v1 | 2 v2
}

// VMessDetail — VMessBean, FR-UI-093.
type VMessDetail struct {
	UUID     string `json:"uuid,omitempty"`
	AlterID  int    `json:"alter_id,omitempty"`
	Security string `json:"security,omitempty"` // auto/none/zero/aes-128-gcm/chacha20-poly1305
}

// TrojanVlessDetail — TrojanVLESSBean (trojan|vless по Profile.Type), FR-UI-094.
type TrojanVlessDetail struct {
	Password string `json:"password,omitempty"` // у VLESS здесь UUID
	Flow     string `json:"flow,omitempty"`     // xtls-rprx-vision
}

// NaiveDetail — NaiveBean, FR-UI-095. Внешнее ядро: тип доступен только при
// включённом флаге enable_external_cores (решение владельца 2026-10-23).
type NaiveDetail struct {
	Username            string `json:"username,omitempty"`
	Password            string `json:"password,omitempty"`
	Proto               string `json:"proto,omitempty"` // https | http
	ExtraHeaders        string `json:"extra_headers,omitempty"`
	SNI                 string `json:"sni,omitempty"`
	Certificate         string `json:"certificate,omitempty"`
	InsecureConcurrency int    `json:"insecure_concurrency,omitempty"`
	DisableLogs         bool   `json:"disable_logs,omitempty"`
}

// Hysteria2Detail — QUICBean(proxy_Hysteria2), FR-UI-096. Имена полей — как в
// sing-box 1.14: server_ports строкой «a:b», hop_interval — Duration-строка
// («30s»); исправление LNB-35 / FR-UI-268.
type Hysteria2Detail struct {
	Password      string `json:"password,omitempty"`
	ObfsPassword  string `json:"obfs_password,omitempty"`
	UploadMbps    int    `json:"upload_mbps,omitempty"`
	DownloadMbps  int    `json:"download_mbps,omitempty"`
	ServerPorts   string `json:"server_ports,omitempty"` // «a:b» — меняет отображение адреса
	HopInterval   string `json:"hop_interval,omitempty"` // Duration-строка, напр. «30s»
	SNI           string `json:"sni,omitempty"`
	ALPN          string `json:"alpn,omitempty"` // QUICBean.hpp:75 (hysteria2)
	CA            string `json:"ca,omitempty"`
	AllowInsecure bool   `json:"allow_insecure,omitempty"`
	DisableSNI    bool   `json:"disable_sni,omitempty"`
}

// TUICDetail — QUICBean(proxy_TUIC), FR-UI-097. Всегда внутренний sing-box.
type TUICDetail struct {
	UUID              string `json:"uuid,omitempty"`
	Password          string `json:"password,omitempty"`
	CongestionControl string `json:"congestion_control,omitempty"` // bbr | cubic | new_reno
	UDPRelayMode      string `json:"udp_relay_mode,omitempty"`     // native | quic
	ZeroRttHandshake  bool   `json:"zero_rtt_handshake,omitempty"`
	Heartbeat         string `json:"heartbeat,omitempty"` // Duration-строка, напр. «10s»
	SNI               string `json:"sni,omitempty"`
	ALPN              string `json:"alpn,omitempty"`
	CA                string `json:"ca,omitempty"`
	AllowInsecure     bool   `json:"allow_insecure,omitempty"`
	DisableSNI        bool   `json:"disable_sni,omitempty"`
	UDPOverStream     bool   `json:"udp_over_stream,omitempty"` // uos (импорт из Clash)
}

// CustomDetail — CustomBean (FR-UI-099/100a/100b). Core:
// internal | internal-full | external (последний — только за флагом).
type CustomDetail struct {
	Core             string   `json:"core,omitempty"` // internal | internal-full | external
	ExternalCoreName string   `json:"external_core_name,omitempty"`
	Command          []string `json:"command,omitempty"`
	ConfigSuffix     string   `json:"config_suffix,omitempty"`
	ConfigTemplate   string   `json:"config_template,omitempty"`  // плейсхолдеры %mapping_port% … (FR-UI-235)
	MappingPort      int      `json:"mapping_port,omitempty"`     // 0 = случайный
	SocksPort        int      `json:"socks_port,omitempty"`       // 0 = случайный
	OutboundJSON     string   `json:"outbound_json,omitempty"`    // core=internal
	FullConfigJSON   string   `json:"full_config_json,omitempty"` // core=internal-full
}

// ChainDetail — ChainBean (FR-UI-098). Порядок сверху вниз = порядок трафика.
type ChainDetail struct {
	ProfileIDs []int64 `json:"profile_ids,omitempty"`
}

func (*SocksHTTPDetail) isProfileDetail()   {}
func (*ShadowsocksDetail) isProfileDetail() {}
func (*VMessDetail) isProfileDetail()       {}
func (*TrojanVlessDetail) isProfileDetail() {}
func (*NaiveDetail) isProfileDetail()       {}
func (*Hysteria2Detail) isProfileDetail()   {}
func (*TUICDetail) isProfileDetail()        {}
func (*CustomDetail) isProfileDetail()      {}
func (*ChainDetail) isProfileDetail()       {}

// newDetailByType возвращает пустую деталь для типа (nil — тип без детали).
func newDetailByType(t ProfileType) Detail {
	switch t {
	case TypeSOCKS, TypeHTTP:
		return &SocksHTTPDetail{}
	case TypeShadowsocks:
		return &ShadowsocksDetail{}
	case TypeVMess:
		return &VMessDetail{}
	case TypeTrojan, TypeVLESS:
		return &TrojanVlessDetail{}
	case TypeNaive:
		return &NaiveDetail{}
	case TypeHysteria2:
		return &Hysteria2Detail{}
	case TypeTUIC:
		return &TUICDetail{}
	case TypeCustomOutbound, TypeCustomConfig, TypeCustomExternal:
		return &CustomDetail{}
	case TypeChain:
		return &ChainDetail{}
	}
	return nil
}

// MarshalJSON пишет detail как конкретный объект (oneof контракта).
func (p *Profile) MarshalJSON() ([]byte, error) {
	type profileAlias Profile // избегаем рекурсии
	alias := profileAlias(*p)
	m, err := json.Marshal(alias)
	if err != nil {
		return nil, err
	}
	if p.Detail == nil {
		return m, nil
	}
	obj := make(map[string]json.RawMessage)
	if err := json.Unmarshal(m, &obj); err != nil {
		return nil, err
	}
	dm, err := json.Marshal(p.Detail)
	if err != nil {
		return nil, err
	}
	obj["detail"] = dm
	return json.Marshal(obj)
}

// UnmarshalJSON восстанавливает Detail по полю type (ровно одна деталь —
// как oneof в контракте; несоответствие типа — ошибка).
func (p *Profile) UnmarshalJSON(b []byte) error {
	type profileAlias Profile
	var alias profileAlias
	if err := json.Unmarshal(b, &alias); err != nil {
		return err
	}
	*p = Profile(alias)

	var raw struct {
		Detail json.RawMessage `json:"detail"`
	}
	if err := json.Unmarshal(b, &raw); err != nil {
		return err
	}
	if len(raw.Detail) == 0 || string(raw.Detail) == "null" {
		p.Detail = nil
		return nil
	}
	detail := newDetailByType(p.Type)
	if detail == nil {
		return fmt.Errorf("model: unknown profile type %q", p.Type)
	}
	if err := json.Unmarshal(raw.Detail, detail); err != nil {
		return err
	}
	p.Detail = detail
	return nil
}

// Clone возвращает глубокую копию профиля через канонический JSON.
func (p *Profile) Clone() (*Profile, error) {
	b, err := json.Marshal(p)
	if err != nil {
		return nil, err
	}
	var c Profile
	if err := json.Unmarshal(b, &c); err != nil {
		return nil, err
	}
	return &c, nil
}
