// SPDX-License-Identifier: GPL-3.0-only

package api

// agent_table.go — единая таблица «процедура → решение для агента». Каждая
// процедура proto обязана быть здесь (TestEveryProcedureHasAgentDecision), так
// что новый RPC нельзя выпустить, не решив, что с ним делает агентский токен.
// Из таблицы строятся белые списки agent.go. Нулевое значение — agentDeny, то
// есть всё, что не отмечено явно, агенту закрыто.

import "github.com/anatolijlaptev1991-ctrl/lavray-android/core/internal/gen/lavrayv1connect"

// agentDecision — что разрешено агенту для процедуры.
type agentDecision uint8

const (
	// agentDeny — агенту недоступно ни в одном режиме (нулевое значение).
	agentDeny agentDecision = iota
	// agentRead — доступно в режимах read и control.
	agentRead
	// agentControl — доступно только в режиме control.
	agentControl
)

// agentTable — решение по каждой процедуре proto. Экспорт ссылок и конфига,
// GetRuntimeConfig, настройки, DNS, правила, импорт, создание и удаление
// серверов и подписок, обновления, шлюз и веб-сессии агентам закрыты.
var agentTable = map[string]agentDecision{
	// CoreService.
	lavrayv1connect.CoreServiceConnectProcedure:          agentControl,
	lavrayv1connect.CoreServiceDisconnectProcedure:       agentControl,
	lavrayv1connect.CoreServiceRestartProfileProcedure:   agentDeny,
	lavrayv1connect.CoreServiceGetStatusProcedure:        agentRead,
	lavrayv1connect.CoreServiceSubscribeEventsProcedure:  agentDeny,
	lavrayv1connect.CoreServiceGetRuntimeConfigProcedure: agentDeny,
	lavrayv1connect.CoreServiceShutdownProcedure:         agentDeny,

	// ProfileService.
	lavrayv1connect.ProfileServiceListProfilesProcedure:              agentRead,
	lavrayv1connect.ProfileServiceGetProfileProcedure:                agentDeny,
	lavrayv1connect.ProfileServiceCreateProfileProcedure:             agentDeny,
	lavrayv1connect.ProfileServiceUpdateProfileProcedure:             agentDeny,
	lavrayv1connect.ProfileServiceDeleteProfileProcedure:             agentDeny,
	lavrayv1connect.ProfileServiceMoveProfilesProcedure:              agentDeny,
	lavrayv1connect.ProfileServiceCloneProfilesProcedure:             agentDeny,
	lavrayv1connect.ProfileServiceResetTrafficProcedure:              agentDeny,
	lavrayv1connect.ProfileServiceReorderProfilesProcedure:           agentDeny,
	lavrayv1connect.ProfileServiceRemoveUnavailableProfilesProcedure: agentDeny,
	lavrayv1connect.ProfileServiceDeduplicateProfilesProcedure:       agentDeny,
	lavrayv1connect.ProfileServiceSetProfileMetaProcedure:            agentDeny,
	lavrayv1connect.ProfileServicePreviewCustomFieldsProcedure:       agentDeny, // T-08B: подтверждает только человек
	lavrayv1connect.ProfileServiceConfirmCustomFieldsProcedure:       agentDeny,
	lavrayv1connect.ProfileServiceImportContentProcedure:             agentDeny,
	lavrayv1connect.ProfileServiceExportLinksProcedure:               agentDeny,
	lavrayv1connect.ProfileServiceExportConfigProcedure:              agentDeny,
	lavrayv1connect.ProfileServiceTestLatencyProcedure:               agentControl,
	lavrayv1connect.ProfileServiceStopTestsProcedure:                 agentControl,
	lavrayv1connect.ProfileServiceClearTestResultsProcedure:          agentDeny,
	lavrayv1connect.ProfileServiceResolveDomainsProcedure:            agentDeny,
	lavrayv1connect.ProfileServiceListGroupsProcedure:                agentRead,
	lavrayv1connect.ProfileServiceCreateGroupProcedure:               agentDeny,
	lavrayv1connect.ProfileServiceUpdateGroupProcedure:               agentDeny,
	lavrayv1connect.ProfileServiceDeleteGroupProcedure:               agentDeny,
	lavrayv1connect.ProfileServiceReorderGroupsProcedure:             agentDeny,

	// SubscriptionService.
	lavrayv1connect.SubscriptionServiceListSubscriptionsProcedure:          agentRead,
	lavrayv1connect.SubscriptionServiceCreateSubscriptionProcedure:         agentDeny,
	lavrayv1connect.SubscriptionServiceUpdateSubscriptionSettingsProcedure: agentDeny,
	lavrayv1connect.SubscriptionServiceDeleteSubscriptionProcedure:         agentDeny,
	lavrayv1connect.SubscriptionServiceUpdateSubscriptionProcedure:         agentControl,
	lavrayv1connect.SubscriptionServiceUpdateAllSubscriptionsProcedure:     agentControl,
	lavrayv1connect.SubscriptionServiceRollbackSubscriptionProcedure:       agentDeny,

	// RoutingService.
	lavrayv1connect.RoutingServiceListPresetsProcedure:        agentDeny,
	lavrayv1connect.RoutingServiceGetPresetProcedure:          agentDeny,
	lavrayv1connect.RoutingServiceCreatePresetProcedure:       agentDeny,
	lavrayv1connect.RoutingServiceUpdatePresetProcedure:       agentDeny,
	lavrayv1connect.RoutingServiceDeletePresetProcedure:       agentDeny,
	lavrayv1connect.RoutingServiceActivatePresetProcedure:     agentDeny,
	lavrayv1connect.RoutingServiceGetActivePresetProcedure:    agentDeny,
	lavrayv1connect.RoutingServiceResetBuiltinPresetProcedure: agentDeny,
	lavrayv1connect.RoutingServiceImportNekoRayRouteProcedure: agentDeny,
	lavrayv1connect.RoutingServiceExportPresetProcedure:       agentDeny, // T-80: наборы агенту закрыты целиком
	lavrayv1connect.RoutingServiceImportPresetProcedure:       agentDeny,

	// DnsService.
	lavrayv1connect.DnsServiceGetDnsProcedure:          agentDeny,
	lavrayv1connect.DnsServiceSetDnsProcedure:          agentDeny,
	lavrayv1connect.DnsServiceValidateDnsJsonProcedure: agentDeny,

	// SettingsService.
	lavrayv1connect.SettingsServiceDescribeSettingsProcedure:  agentDeny,
	lavrayv1connect.SettingsServiceGetSettingsProcedure:       agentDeny,
	lavrayv1connect.SettingsServiceSetSettingsProcedure:       agentDeny,
	lavrayv1connect.SettingsServiceResetSettingsProcedure:     agentDeny,
	lavrayv1connect.SettingsServiceSubscribeSettingsProcedure: agentDeny,

	// LogService.
	lavrayv1connect.LogServiceSubscribeProcedure: agentDeny,
	lavrayv1connect.LogServiceGetTailProcedure:   agentRead,

	// StatsService.
	lavrayv1connect.StatsServiceSubscribeProcedure:         agentRead,
	lavrayv1connect.StatsServiceGetTrafficTotalsProcedure:  agentRead,
	lavrayv1connect.StatsServiceGetTrafficSummaryProcedure: agentRead, // T-79: сводка по группам — числа без адресов

	// ConnectionService.
	lavrayv1connect.ConnectionServiceListConnectionsProcedure:  agentDeny,
	lavrayv1connect.ConnectionServiceWatchConnectionsProcedure: agentDeny,
	lavrayv1connect.ConnectionServiceCloseConnectionProcedure:  agentDeny,

	// DiagnosticsService.
	lavrayv1connect.DiagnosticsServiceCheckRouteProcedure:   agentDeny,
	lavrayv1connect.DiagnosticsServiceRunSelfCheckProcedure: agentDeny,
	lavrayv1connect.DiagnosticsServiceGetPublicIpProcedure:  agentRead,
	lavrayv1connect.DiagnosticsServiceCheckLeaksProcedure:   agentDeny,

	// GatewayService.
	lavrayv1connect.GatewayServiceGetGatewayStatusProcedure:   agentDeny,
	lavrayv1connect.GatewayServiceListDevicePoliciesProcedure: agentDeny,
	lavrayv1connect.GatewayServiceSetDevicePolicyProcedure:    agentDeny,

	// UpdateService.
	lavrayv1connect.UpdateServiceCheckUpdatesProcedure:      agentDeny,
	lavrayv1connect.UpdateServiceDownloadUpdatesProcedure:   agentDeny,
	lavrayv1connect.UpdateServiceApplyUpdatesProcedure:      agentDeny,
	lavrayv1connect.UpdateServiceRollbackProcedure:          agentDeny,
	lavrayv1connect.UpdateServiceGetUpdateStateProcedure:    agentRead,
	lavrayv1connect.UpdateServiceVerifyLocalUpdateProcedure: agentDeny,

	// WebSessionService.
	lavrayv1connect.WebSessionServiceGetWebModeStatusProcedure: agentDeny,
	lavrayv1connect.WebSessionServiceSetupFirstAdminProcedure:  agentDeny,
	lavrayv1connect.WebSessionServiceLoginProcedure:            agentDeny,
	lavrayv1connect.WebSessionServiceLogoutProcedure:           agentDeny,
	lavrayv1connect.WebSessionServiceRenewSessionProcedure:     agentDeny,
	lavrayv1connect.WebSessionServiceWhoAmIProcedure:           agentDeny,
	lavrayv1connect.WebSessionServiceChangePasswordProcedure:   agentDeny,
	lavrayv1connect.WebSessionServiceListSessionsProcedure:     agentDeny,
	lavrayv1connect.WebSessionServiceRevokeSessionProcedure:    agentDeny,

	// BackupService (T-83): копия содержит секреты и заменяет все данные —
	// агентам закрыты все три процедуры.
	lavrayv1connect.BackupServiceExportBackupProcedure:  agentDeny,
	lavrayv1connect.BackupServicePreviewBackupProcedure: agentDeny,
	lavrayv1connect.BackupServiceImportBackupProcedure:  agentDeny,

	// MigrationService (T-78): читает произвольные каталоги диска и пишет в базу —
	// агентам закрыт весь сервис.
	lavrayv1connect.MigrationServiceDetectNekoBoxProcedure:        agentDeny,
	lavrayv1connect.MigrationServicePreviewNekoBoxImportProcedure: agentDeny,
	lavrayv1connect.MigrationServiceImportNekoBoxProcedure:        agentDeny,
}

// agentProcedures — множество процедур с решением d.
func agentProcedures(d agentDecision) map[string]bool {
	set := make(map[string]bool)
	for procedure, decision := range agentTable {
		if decision == d {
			set[procedure] = true
		}
	}
	return set
}
