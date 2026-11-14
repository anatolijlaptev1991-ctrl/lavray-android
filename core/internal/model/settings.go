// SPDX-License-Identifier: GPL-3.0-only

package model

// SettingType — тип значения настройки (proto SettingType).
type SettingType string

const (
	SettingString     SettingType = "string"
	SettingBool       SettingType = "bool"
	SettingInt        SettingType = "int"
	SettingDouble     SettingType = "double"
	SettingStringList SettingType = "string_list"
	SettingJSON       SettingType = "json"
)

// SettingDescriptor — элемент карты настроек (скелет по Приложению А
// REQUIREMENTS + таблица миграции core-port-plan §5.4). DescribeSettings
// (SettingsService, W6) отдаёт этот каталог UI.
type SettingDescriptor struct {
	Key                    string      `json:"key"`
	Type                   SettingType `json:"type"`
	GroupID                string      `json:"group_id"` // секция UI
	DefaultValue           string      `json:"default_value_json,omitempty"`
	RequiresProfileRestart bool        `json:"requires_profile_restart,omitempty"`
	RequiresAppRestart     bool        `json:"requires_app_restart,omitempty"`
	ShellOwned             bool        `json:"shell_owned,omitempty"` // читает оболочка, ядро хранит
	// DeprecatedBy — прежнее имя ключа DataStore NekoBox (для миграции §5.4).
	DeprecatedBy string `json:"deprecated_by,omitempty"`
	// Internal — служебный ключ ядра: хранится в таблице settings, но не
	// отдаётся UI (DescribeSettings/GetSettings) и не принимается SetSettings.
	Internal bool `json:"internal,omitempty"`
	// BackupExclude — ключ не входит в резервную копию (T-83): корень доверия
	// обновлений и счётчики защиты от отката принадлежат установке, а не профилю
	// пользователя; восстановление копии не должно их откатывать.
	BackupExclude bool `json:"backup_exclude,omitempty"`
}

// SettingHWIDID — служебный ключ с постоянным HWID установки (D-02).
const SettingHWIDID = "sub.hwid_id"

// SettingGroups — секции UI настроек (stable-ключи).
var SettingGroups = []string{
	"inbound", "test", "routing", "sub", "remember", "ui", "tun",
	"hotkeys", "core", "security", "mux", "update", "misc", "agent",
}

// SettingDescriptors — каталог известных ключей LAVRay (скелет W1 по
// Приложению А; дефолты новых ключей задаёт DescribeSettings).
//
// Значения хранятся в таблице settings как JSON (тип — из дескриптора).
// extraCore.core_map NekoBox НЕ переносится (DROP, FR-UI-175).
func SettingDescriptors() []SettingDescriptor {
	return append(baseDescriptors(), updateInternalDescriptors()...)
}

func baseDescriptors() []SettingDescriptor {
	return []SettingDescriptor{
		// ---- Inbound (FR-UI-157/158/279) ----
		{Key: "inbound.address", Type: SettingString, GroupID: "inbound", DefaultValue: `"127.0.0.1"`, RequiresProfileRestart: true, DeprecatedBy: "inbound_address"},
		{Key: "inbound.port", Type: SettingInt, GroupID: "inbound", DefaultValue: `2080`, RequiresProfileRestart: true, DeprecatedBy: "inbound_socks_port"},
		{Key: "inbound.auth.user", Type: SettingString, GroupID: "inbound", RequiresProfileRestart: true, DeprecatedBy: "inbound_auth.user"},
		{Key: "inbound.auth.pass", Type: SettingString, GroupID: "inbound", RequiresProfileRestart: true, DeprecatedBy: "inbound_auth.pass"},
		{Key: "inbound.custom_inbound", Type: SettingString, GroupID: "inbound", RequiresProfileRestart: true, DeprecatedBy: "custom_inbound"},

		// ---- Тесты (URL- и полный тест, T-84) ----
		// Весь тестовый трафик идёт через исходящее проверяемого профиля. Адреса
		// по умолчанию — решение владельца 2026-10-29; URL — http/https с
		// хостом, test.udp_target — host:port.
		{Key: "test.url", Type: SettingString, GroupID: "test", DefaultValue: `"https://www.gstatic.com/generate_204"`, DeprecatedBy: "test_url"},
		{Key: "test.url_dl", Type: SettingString, GroupID: "test", DefaultValue: `"http://cachefly.cachefly.net/10mb.test"`, DeprecatedBy: "test_url_dl"},
		{Key: "test.dl_timeout", Type: SettingInt, GroupID: "test", DefaultValue: `10`, DeprecatedBy: "test_dl_timeout"},
		// Параллельные тестовые инстансы (каждый — отдельный box.Box), 1…16.
		{Key: "test.concurrent", Type: SettingInt, GroupID: "test", DefaultValue: `5`, DeprecatedBy: "test_concurrent"},
		{Key: "test.url_ip", Type: SettingString, GroupID: "test", DefaultValue: `"https://cloudflare.com/cdn-cgi/trace"`},
		{Key: "test.udp_target", Type: SettingString, GroupID: "test", DefaultValue: `"1.1.1.1:53"`},

		// ---- Маршрутизация ----
		{Key: "routing.active_preset", Type: SettingString, GroupID: "routing", DefaultValue: `"lav-ru"`, RequiresProfileRestart: true, DeprecatedBy: "active_routing"},
		{Key: "routing.custom_global", Type: SettingString, GroupID: "routing", RequiresProfileRestart: true, DeprecatedBy: "custom_route"},

		// ---- Подписки (LNB-33: use_proxy default true) ----
		{Key: "sub.user_agent", Type: SettingString, GroupID: "sub", DeprecatedBy: "user_agent2"},
		{Key: "sub.use_proxy", Type: SettingBool, GroupID: "sub", DefaultValue: `true`, DeprecatedBy: "sub_use_proxy"},
		{Key: "sub.clear", Type: SettingBool, GroupID: "sub", DeprecatedBy: "sub_clear"},
		{Key: "sub.insecure", Type: SettingBool, GroupID: "sub", DeprecatedBy: "sub_insecure"},
		{Key: "sub.auto_update", Type: SettingInt, GroupID: "sub", DefaultValue: `0`, DeprecatedBy: "sub_auto_update"},
		{Key: "sub.hwid_enabled", Type: SettingBool, GroupID: "sub", DefaultValue: `false`},
		// D-02: постоянный идентификатор установки для x-hwid. Создаётся ядром
		// один раз при первом обновлении подписки с включённым sub.hwid_enabled
		// и не меняется; при выключенном HWID не создаётся вовсе.
		{Key: SettingHWIDID, Type: SettingString, GroupID: "sub", Internal: true},

		// ---- Запоминание ----
		{Key: "remember.id", Type: SettingInt, GroupID: "remember", DefaultValue: `-1`, DeprecatedBy: "remember_id"},
		{Key: "remember.enable", Type: SettingBool, GroupID: "remember", DeprecatedBy: "remember_enable"},
		{Key: "remember.modes", Type: SettingStringList, GroupID: "remember", DeprecatedBy: "spmode2"},

		// ---- UI (shell_owned; conn_stat всегда true — FR-UI-274) ----
		{Key: "ui.theme", Type: SettingString, GroupID: "ui", ShellOwned: true, DeprecatedBy: "theme"},
		{Key: "ui.lang", Type: SettingString, GroupID: "ui", ShellOwned: true, DeprecatedBy: "language"},
		{Key: "ui.window_size", Type: SettingString, GroupID: "ui", ShellOwned: true, DeprecatedBy: "mw_size"},
		{Key: "ui.splitter", Type: SettingString, GroupID: "ui", ShellOwned: true, DeprecatedBy: "splitter_state"},
		{Key: "ui.start_minimal", Type: SettingBool, GroupID: "ui", ShellOwned: true, DeprecatedBy: "start_minimal"},
		{Key: "ui.max_log_lines", Type: SettingInt, GroupID: "ui", ShellOwned: true, DefaultValue: `1000`, DeprecatedBy: "max_log_line"},
		{Key: "ui.conn_stat", Type: SettingBool, GroupID: "ui", DefaultValue: `true`, DeprecatedBy: "conn_stat"},
		{Key: "ui.log_ignore", Type: SettingStringList, GroupID: "ui", ShellOwned: true, DeprecatedBy: "log_ignore"},
		{Key: "ui.current_group", Type: SettingInt, GroupID: "ui", ShellOwned: true, DeprecatedBy: "current_group"},

		// ---- TUN (+tun.kill_switch FR-UI-275, +tun.interface_name) ----
		{Key: "tun.fake_dns", Type: SettingBool, GroupID: "tun", DeprecatedBy: "fakedns"},
		{Key: "tun.enabled", Type: SettingBool, GroupID: "tun", DeprecatedBy: "vpn_internal_tun"},
		{Key: "tun.impl", Type: SettingInt, GroupID: "tun", DeprecatedBy: "vpn_impl"},
		{Key: "tun.mtu", Type: SettingInt, GroupID: "tun", DefaultValue: `9000`, DeprecatedBy: "vpn_mtu"},
		{Key: "tun.ipv6", Type: SettingBool, GroupID: "tun", DeprecatedBy: "vpn_ipv6"},
		{Key: "tun.hide_console", Type: SettingBool, GroupID: "tun", ShellOwned: true, DeprecatedBy: "vpn_hide_console"},
		{Key: "tun.strict_route", Type: SettingBool, GroupID: "tun", DeprecatedBy: "vpn_strict_route"},
		{Key: "tun.bypass_process", Type: SettingString, GroupID: "tun", DeprecatedBy: "vpn_bypass_process"},
		{Key: "tun.bypass_cidr", Type: SettingString, GroupID: "tun", DeprecatedBy: "vpn_bypass_cidr"},
		{Key: "tun.whitelist", Type: SettingBool, GroupID: "tun", DeprecatedBy: "vpn_rule_white"},
		{Key: "tun.kill_switch", Type: SettingBool, GroupID: "tun", DefaultValue: `false`},
		{Key: "tun.interface_name", Type: SettingString, GroupID: "tun", DefaultValue: `"lavray-tun"`},

		// ---- Хоткеи (shell_owned) ----
		{Key: "hotkeys.main_window", Type: SettingString, GroupID: "hotkeys", ShellOwned: true, DeprecatedBy: "hk_mw"},
		{Key: "hotkeys.group", Type: SettingString, GroupID: "hotkeys", ShellOwned: true, DeprecatedBy: "hk_group"},
		{Key: "hotkeys.routing", Type: SettingString, GroupID: "hotkeys", ShellOwned: true, DeprecatedBy: "hk_route"},
		{Key: "hotkeys.system_proxy_menu", Type: SettingString, GroupID: "hotkeys", ShellOwned: true, DeprecatedBy: "hk_spmenu"},

		// ---- Ядро (clash_api hardening — core-port-plan §6.3) ----
		{Key: "core.clash_api.enabled", Type: SettingBool, GroupID: "core", DeprecatedBy: "core_box_clash_api"},
		{Key: "core.clash_api.secret", Type: SettingString, GroupID: "core", DeprecatedBy: "core_box_clash_api_secret"},
		{Key: "core.underlying_dns", Type: SettingString, GroupID: "core", DefaultValue: `"local"`, DeprecatedBy: "core_box_underlying_dns"},
		// T-49: уровень лога sing-box (trace|debug|info|warn|error|fatal|panic);
		// неизвестное значение cfgbuild заменяет на "info".
		{Key: "log.level", Type: SettingString, GroupID: "core", DefaultValue: `"info"`, RequiresProfileRestart: true, DeprecatedBy: "log_level"},
		// 0.2.1: после подключения ядро один раз запрашивает test.url через исходящее
		// профиля; неудача — предупреждение «Подключено, но трафик не идёт».
		{Key: "connect.health_check", Type: SettingBool, GroupID: "core", DefaultValue: `true`},
		// Хвосты B (7.3): при падении ядра в режиме системного прокси оболочка НЕ снимает
		// прокси — приложения не уходят напрямую, а получают отказ. Читает оболочка.
		{Key: "connect.kill_switch_proxy", Type: SettingBool, GroupID: "core", ShellOwned: true, DefaultValue: `false`},

		// ---- Безопасность ----
		{Key: "security.allow_insecure_default", Type: SettingBool, GroupID: "security", DeprecatedBy: "skip_cert"},
		{Key: "security.utls_fingerprint", Type: SettingString, GroupID: "security", DeprecatedBy: "utlsFingerprint"},

		// ---- Mux ----
		{Key: "mux.protocol", Type: SettingString, GroupID: "mux", DefaultValue: `"h2mux"`, DeprecatedBy: "mux_protocol"},
		{Key: "mux.concurrency", Type: SettingInt, GroupID: "mux", DefaultValue: `-1`, DeprecatedBy: "mux_concurrency"},
		{Key: "mux.padding", Type: SettingBool, GroupID: "mux", DeprecatedBy: "mux_padding"},
		{Key: "mux.default_on", Type: SettingBool, GroupID: "mux", DeprecatedBy: "mux_default_on"},

		// ---- Агенты (MCP): off | read | control; смена действует без перезапуска ----
		{Key: "agent.mode", Type: SettingString, GroupID: "agent", DefaultValue: `"off"`},

		// ---- Прочее ----
		{Key: "win.sp_format", Type: SettingString, GroupID: "misc", ShellOwned: true, DeprecatedBy: "sp_format"},
		{Key: "update.channel", Type: SettingString, GroupID: "update", DefaultValue: `"stable"`, DeprecatedBy: "check_include_pre"},
		// T-75: фоновая проверка раз в ~6 ч; выключается пользователем.
		{Key: "update.auto_check", Type: SettingBool, GroupID: "update", DefaultValue: `true`},
		{Key: "stats.interval_ms", Type: SettingInt, GroupID: "misc", DefaultValue: `1000`, DeprecatedBy: "traffic_loop_interval"},
		{Key: "share.vmess_old_format", Type: SettingBool, GroupID: "misc", DefaultValue: `true`, DeprecatedBy: "old_share_link_format"},
		{Key: "enable_external_cores", Type: SettingBool, GroupID: "core", DefaultValue: `false`},
		{Key: "web.bind", Type: SettingString, GroupID: "core"},
		{Key: "web.tls", Type: SettingBool, GroupID: "core", DefaultValue: `false`},
	}
}
