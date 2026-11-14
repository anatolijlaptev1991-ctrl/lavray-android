// SPDX-License-Identifier: GPL-3.0-only

package api

import (
	"context"
	"net"
	"net/http"
	"net/netip"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"connectrpc.com/connect"

	"github.com/anatolijlaptev1991-ctrl/lavray-android/core/internal/cfgbuild"
	"github.com/anatolijlaptev1991-ctrl/lavray-android/core/internal/gen/lavrayv1"
	lavrayv1connect "github.com/anatolijlaptev1991-ctrl/lavray-android/core/internal/gen/lavrayv1connect"
	"github.com/anatolijlaptev1991-ctrl/lavray-android/core/internal/inst"
	"github.com/anatolijlaptev1991-ctrl/lavray-android/core/internal/logs"
	"github.com/anatolijlaptev1991-ctrl/lavray-android/core/internal/presets"
	"github.com/anatolijlaptev1991-ctrl/lavray-android/core/internal/store"
	"github.com/anatolijlaptev1991-ctrl/lavray-android/core/internal/subs"
	"github.com/anatolijlaptev1991-ctrl/lavray-android/core/internal/version"
)

// Deps — зависимости сервера (все внедряются: cmd/lavray-core в W8, тесты —
// httptest + FakeRunner).
type Deps struct {
	Store  *store.Store // обязателен
	Logs   *logs.Mux    // nil → свой на 1000 строк
	Runner inst.Runner  // nil → inst.NopRunner (сборка без singbox_run)
	HTTP   *http.Client // клиент подписок (туннель sub_use_proxy соберёт cmd; nil → дефолт)
	// TunCheck — проверка прав на TUN перед стартом в режиме TUN: ошибка
	// возвращается клиенту как есть (cmd подставляет проверку повышения на
	// Windows; nil — не проверять).
	TunCheck func() error
	// Updater — обновления сборки (корень доверия, источники); nil — не настроены.
	Updater *UpdaterConf
}

// Server — реализация сервисов W6. Один экземпляр на процесс; горутинно-
// безопасен (хранилище сериализует транзакции, менеджер — FSM).
type Server struct {
	st   *store.Store
	logs *logs.Mux
	inst *inst.Manager
	http *http.Client

	// ipURL — сервис публичного IP (GetPublicIp/CheckLeaks); dial — набор
	// номера для зондов (IPv6, порт); тесты подменяют оба.
	ipURL      string
	ipProbeURL string // адрес-литерал для проверки «трафик идёт через туннель» (без DNS)
	dial       func(ctx context.Context, network, addr string) (net.Conn, error)

	// OnShutdown вызывается после остановки инстанса при CoreService.Shutdown
	// (W8 headless подставит выход из процесса; здесь — только остановка).
	OnShutdown func(ctx context.Context)

	// upd — подсистема обновлений (updater_wiring.go).
	upd updaterState
	// acct — учёт трафика сессии (traffic_accountant.go, T-79).
	acct trafficAccountant
	// presetRepo — пользовательские наборы маршрутов (preset_repo.go, T-80).
	presetRepo *presetRepo

	// ---- подписки: прогресс-мультиплексирование по group_id ----
	subsMu      sync.Mutex
	subsRunning map[int64]bool           // бегущие UpdateSubscription → ALREADY_EXISTS
	subsSnap    map[int64]*subs.Snapshot // последний успешный снапшот (Rollback)
	hwidMu      sync.Mutex               // создание постоянного HWID установки (D-02)

	// ---- SettingsService: поток изменений ----
	setMu   sync.Mutex
	setSubs map[chan *lavrayv1.SettingsChanged]struct{}

	// ---- тесты задержки: один батч на процесс (StopTests) ----
	testsMu      sync.Mutex
	testsRunning *testRun
	// newTestInst — фабрика временных инстансов URL- и полного теста (T-84);
	// nil → inst.NewTestInstance. lookupIP — разбор имени сервера для «входного
	// IP»; nil → системный резолвер. Тесты подменяют оба.
	newTestInst inst.TestInstanceFactory
	lookupIP    func(ctx context.Context, host string) (netip.Addr, error)
	// resolve — резолвер и пределы ResolveDomains (resolver.go); нулевой — системный.
	resolve resolveConfig

	backup *backupService // зашифрованная копия (backup_service.go)
	// migration — перенос из NekoBox (migration_service.go).
	migration *migrationService
	sched     schedCtl // жизненный цикл планировщика подписок (sched_ctl.go)

	// ---- агентский доступ (MCP): токен этого запуска, режим — agent.mode ----
	agentMu    sync.RWMutex
	agentToken string

	tunCheck func() error     // проверка прав на TUN перед стартом (nil — не проверять)
	ext      serverExt        // веб-режим и шлюз (server_ext.go)
	auto     autoConnectState // счётчик ручных действий для --connect-last (connect_last.go)

	// health — итог проверки связи после подключения (connect_health.go).
	health connHealth
	// healthRetryDelay — пауза перед повтором быстро упавшей проверки,
	// healthTimeout — время ожидания ответа; тесты подменяют оба.
	healthRetryDelay time.Duration
	healthTimeout    time.Duration
	// onSettingRead — счётчик чтений настроек (тесты бюджета SQL); nil в работе.
	onSettingRead func(key string)
}

// NewServer создаёт сервер зависимостями (Deps.Store обязателен).
func NewServer(d Deps) *Server {
	if d.Store == nil {
		panic("api: Deps.Store обязателен")
	}
	lg := d.Logs
	if lg == nil {
		lg = logs.NewMux(1000)
	}
	httpc := d.HTTP
	if httpc == nil {
		httpc = &http.Client{Timeout: subs.DefaultOverallTimeout}
	}
	// T-33: журнал sing-box попадает в журнал ядра (GetTail/Subscribe);
	// секреты вычищает Mux.Logf (Redact) до буфера.
	if ls, ok := d.Runner.(inst.LogSinker); ok {
		ls.SetLogSink(func(level, message string) {
			lg.Logf(level, logs.SourceSingBox, "%s", message)
		})
	}
	s := &Server{
		st:               d.Store,
		presetRepo:       newPresetRepo(d.Store),
		logs:             lg,
		inst:             inst.NewManager(d.Runner),
		http:             httpc,
		ipURL:            publicIPURL,
		ipProbeURL:       "https://1.1.1.1/",
		dial:             (&net.Dialer{}).DialContext,
		upd:              newUpdaterState(d.Updater),
		tunCheck:         d.TunCheck,
		healthRetryDelay: healthRetryDelay,
		healthTimeout:    healthCheckTimeout,
		subsRunning:      map[int64]bool{},
		subsSnap:         map[int64]*subs.Snapshot{},
		setSubs:          map[chan *lavrayv1.SettingsChanged]struct{}{},
	}
	s.backup = newBackupService(s)
	s.migration = newMigrationService(s)
	return s
}

// HandlerOptions — опции всех хендлеров (интерсептор recover+map).
func (s *Server) HandlerOptions() []connect.HandlerOption {
	return []connect.HandlerOption{
		connect.WithInterceptors(newInterceptor(s.logs, s.AgentMode)),
		connect.WithReadMaxBytes(MaxRequestBytes), // T-50: лимит тела запроса
	}
}

// Register монтирует все 13 сервисов на mux. WebSession и Gateway берутся из
// server_ext.go: без включения обвязкой они UNIMPLEMENTED и INVALID_ARGUMENT.
func (s *Server) Register(mux *http.ServeMux) {
	opts := s.HandlerOptions()
	mount := func(path string, h http.Handler) {
		mux.Handle(path, corsWrap(h))
	}
	path, h := lavrayv1connect.NewCoreServiceHandler(s, opts...)
	mount(path, h)
	path, h = lavrayv1connect.NewProfileServiceHandler(s, opts...)
	mount(path, h)
	path, h = lavrayv1connect.NewSubscriptionServiceHandler(s, opts...)
	mount(path, h)
	path, h = lavrayv1connect.NewRoutingServiceHandler(s, opts...)
	mount(path, h)
	path, h = lavrayv1connect.NewDnsServiceHandler(s, opts...)
	mount(path, h)
	path, h = lavrayv1connect.NewSettingsServiceHandler(s, opts...)
	mount(path, h)
	path, h = lavrayv1connect.NewLogServiceHandler(s, opts...)
	mount(path, h)
	path, h = lavrayv1connect.NewStatsServiceHandler(statsSvc{s}, opts...)
	mount(path, h)
	path, h = lavrayv1connect.NewConnectionServiceHandler(s, opts...)
	mount(path, h)
	path, h = lavrayv1connect.NewDiagnosticsServiceHandler(s, opts...)
	mount(path, h)
	path, h = lavrayv1connect.NewGatewayServiceHandler(s.gatewayHandler(), opts...)
	mount(path, h)
	path, h = lavrayv1connect.NewUpdateServiceHandler(s, opts...)
	mount(path, h)
	path, h = lavrayv1connect.NewWebSessionServiceHandler(s.webSessionHandler(), opts...)
	mount(path, h)
	path, h = lavrayv1connect.NewBackupServiceHandler(s.backup, s.backup.handlerOptions(opts)...)
	mount(path, h)
	path, h = lavrayv1connect.NewMigrationServiceHandler(s.migration, opts...)
	mount(path, h)
}

// NewHandler — готовый http.Handler со всеми сервисами (хелпер для cmd/тестов).
func (s *Server) NewHandler() http.Handler {
	mux := http.NewServeMux()
	s.Register(mux)
	return mux
}

// Close — очистка при завершении процесса (подписки стримов закрывает ctx).
func (s *Server) Close() error { return nil }

// ============================================================================
// Сборка cfgbuild.BuildSettings из карты настроек
// ============================================================================

func (s *Server) buildSettings(tunMode bool) *cfgbuild.BuildSettings {
	st := cfgbuild.DefaultSettings()
	st.Inbound = cfgbuild.InboundSettings{
		Address:           s.settingStr("inbound.address"),
		Port:              s.settingInt("inbound.port"),
		AuthUser:          s.settingStr("inbound.auth.user"),
		AuthPass:          s.settingStr("inbound.auth.pass"),
		CustomInboundJSON: s.settingStr("inbound.custom_inbound"),
	}
	// L21 аудита №1: TUN включается ТОЛЬКО режимом подключения. Настройка
	// tun.enabled=true в режиме SYSTEM_PROXY раньше глушила mixed-слушатель
	// (inbounds=[tun-in]) — системный прокси указывал на 127.0.0.1:2080, где
	// никто не слушал. Настройка теперь лишь разрешает TUN как режим.
	st.Tun = cfgbuild.TunSettings{
		Enabled:       tunMode,
		InterfaceName: s.settingStr("tun.interface_name"),
		MTU:           s.settingInt("tun.mtu"),
		Stack:         tunStackOf(s.settingInt("tun.impl")),
		StrictRoute:   s.settingBool("tun.strict_route"),
		IPv6:          s.settingBool("tun.ipv6"),
		FakeDNS:       s.settingBool("tun.fake_dns"),
		BypassProcess: s.settingStr("tun.bypass_process"),
		BypassCIDR:    s.settingStr("tun.bypass_cidr"),
		Whitelist:     s.settingBool("tun.whitelist"),
		KillSwitch:    s.settingBool("tun.kill_switch"),
	}
	st.Mux = cfgbuild.MuxSettings{
		Protocol:    s.settingStr("mux.protocol"),
		Concurrency: s.settingInt("mux.concurrency"),
		Padding:     s.settingBool("mux.padding"),
		DefaultOn:   s.settingBool("mux.default_on"),
	}
	st.AllowInsecureDefault = s.settingBool("security.allow_insecure_default")
	st.LogLevel = s.settingStr("log.level") // T-49: пусто/неизвестно → "info" в cfgbuild
	if v := s.settingStr("core.underlying_dns"); v != "" {
		st.UnderlyingDNS = v
	}
	st.CustomRouteGlobalJSON = s.settingStr("routing.custom_global")
	st.GeoDir = filepath.ToSlash(filepath.Join(s.st.Dir(), "geo"))
	st.CacheFilePath = filepath.ToSlash(filepath.Join(s.st.Dir(), "cache.db"))
	if ov, ok := s.activeDNSChoice(); ok {
		st.DNSRemoteDirect = ov.RemoteDirect
	}
	return st
}

// tunStackOf — vpn_impl некора: 1 gvisor (default) | 2 system | 3 mixed.
func tunStackOf(impl int) string {
	switch impl {
	case 2:
		return "system"
	case 3:
		return "mixed"
	}
	return "gvisor"
}

// validateInboundLAN — FR-UI-279/LNB-33: inbound-адрес не loopback при пустой
// авторизации → INVALID_ARGUMENT («пароль обязателен при доступе из LAN»).
// В TUN-режиме mixed-слушатель остаётся (cfgbuild/inbounds.go), но не-loopback
// адрес без авторизации там отвергает уже сборка конфига — повторная проверка
// не нужна (T-55: прежний текст «в TUN mixed нет» был неверен).
func (s *Server) validateInboundLAN(inbound cfgbuild.InboundSettings) error {
	addr := strings.TrimSpace(inbound.Address)
	if addr == "" || addr == "localhost" {
		return nil
	}
	if ip := net.ParseIP(addr); ip != nil && ip.IsLoopback() {
		return nil
	}
	if inbound.AuthUser == "" || inbound.AuthPass == "" {
		return invalidArg("inbound.address: адрес %s не loopback — авторизация (inbound.auth.user/pass) обязательна при доступе из LAN (FR-UI-279)", addr)
	}
	return nil
}

// ============================================================================
// Пресеты: builtin (presets.BuiltinPresets) + пользовательские (meta)
// ============================================================================

const (
	metaDNSOverride     = "routing.dns_override" // JSON map[id пресета]dnsChoice (dns_choice.go)
	settingActivePreset = "routing.active_preset"
)

// activePresetID — id активного набора. Значение по умолчанию задаёт дескриптор
// routing.active_preset; global здесь — только запасной вариант для пустого значения.
func (s *Server) activePresetID() string {
	if v := s.settingStr(settingActivePreset); v != "" {
		return v
	}
	return "global"
}

// activePreset — активный набор (builtin, пользовательский или нейтральный).
func (s *Server) activePreset() (*presets.Preset, error) {
	p, err := s.activeOrGlobal()
	if err != nil {
		return nil, err
	}
	return s.applyDNSOverride(p), nil
}

// ============================================================================
// CoreStatus
// ============================================================================

// coreStatus — снимок CoreStatus (менеджер + настройки inbound/tun/пресет).
func (s *Server) coreStatus() *lavrayv1.CoreStatus {
	st := s.inst.Status()
	cs := &lavrayv1.CoreStatus{
		State:           coreStateOf(st.State),
		CoreVersion:     version.Version,
		Mode:            connectModeOf(""),
		RoutingPresetId: strpIf(s.activePresetID()),
	}
	if st.LastError != "" {
		cs.LastError = strp(st.LastError)
	}
	if st.Run != nil {
		if st.Run.ProfileID != 0 {
			cs.ActiveProfileId = int64p(st.Run.ProfileID)
		}
		if st.Run.GroupID != 0 {
			cs.ActiveGroupId = int64p(st.Run.GroupID)
		}
		if st.Run.ProfileName != "" {
			cs.ActiveProfileName = strp(st.Run.ProfileName)
		}
		cs.Mode = connectModeOf(st.Run.Mode)
		if st.Run.StartedAtMs != 0 {
			cs.StartedAtMs = int64p(st.Run.StartedAtMs)
		}
	}
	if st.State == inst.StateReady {
		cs.HealthError = strpIf(s.healthFailure())
	}
	cs.Inbound, cs.Tun = s.statusInboundTun()
	return cs
}
