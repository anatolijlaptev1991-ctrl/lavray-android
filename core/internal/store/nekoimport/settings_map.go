// SPDX-License-Identifier: GPL-3.0-only

package nekoimport

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

// settings_map.go — настройки: groups/nekobox.json (DataStore NekoBox) →
// ключи LAVRay по таблице docs/stage3/core-port-plan.md §5.4. Здесь только
// сопоставление и приведение типа; допустимость значения (диапазоны, адреса)
// проверяет слой API тем же валидатором, что и SetSettings.

// settingRule — одна строка таблицы: ключ NekoBox → ключ LAVRay.
type settingRule struct {
	neko    string
	lavray  string
	conv    func(json.RawMessage) (string, bool) // значение JSON; false — не тот тип
	ref     RefKind                              // значение — id NekoBox (заменяется после переноса)
	dropMsg string                               // не пусто — ключ не переносится, причина в предупреждении
}

// settingRules — таблица соответствия. Порядок строк — порядок отчёта.
var settingRules = []settingRule{
	{neko: "inbound_address", lavray: "inbound.address", conv: asString},
	{neko: "inbound_socks_port", lavray: "inbound.port", conv: asInt},
	{neko: "inbound_auth.user", lavray: "inbound.auth.user", conv: asString},
	{neko: "inbound_auth.pass", lavray: "inbound.auth.pass", conv: asString},
	{neko: "custom_inbound", lavray: "inbound.custom_inbound", conv: asString},
	{neko: "test_url", lavray: "test.url", conv: asString},
	{neko: "test_url_dl", lavray: "test.url_dl", conv: asString},
	{neko: "test_dl_timeout", lavray: "test.dl_timeout", conv: asInt},
	{neko: "test_concurrent", lavray: "test.concurrent", conv: asInt},
	{neko: "custom_route", lavray: "routing.custom_global", conv: asString},
	{neko: "user_agent2", lavray: "sub.user_agent", conv: asString},
	{neko: "sub_use_proxy", lavray: "sub.use_proxy", conv: asBool},
	{neko: "sub_clear", lavray: "sub.clear", conv: asBool},
	{neko: "sub_insecure", lavray: "sub.insecure", conv: asBool},
	{neko: "sub_auto_update", lavray: "sub.auto_update", conv: asInt},
	{neko: "remember_id", lavray: "remember.id", conv: asInt, ref: RefProfile},
	{neko: "remember_enable", lavray: "remember.enable", conv: asBool},
	{neko: "spmode2", lavray: "remember.modes", conv: asStringList},
	{neko: "theme", lavray: "ui.theme", conv: oneOf("light", "dark", "system")},
	{neko: "language", lavray: "ui.lang", conv: oneOf("ru", "en", "de", "es", "zh")},
	{neko: "start_minimal", lavray: "ui.start_minimal", conv: asBool},
	{neko: "max_log_line", lavray: "ui.max_log_lines", conv: asInt},
	{neko: "log_ignore", lavray: "ui.log_ignore", conv: asStringList},
	{neko: "current_group", lavray: "ui.current_group", conv: asInt, ref: RefGroup},
	{neko: "fakedns", lavray: "tun.fake_dns", conv: asBool},
	{neko: "vpn_internal_tun", lavray: "tun.enabled", conv: asBool},
	{neko: "vpn_impl", lavray: "tun.impl", conv: asInt},
	{neko: "vpn_mtu", lavray: "tun.mtu", conv: asInt},
	{neko: "vpn_ipv6", lavray: "tun.ipv6", conv: asBool},
	{neko: "vpn_hide_console", lavray: "tun.hide_console", conv: asBool},
	{neko: "vpn_strict_route", lavray: "tun.strict_route", conv: asBool},
	{neko: "vpn_bypass_process", lavray: "tun.bypass_process", conv: asString},
	{neko: "vpn_bypass_cidr", lavray: "tun.bypass_cidr", conv: asString},
	{neko: "vpn_rule_white", lavray: "tun.whitelist", conv: asBool},
	{neko: "hk_mw", lavray: "hotkeys.main_window", conv: asString},
	{neko: "hk_group", lavray: "hotkeys.group", conv: asString},
	{neko: "hk_route", lavray: "hotkeys.routing", conv: asString},
	{neko: "hk_spmenu", lavray: "hotkeys.system_proxy_menu", conv: asString},
	{neko: "core_box_clash_api", lavray: "core.clash_api.enabled", conv: portToEnabled},
	{neko: "core_box_clash_api_secret", lavray: "core.clash_api.secret", conv: asString},
	{neko: "core_box_underlying_dns", lavray: "core.underlying_dns", conv: asString},
	{neko: "log_level", lavray: "log.level", conv: asString},
	{neko: "skip_cert", lavray: "security.allow_insecure_default", conv: asBool},
	{neko: "utlsFingerprint", lavray: "security.utls_fingerprint", conv: asString},
	{neko: "mux_protocol", lavray: "mux.protocol", conv: asString},
	{neko: "mux_concurrency", lavray: "mux.concurrency", conv: asInt},
	{neko: "mux_padding", lavray: "mux.padding", conv: asBool},
	{neko: "mux_default_on", lavray: "mux.default_on", conv: asBool},
	{neko: "sp_format", lavray: "win.sp_format", conv: asString},
	{neko: "traffic_loop_interval", lavray: "stats.interval_ms", conv: asInt},
	{neko: "old_share_link_format", lavray: "share.vmess_old_format", conv: asBool},
	// Не переносятся: у LAVRay своё значение или оно не имеет смысла.
	{neko: "conn_stat", dropMsg: "всегда включено в LAVRay"},
	{neko: "check_include_pre", dropMsg: "канал обновлений выбирается в LAVRay отдельно (состояние обновлений update.* не переносится)"},
	{neko: "mw_size", dropMsg: "размер окна в формате Qt"},
	{neko: "splitter_state", dropMsg: "положение разделителей в формате Qt"},
	{neko: "extraCore", dropMsg: "внешние ядра за отдельным флагом, пути не переносятся"},
	{neko: activeRoutingKey}, // имя маршрута: см. Plan.ActiveRoute
}

// activeRoutingKey — ключ имени активного маршрута NekoBox.
const activeRoutingKey = "active_routing"

// lookupKey ищет ключ верхнего уровня, а «a.b» — ещё и как поле b объекта a.
func lookupKey(top map[string]json.RawMessage, key string) (json.RawMessage, bool) {
	if v, ok := top[key]; ok {
		return v, true
	}
	head, tail, found := strings.Cut(key, ".")
	if !found {
		return nil, false
	}
	var nested map[string]json.RawMessage
	if obj, ok := top[head]; !ok || json.Unmarshal(obj, &nested) != nil {
		return nil, false
	}
	v, ok := nested[tail]
	return v, ok
}

// MapSettings применяет таблицу к DataStore: настройки LAVRay, имя активного
// маршрута и предупреждения (неизвестные ключи одной строкой, неверные типы).
func MapSettings(top map[string]json.RawMessage) (out []PlanSetting, activeRoute string, warns []string) {
	known := map[string]bool{}
	for _, r := range settingRules {
		known[strings.SplitN(r.neko, ".", 2)[0]] = true
		v, ok := lookupKey(top, r.neko)
		if !ok {
			continue
		}
		switch {
		case r.neko == activeRoutingKey:
			_ = json.Unmarshal(v, &activeRoute)
		case r.dropMsg != "":
			warns = append(warns, fmt.Sprintf("настройка %s не переносится: %s", r.neko, r.dropMsg))
		default:
			s, ok := r.conv(v)
			if !ok {
				warns = append(warns, fmt.Sprintf("настройка %s не перенесена: неожиданный тип значения", r.neko))
				continue
			}
			ps := PlanSetting{Key: r.lavray, JSON: s, Ref: r.ref}
			if r.ref != RefNone {
				_ = json.Unmarshal([]byte(s), &ps.RefID)
			}
			out = append(out, ps)
		}
	}
	var unknown []string
	for k := range top {
		if !known[k] {
			unknown = append(unknown, k)
		}
	}
	if len(unknown) > 0 {
		sort.Strings(unknown)
		warns = append(warns, unknownWarning(unknown))
	}
	return out, activeRoute, warns
}

// maxUnknownListed — сколько неизвестных ключей назвать в предупреждении.
const maxUnknownListed = 30

func unknownWarning(keys []string) string {
	shown := keys
	if len(shown) > maxUnknownListed {
		shown = shown[:maxUnknownListed]
	}
	msg := fmt.Sprintf("настроек NekoBox без соответствия в LAVRay: %d (%s", len(keys), strings.Join(shown, ", "))
	if len(keys) > len(shown) {
		msg += fmt.Sprintf(" и ещё %d", len(keys)-len(shown))
	}
	return msg + "); не перенесены"
}
