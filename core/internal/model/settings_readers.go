// SPDX-License-Identifier: GPL-3.0-only

package model

// settings_readers.go — кто читает каждую настройку каталога (хвосты A).
// Ключ без читателя — «настройка-обманка»: пользователь её меняет, ничего не
// происходит. TestEverySettingHasReader требует запись для каждого ключа и
// проверяет, что строка ключа есть в файле читателя; новый ключ нельзя
// добавить в SettingDescriptors, не указав, кто его читает.

// ReaderKind — вид читателя настройки.
type ReaderKind string

const (
	ReaderCore       ReaderKind = "core"  // ядро (Go)
	ReaderShell      ReaderKind = "shell" // оболочка (Rust)
	ReaderUI         ReaderKind = "ui"    // интерфейс (TypeScript)
	ReaderImportOnly ReaderKind = "import-only"
)

// SettingReader — читатель одной настройки.
type SettingReader struct {
	Kind ReaderKind
	// File — файл читателя от корня репозитория, в нём есть строка ключа.
	// У import-only пуст.
	File string
	// Reason — у import-only: почему ключ хранится, но рабочий код его не читает.
	Reason string
}

func apiReader(file string) SettingReader {
	return SettingReader{Kind: ReaderCore, File: "core/internal/api/" + file}
}

func uiReader(file string) SettingReader {
	return SettingReader{Kind: ReaderUI, File: "ui/src/" + file}
}

func importOnly(reason string) SettingReader {
	return SettingReader{Kind: ReaderImportOnly, Reason: reason}
}

// SettingReaders — таблица «ключ → читатель».
func SettingReaders() map[string]SettingReader {
	const hotkeys = "сочетание «подключить / отключить» хранится в ui-state.json (lavray.hotkeyConnect); ключи NekoBox не читаются"
	const clashAPI = "clash_api в конфиге запрещён (core-port-plan §6.3, hardening), настройка не используется"
	const uiState = "хранится и читается интерфейсом под своим именем (localStorage и ui-state.json, ключи lavray.*), ключ ядра не используется"
	return map[string]SettingReader{
		// Inbound — сборка конфига.
		"inbound.address":        apiReader("server.go"),
		"inbound.port":           apiReader("server.go"),
		"inbound.auth.user":      apiReader("server.go"),
		"inbound.auth.pass":      apiReader("server.go"),
		"inbound.custom_inbound": apiReader("server.go"),

		// Тесты задержки (T-84).
		"test.url":        apiReader("test_instance.go"),
		"test.url_dl":     apiReader("test_instance.go"),
		"test.dl_timeout": apiReader("test_instance.go"),
		"test.concurrent": apiReader("test_instance.go"),
		"test.url_ip":     apiReader("test_instance.go"),
		"test.udp_target": apiReader("test_instance.go"),

		// Маршрутизация.
		"routing.active_preset": apiReader("server.go"),
		"routing.custom_global": apiReader("server.go"),

		// Подписки.
		"sub.user_agent":   apiReader("subscription_service.go"),
		"sub.use_proxy":    apiReader("tunnel_http.go"),
		"sub.clear":        apiReader("subscription_service.go"),
		"sub.insecure":     apiReader("subscription_service.go"),
		"sub.auto_update":  apiReader("subscription_auto.go"),
		"sub.hwid_enabled": apiReader("subscription_service.go"),
		SettingHWIDID:      apiReader("subscription_hwid.go"),

		// Запоминание последнего профиля (remember.go, headless_glue.go).
		"remember.id":     apiReader("headless_glue.go"),
		"remember.enable": apiReader("headless_glue.go"),
		"remember.modes":  apiReader("remember.go"),

		// UI.
		"ui.theme":         importOnly(uiState),
		"ui.lang":          importOnly(uiState),
		"ui.window_size":   importOnly("размер окна хранит плагин window-state оболочки, не карта настроек"),
		"ui.splitter":      importOnly(uiState),
		"ui.start_minimal": importOnly("оболочка читает собственный lavray.startMinimized из ui-state.json (shell/src/prefs.rs)"),
		"ui.max_log_lines": uiReader("screens/log/useLogBuffer.ts"),
		"ui.conn_stat":     importOnly("счётчики трафика показываются всегда (FR-UI-274), выключателя нет"),
		"ui.log_ignore":    importOnly("фильтр строк журнала не реализован, ключ перенесён из NekoBox"),
		"ui.current_group": importOnly(uiState),

		// TUN.
		"tun.fake_dns":       apiReader("server.go"),
		"tun.enabled":        importOnly("TUN включается режимом Connect, а не настройкой (L21 аудита №1)"),
		"tun.impl":           apiReader("server.go"),
		"tun.mtu":            apiReader("server.go"),
		"tun.ipv6":           apiReader("server.go"),
		"tun.hide_console":   importOnly("оболочка запускает ядро без окна консоли всегда (CREATE_NO_WINDOW)"),
		"tun.strict_route":   apiReader("server.go"),
		"tun.bypass_process": apiReader("server.go"),
		"tun.bypass_cidr":    apiReader("server.go"),
		"tun.whitelist":      apiReader("server.go"),
		"tun.kill_switch":    apiReader("server.go"),
		"tun.interface_name": apiReader("server.go"),

		// Горячие клавиши.
		"hotkeys.main_window":       importOnly(hotkeys),
		"hotkeys.group":             importOnly(hotkeys),
		"hotkeys.routing":           importOnly(hotkeys),
		"hotkeys.system_proxy_menu": importOnly(hotkeys),

		// Ядро.
		"core.clash_api.enabled": importOnly(clashAPI),
		"core.clash_api.secret":  importOnly(clashAPI),
		"core.underlying_dns":    apiReader("server.go"),
		"log.level":              apiReader("server.go"),
		"connect.health_check":   apiReader("connect_health.go"),
		// Оболочка читает зеркало ключа lavray.killSwitchProxy из ui-state.json
		// (интерфейс копирует в него настройку ядра), см. shell/src/killswitch.rs.
		"connect.kill_switch_proxy": {Kind: ReaderShell, File: "shell/src/killswitch.rs"},

		// Безопасность.
		"security.allow_insecure_default": apiReader("server.go"),
		"security.utls_fingerprint":       importOnly("отпечаток uTLS берётся из параметров транспорта профиля (Stream.UTLSFingerprint), глобальный ключ не читается"),

		// Mux.
		"mux.protocol":    apiReader("server.go"),
		"mux.concurrency": apiReader("server.go"),
		"mux.padding":     apiReader("server.go"),
		"mux.default_on":  apiReader("server.go"),

		// Агенты (MCP).
		"agent.mode": apiReader("agent.go"),

		// Прочее.
		"win.sp_format":     importOnly("ProxyServer системного прокси всегда host:port; передача формата через UI, set_system_proxy и Backend::apply (в том числе RAS) — отдельная работа вместе с выбором формата в «Настройках» (хвосты B)"),
		"update.channel":    uiReader("hooks/useUpdateSettings.ts"),
		"update.auto_check": uiReader("hooks/useUpdateSettings.ts"),
		// служебные ключи обновлений (T-75): SetSettings их не принимает, читает и пишет api
		SettingUpdateRootVersion:      apiReader("update_store.go"),
		SettingUpdateTimestampVersion: apiReader("update_store.go"),
		SettingUpdateSerialStable:     apiReader("update_store.go"),
		SettingUpdateSerialBeta:       apiReader("update_store.go"),
		SettingUpdateLastChecked:      apiReader("update_store.go"),
		SettingUpdateLastURL:          apiReader("update_store.go"),
		SettingUpdateRootJSON:         importOnly("устарел: принятые корни доверия лежат файлами updates/roots и перепроверяются от вшитого корня; ключ остаётся служебным, чтобы SetSettings его не принимал"),
		"stats.interval_ms":           apiReader("stats_interval.go"),
		"share.vmess_old_format":      apiReader("share_options.go"),
		"enable_external_cores":       apiReader("core_service.go"),
		"web.bind":                    importOnly("зарезервирован для веб-панели (Ф7, T-76): адрес пока задают флаги запуска"),
		"web.tls":                     importOnly("зарезервирован для веб-панели (Ф7, T-76): TLS пока задают флаги запуска"),
	}
}
