// SPDX-License-Identifier: GPL-3.0-only

// Package cfgbuild — конфигбилдер LAVRay (W4: §3.4 шаги 1–3; W5: шаги 4–7
// плана core-port-plan.md): allowlist-мерж custom_config, outbounds из
// профилей всех типов модели, цепочки с тегами proxy/g-/c-, inbounds
// (mixed + TUN), DNS из пресета (simple/advanced), route-правила пресета с
// rule-set из поставки .srs, cache_file, kill-switch.
//
// Оракул порта: nekoray/db/ConfigBuilder.cpp + fmt/Bean2CoreObj_box.cpp.
package cfgbuild

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"github.com/anatolijlaptev1991-ctrl/lavray-android/core/internal/model"
	"github.com/anatolijlaptev1991-ctrl/lavray-android/core/internal/presets"
)

// ProfileLookup — разрешение id → профиль (store.GetProfile, W6 подставит
// реализацию хранилища; в тестах — карта extra_profiles).
type ProfileLookup func(id int64) (*model.Profile, error)

// MuxSettings — глобальные настройки мультиплексирования (§3.1.10);
// дефолты соответствуют model.SettingDescriptors (mux.*).
type MuxSettings struct {
	Protocol    string `json:"protocol"`    // h2mux | smux | yamux
	Concurrency int    `json:"concurrency"` // <= 0 — мультиплексирование выключено глобально
	Padding     bool   `json:"padding"`
	DefaultOn   bool   `json:"default_on"` // multiplex_status == 0 (default) → включён?
}

// InboundSettings — слушатель mixed (inbound.* из §5.4; mixed обслуживает
// socks и http одновременно — «mixed/socks/http по настройкам» оракула).
type InboundSettings struct {
	Address           string `json:"address"`             // default 127.0.0.1 (не-loopback требует auth — FR-UI-279)
	Port              int    `json:"port"`                // default 2080; <=0 — слушатель выключен
	AuthUser          string `json:"auth_user,omitempty"` // оба поля — иначе auth не применяется (оракул NeedAuth)
	AuthPass          string `json:"auth_pass,omitempty"`
	CustomInboundJSON string `json:"custom_inbound,omitempty"` // {"inbounds":[...]} — добавка к сгенерированным
}

// TunSettings — внутренний TUN (tun.* из §5.4; FR-UI-178/184, kill-switch
// FR-UI-275).
type TunSettings struct {
	Enabled       bool   `json:"enabled"`
	InterfaceName string `json:"interface_name,omitempty"` // default lavray-tun (darwin: utun9 — THREAT-MODEL «детект VPN», имя настраиваемое)
	MTU           int    `json:"mtu,omitempty"`            // default 9000
	Stack         string `json:"stack,omitempty"`          // gvisor | system | mixed; default gvisor
	StrictRoute   bool   `json:"strict_route,omitempty"`
	IPv6          bool   `json:"ipv6,omitempty"`           // +fd4c:4156:5259::1/126
	FakeDNS       bool   `json:"fake_dns,omitempty"`       // FR-UI-151
	BypassProcess string `json:"bypass_process,omitempty"` // многострочник имён процессов
	BypassCIDR    string `json:"bypass_cidr,omitempty"`    // многострочник CIDR
	Whitelist     bool   `json:"whitelist,omitempty"`      // инвертирует ТОЛЬКО tun-списки (поправка №28)
	KillSwitch    bool   `json:"kill_switch,omitempty"`    // FR-UI-275: форсирует strict_route
}

// BuildSettings — параметры сборки, влияющие на конфиг (подмножество карты
// настроек; W6 заполняет из settings-хранилища).
type BuildSettings struct {
	LogLevel             string      `json:"log_level"`              // log.level (default "info")
	AllowInsecureDefault bool        `json:"allow_insecure_default"` // security.allow_insecure_default (skip_cert)
	Mux                  MuxSettings `json:"mux"`

	Inbound InboundSettings `json:"inbound"`
	Tun     TunSettings     `json:"tun"`

	// UnderlyingDNS — core.underlying_dns (dns-local); "" → пресет → "local".
	UnderlyingDNS string `json:"underlying_dns,omitempty"`
	// CustomRouteGlobalJSON — routing.custom_global ({"rules":[...]}) —
	// пользовательские глобальные правила поверх пресета.
	CustomRouteGlobalJSON string `json:"custom_route_global,omitempty"`
	// GeoDir — каталог поставки .srs (rule-set type=local, FR-UI-266/LNB-31);
	// "" → относительный "geo" (рабочий каталог процесса, W6 подставит абсолютный).
	GeoDir string `json:"geo_dir,omitempty"`
	// CacheFilePath — experimental.cache_file.path; "" → путь по умолчанию sing-box.
	CacheFilePath string `json:"cache_file_path,omitempty"`
	// GOOS — платформа для процессных правил пресета ("" → runtime.GOOS).
	GOOS    string         `json:"goos,omitempty"`
	OpenWRT bool           `json:"openwrt,omitempty"` // --platform openwrt: без процессных правил (SCHEMA §2.1)
	TProxy  TProxySettings `json:"tproxy"`            // tproxy-вход шлюза (tproxy.go)
	// DNSRemoteDirect — запросы к dns-remote идут напрямую, без detour в
	// туннель (карточка «DoH» экрана DNS); по умолчанию — внутри туннеля.
	DNSRemoteDirect bool `json:"dns_remote_direct,omitempty"`
}

// DefaultSettings — значения по умолчанию (model.SettingDescriptors).
func DefaultSettings() *BuildSettings {
	return &BuildSettings{
		LogLevel: "info",
		Mux:      MuxSettings{Protocol: "h2mux", Concurrency: -1},
		Inbound:  InboundSettings{Address: "127.0.0.1", Port: 2080},
		Tun:      TunSettings{MTU: 9000, Stack: "gvisor"},
	}
}

// BuildRequest — вход сборки конфига.
type BuildRequest struct {
	Profile   *model.Profile  // целевой профиль (в т.ч. chain / custom)
	Group     *model.Group    // группа профиля (front_proxy_id), может быть nil
	Settings  *BuildSettings  // nil → DefaultSettings
	Lookup    ProfileLookup   // обязательна для chain/front proxy
	Preset    *presets.Preset // активный маршрутный пресет; nil → нейтральный (см. neutralPreset)
	ForTest   bool            // §3.1.9: режим теста (пустые inbounds, без user-правил)
	ForExport bool            // §3.1.9: экспорт (без auto_detect_interface)
}

// BuildResult — результат сборки.
type BuildResult struct {
	// Config — итоговый конфиг sing-box (map; маршалится с сортировкой
	// ключей — детерминизм golden).
	Config map[string]any

	// OutboundTag — тег «последней мили» цепочки, всегда "proxy".
	OutboundTag string

	// OutboundTags — теги outbound'ов профиля (для allowlist-мержа custom_config → outbounds).
	OutboundTags []string

	// IgnoreConnTags — теги промежуточных outbound'ов (index != 0): их соединения не пользовательские.
	IgnoreConnTags []string

	// ServerDomains — домены серверов цепочки («full:<домен>») для dns-direct правил (§3.1.4).
	ServerDomains []string

	// ExternalCores — записи о внешних ядрах (всегда пуст в W4: внешние
	// ядра за флагом enable_external_cores, W6).
	ExternalCores []ExternalCore

	// Warnings — замечания сборки, не мешающие запуску (напр. проигнорированная настройка).
	Warnings []string
}

// ExternalCore — описание внешнего ядра (заполняется только в W6 за флагом).
type ExternalCore struct {
	ProfileID int64
	Tag       string
	Command   []string
}

// Build собирает конфиг sing-box для профиля (порт BuildConfig,
// ConfigBuilder.cpp:73–92 + BuildConfigSingBox W4-часть).
func Build(req BuildRequest) (*BuildResult, error) {
	if req.Profile == nil {
		return nil, invalidArgument("profile", "profile is required")
	}
	st := req.Settings
	if st == nil {
		st = DefaultSettings()
	}
	res := &BuildResult{}

	var cfg map[string]any
	if isInternalFull(req.Profile) {
		// L2 аудита №1: произвольный конфиг ядра («как есть»), пришедший из
		// импорта, применяется ТОЛЬКО после подтверждения пользователем —
		// ровно как custom_config (LNB-25/FR-UI-257). Иначе импортированный
		// профиль открывал 0.0.0.0:9 без ведома пользователя.
		if !req.Profile.CustomFieldsConfirmed {
			return nil, invalidArgument("custom.bean.core",
				"internal-full конфиг не подтверждён: откройте diff и подтвердите (PreviewCustomFields → ConfirmCustomFields)")
		}
		var err error
		cfg, err = internalFullConfig(req.Profile)
		if err != nil {
			return nil, err
		}
		// T-08: «как есть» не значит «с путями к файлам» (log.output,
		// experimental.cache_file.path, route.rule_set[].path, …).
		if err := rejectFileKeys("", cfg); err != nil {
			return nil, err
		}
	} else {
		var err error
		cfg, err = buildSingBoxConfig(req, st, res)
		if err != nil {
			return nil, err
		}
	}

	// custom_config мержится ТОЛЬКО по allowlist (§3.1.1, LNB-25) и только
	// после подтверждения пользователя для импортированных профилей
	// (FR-UI-257; не подтверждённые поля отбрасывает уже парсер W3).
	if req.Profile.CustomConfigJSON != "" && req.Profile.CustomFieldsConfirmed {
		if err := MergeCustomConfig(cfg, req.Profile.CustomConfigJSON, res.OutboundTags); err != nil {
			return nil, err
		}
	}
	res.Config = cfg
	return res, nil
}

// NeutralPreset — экспорт neutralPreset для слоя API (DNS-настройки должны
// применяться и когда активный пресет не найден, A5 аудита round 1).
func NeutralPreset() *presets.Preset { return neutralPreset() }

// neutralPreset — пресет по умолчанию при Preset==nil (W6 всегда передаёт
// routing.active_preset; значение настройки по умолчанию — "lav-ru", здесь —
// эквивалент global: proxy final, sniffing для маршрутизации, раздельный
// DNS google-DoH/local с dns_routing).
func neutralPreset() *presets.Preset {
	return &presets.Preset{
		Routing: presets.Routing{
			DefOutbound:  "proxy",
			SniffingMode: 1,
		},
		DNS: presets.DNSBlock{
			Mode:           "simple",
			RemoteDNS:      "https://dns.google/dns-query",
			DirectDNS:      "local",
			RoutingEnabled: true,
		},
	}
}

// buildSingBoxConfig — каркас + outbounds (W4) + inbounds/DNS/route/
// experimental (W5, порт BuildConfigSingBox ConfigBuilder.cpp:389–771).
func buildSingBoxConfig(req BuildRequest, st *BuildSettings, res *BuildResult) (map[string]any, error) {
	// Частичные настройки (W6 может передать подмножество полей): пустой или
	// неизвестный уровень лога — как дефолт "info" (SettingDescriptors
	// log.level; T-49: normalizeLogLevel).
	logLevel := normalizeLogLevel(st.LogLevel)
	cfg := map[string]any{
		"log": map[string]any{"level": logLevel},
	}

	// Inbounds: mixed (+custom) и TUN (FR-UI-279: в режиме TUN loopback-слушатель
	// mixed остаётся, как в NekoBox; forTest — без inbounds, но секция
	// присутствует — пустой массив, как в оракуле).
	inbounds, err := buildInbounds(st, req.ForTest)
	if err != nil {
		return nil, err
	}
	if inbounds == nil {
		inbounds = []any{}
	}

	// Цепочка (или одиночный профиль) → outbounds с тегами proxy/g-/c-.
	ents, err := resolveChain(req)
	if err != nil {
		return nil, err
	}
	outbounds, err := buildChainOutbounds(0, ents, st, res)
	if err != nil {
		return nil, err
	}

	// outbounds-минимум (оракул): direct, bypass, block.
	outbounds = append(outbounds,
		map[string]any{"type": "direct", "tag": "direct"},
		map[string]any{"type": "direct", "tag": "bypass"},
		map[string]any{"type": "block", "tag": "block"},
	)
	cfg["inbounds"] = inbounds
	cfg["outbounds"] = outbounds

	preset := req.Preset
	if preset == nil {
		preset = neutralPreset()
	}

	// DNS строится ПЕРВЫМ (как в оракуле): rule-set-определения geosite:
	// из dns-правил собираются раньше маршрутных — порядок route.rule_set
	// детерминирован порядком встречи.
	rs := newRuleSetCollector(st)
	dnsObj, err := buildDNS(preset, st, res, req.ForTest, rs)
	if err != nil {
		return nil, err
	}
	cfg["dns"] = dnsObj

	routeObj, err := buildRoute(preset, st, res, req.ForTest, req.ForExport, rs)
	if err != nil {
		return nil, err
	}
	cfg["route"] = routeObj

	// experimental.cache_file — включён всегда в рабочих конфигах (LNB-36,
	// FR-UI-266: rule-set кэш + старт без сети).
	if !req.ForTest {
		cfg["experimental"] = buildExperimental(st)
	}
	return cfg, nil
}

// ---- internal-full ----------------------------------------------------------

func isInternalFull(p *model.Profile) bool { return model.IsInternalFull(p) }

func internalFullConfig(p *model.Profile) (map[string]any, error) {
	d := p.Detail.(*model.CustomDetail)
	raw := strings.TrimSpace(d.FullConfigJSON)
	if raw == "" {
		return nil, invalidArgument("custom", "custom internal-full profile: config JSON is empty")
	}
	var cfg map[string]any
	if err := json.Unmarshal([]byte(raw), &cfg); err != nil {
		return nil, invalidArgument("custom", "custom internal-full profile: config JSON: %v", err)
	}
	return cfg, nil
}

// ---- цепочки (порт BuildChain + BuildChainInternal) --------------------------

// resolveChain — список профилей цепочки (порядок = порядок трафика:
// ents[0] — последняя миля, ents[len-1] — вход) + групповой front proxy
// (ConfigBuilder.cpp:94–148). chain-in-chain и отсутствующие члены — ошибки.
func resolveChain(req BuildRequest) ([]*model.Profile, error) {
	lookup := func(id int64) (*model.Profile, error) {
		if req.Lookup == nil {
			return nil, failedPrecondition("chain", "profile lookup is not available")
		}
		return req.Lookup(id)
	}
	var resolveErr error
	resolve := func(ent *model.Profile) []*model.Profile {
		if ent.Type != model.TypeChain {
			return []*model.Profile{ent}
		}
		d, ok := ent.Detail.(*model.ChainDetail)
		if !ok || d == nil {
			resolveErr = invalidArgument("detail", "chain %d: missing or wrong detail type", ent.ID)
			return nil
		}
		ids := d.ProfileIDs
		resolved := make([]*model.Profile, 0, len(ids))
		// Оракул реверсирует список: хранимый порядок — сверху вниз (вход →
		// выход), порядок итерации — выход → вход.
		for i := len(ids) - 1; i >= 0; i-- {
			m, err := lookup(ids[i])
			if err != nil {
				resolveErr = err // сбой стора — не «профиль пропал» (N2)
				return nil
			}
			if m == nil {
				resolveErr = failedPrecondition("chain", "chain missing ent: %d", ids[i])
				return nil
			}
			if m.Type == model.TypeChain {
				resolveErr = failedPrecondition("chain", "chain in chain is not allowed: %d", ids[i])
				return nil
			}
			resolved = append(resolved, m)
		}
		return resolved
	}

	ents := resolve(req.Profile)
	if resolveErr != nil {
		return nil, resolveErr
	}
	// L12 аудита №1: пустая цепочка раньше давала конфиг без outbound «proxy»,
	// при этом route.final=proxy — sing-box падал на старте, а Connect
	// сообщал успех. Пустая цепочка — ошибка валидации на месте.
	if len(ents) == 0 {
		return nil, invalidArgument("chain", "цепочка пуста: нет профилей для outbound «proxy»")
	}

	// Групповой front proxy добавляется в конец (становится входом).
	if req.Group != nil && req.Group.FrontProxyID != nil && *req.Group.FrontProxyID >= 0 {
		f, err := lookup(*req.Group.FrontProxyID)
		if err != nil || f == nil {
			return nil, failedPrecondition("chain", "front proxy ent not found")
		}
		ents = append(ents, resolve(f)...)
		if resolveErr != nil {
			return nil, resolveErr
		}
	}
	return ents, nil
}

// buildChainOutbounds — порт BuildChainInternal (ConfigBuilder.cpp:174–385),
// только внутренние sing-box-outbound'ы (внешние ядра — W6 за флагом).
//
// Теги: index 0 → "proxy"; последний → "g-<id>" (дедуплицируется);
// промежуточные → "c-<chainId>-<id>". detour каждого outbound'а указывает
// на предыдущий по направлению к клиенту.
func buildChainOutbounds(chainID int, ents []*model.Profile, st *BuildSettings, res *BuildResult) ([]map[string]any, error) {
	chainTag := "c-" + strconv.Itoa(chainID)
	var outbounds []map[string]any
	globalProfiles := map[int64]bool{}
	muxApplied := false

	for index, ent := range ents {
		tagOut := chainTag + "-" + strconv.FormatInt(ent.ID, 10)

		needGlobal := false
		isFirstProfile := index == len(ents)-1
		if isFirstProfile {
			needGlobal = true
			tagOut = "g-" + strconv.FormatInt(ent.ID, 10)
		}
		if chainID == 0 && index == 0 {
			needGlobal = false
			tagOut = "proxy"
		}

		// ignoreConnTag: все, кроме "proxy".
		if index != 0 {
			res.IgnoreConnTags = append(res.IgnoreConnTags, tagOut)
		}

		// Патч detour предыдущего outbound'а. NB (ревью N1): оракул
		// (ConfigBuilder.cpp:212→219) делает continue ДО патча detour; здесь
		// патч после — поведение ненаблюдаемо (дедупликация мёртвый код:
		// needGlobal истинен только на последнем индексе).
		if index > 0 && len(outbounds) > 0 {
			outbounds[len(outbounds)-1]["detour"] = tagOut
		}

		if needGlobal {
			if globalProfiles[ent.ID] {
				continue
			}
			globalProfiles[ent.ID] = true
		}

		if index == 0 {
			res.OutboundTag = tagOut
		}

		// Валидация адреса/порта (LNB-15: порт 0 невалиден; FR-UI-083 —
		// пробелы вырезаются на уровне модели). Для custom internal адрес
		// живёт в JSON outbound'а — валидируем только порт-заглушку.
		if err := validateEnt(ent); err != nil {
			return nil, err
		}

		outbound, err := buildOutbound(ent, st)
		if err != nil {
			return nil, err
		}
		outbound["tag"] = tagOut
		res.OutboundTags = append(res.OutboundTags, tagOut)

		// mux (§3.1.10): только vmess/trojan/vless; grpc/quic/tls-http
		// выключают; явное stream-решение перекрывает; flow выключает;
		// применяется не более одного раза на цепочку.
		needMux := needMuxFor(ent, outbound, st)
		if !muxApplied && needMux {
			outbound["multiplex"] = map[string]any{
				"enabled":     true,
				"protocol":    st.Mux.Protocol,
				"padding":     st.Mux.Padding,
				"max_streams": st.Mux.Concurrency,
			}
			muxApplied = true
		}

		// custom_outbound профиля — мерж в собственный outbound (точка
		// расширения FR-UI-084; для импортированных — после подтверждения,
		// LNB-25/FR-UI-257).
		if ent.CustomOutboundJSON != "" && ent.CustomFieldsConfirmed {
			var custom map[string]any
			if err := json.Unmarshal([]byte(ent.CustomOutboundJSON), &custom); err != nil {
				return nil, invalidArgument("custom_outbound", "profile %d: invalid JSON: %v", ent.ID, err)
			}
			// T-08: custom_outbound не должен приносить пути к файлам.
			if err := rejectFileKeys("custom_outbound", custom); err != nil {
				return nil, err
			}
			MergeJSON(outbound, custom)
		}

		// Домены серверов цепочки → dns-direct full: (§3.1.4, потребляет W5).
		serverAddress := ent.Address
		if ent.Type == model.TypeCustomOutbound {
			if d, ok := ent.Detail.(*model.CustomDetail); ok {
				var o map[string]any
				if json.Unmarshal([]byte(d.OutboundJSON), &o) == nil {
					if s, ok := o["server"].(string); ok && s != "" {
						serverAddress = s
					}
				}
			}
		}
		if serverAddress != "" && !isIPAddr(serverAddress) {
			full := "full:" + serverAddress
			if !containsStr(res.ServerDomains, full) {
				res.ServerDomains = append(res.ServerDomains, full)
			}
		}

		outbounds = append(outbounds, outbound)
	}
	return outbounds, nil
}

// needMuxFor — порт mux-логики оракула (ConfigBuilder.cpp:327–345).
func needMuxFor(ent *model.Profile, outbound map[string]any, st *BuildSettings) bool {
	needMux := false
	switch ent.Type {
	case model.TypeVMess, model.TypeTrojan, model.TypeVLESS:
		needMux = st.Mux.Concurrency > 0
	}
	s := ent.Stream
	if s != nil {
		if s.Network == "grpc" || s.Network == "quic" || (s.Network == "http" && s.Security == "tls") {
			needMux = false
		}
		switch s.Multiplex {
		case 0:
			if !st.Mux.DefaultOn {
				needMux = false
			}
		case 1:
			// Явное per-profile «вкл» не может включить mux при глобально
			// выключенном (Concurrency <= 0): нет валидной concurrency —
			// применить нечего (см. TestMuxRules, concurrency=-1).
			needMux = st.Mux.Concurrency > 0
		case 2:
			needMux = false
		}
	}
	if ent.Type == model.TypeVLESS {
		if flow, ok := outbound["flow"].(string); ok && flow != "" {
			needMux = false
		}
	}
	return needMux
}

func containsStr(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}

// MarshalIndent — детерминированный (ключи отсортированы) JSON конфига.
func MarshalIndent(cfg map[string]any) ([]byte, error) {
	b, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("cfgbuild: marshal config: %w", err)
	}
	return b, nil
}
