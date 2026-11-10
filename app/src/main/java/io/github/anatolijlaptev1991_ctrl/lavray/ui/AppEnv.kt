// SPDX-License-Identifier: GPL-3.0-only
package io.github.anatolijlaptev1991_ctrl.lavray.ui

import android.content.Context
import androidx.compose.runtime.Stable
import androidx.compose.runtime.compositionLocalOf
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.setValue
import io.github.anatolijlaptev1991_ctrl.lavray.R
import io.github.anatolijlaptev1991_ctrl.lavray.ui.core.CoreSession
import io.github.anatolijlaptev1991_ctrl.lavray.ui.core.EndpointSource
import io.github.anatolijlaptev1991_ctrl.lavray.ui.design.components.ToastState
import io.github.anatolijlaptev1991_ctrl.lavray.ui.importer.ImportModel
import io.github.anatolijlaptev1991_ctrl.lavray.ui.prefs.UiPrefs
import io.github.anatolijlaptev1991_ctrl.lavray.ui.prefs.UiSettings
import io.github.anatolijlaptev1991_ctrl.lavray.ui.screens.connection.ConnectionModel
import io.github.anatolijlaptev1991_ctrl.lavray.ui.screens.connection.CoreProbeBackend
import io.github.anatolijlaptev1991_ctrl.lavray.ui.screens.connection.ExitProbe
import io.github.anatolijlaptev1991_ctrl.lavray.ui.screens.backup.BackupModel
import io.github.anatolijlaptev1991_ctrl.lavray.ui.screens.backup.ResolverBackupFiles
import io.github.anatolijlaptev1991_ctrl.lavray.ui.screens.connection.Phase
import io.github.anatolijlaptev1991_ctrl.lavray.ui.screens.connection.phaseOf
import io.github.anatolijlaptev1991_ctrl.lavray.ui.screens.rules.PendingReason
import io.github.anatolijlaptev1991_ctrl.lavray.ui.screens.rules.RestartHub
import io.github.anatolijlaptev1991_ctrl.lavray.ui.screens.settings.AppSelectionSaver
import io.github.anatolijlaptev1991_ctrl.lavray.ui.screens.settings.canonical
import io.github.anatolijlaptev1991_ctrl.lavray.ui.screens.settings.Reconnect
import io.github.anatolijlaptev1991_ctrl.lavray.ui.screens.servers.ServersModel
import io.github.anatolijlaptev1991_ctrl.lavray.ui.screens.subscriptions.SubFailureStore
import io.github.anatolijlaptev1991_ctrl.lavray.ui.screens.subscriptions.SubscriptionsModel
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.SupervisorJob
import kotlinx.coroutines.cancel
import kotlinx.coroutines.flow.SharingStarted
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.map
import kotlinx.coroutines.flow.stateIn

/**
 * The one object screens reach for: the VPN controller, the core session, the settings and the models that
 * outlive a single screen (servers list, import). Created by the activity, cancelled with it. Models are
 * built on first use, so a screen that is never opened costs nothing. Other agents' screens add their own
 * model the same way: a `by lazy` property here, or a model of their own created inside the screen.
 */
@Stable
class AppEnv(
    val appContext: Context,
    val controller: VpnController,
    private val prefs: UiPrefs,
) {
    val scope = CoroutineScope(SupervisorJob() + Dispatchers.Default)
    val session = CoreSession(EndpointSource { controller.endpoint() })
    val toast = ToastState()
    val vpn = VpnActions(controller, session, scope) { id -> update { it.copy(selectedProfileId = id) } }

    var settings: UiSettings by mutableStateOf(prefs.load())
        private set

    val connection: ConnectionModel by lazy { ConnectionModel(session, scope) }
    val servers: ServersModel by lazy { ServersModel(session, scope, vpn::connect) }
    /** Raised when servers or subscriptions change behind a screen's back (import); screens that list them reload on it. */
    val data = DataChanges()
    val importer: ImportModel by lazy { ImportModel(session, scope, subscriptions, ::serversChanged) }

    private fun serversChanged() {
        servers.refresh()
        data.notifyChanged()
    }

    /** True while a tunnel is up or on its way up; the pending-changes state ends with it. */
    private val tunnelLive: StateFlow<Boolean> by lazy {
        vpn.status.map { phaseOf(it.state).let { p -> p == Phase.On || p == Phase.Connecting || p == Phase.WaitingNetwork } }
            .stateIn(scope, SharingStarted.Eagerly, false)
    }
    val restart: RestartHub by lazy { RestartHub(session, scope, tunnelLive) { Reconnect.now(this) } }
    val appSelection: AppSelectionSaver by lazy {
        AppSelectionSaver(
            read = { runCatching { controller.appSelection() }.getOrNull() },
            write = { s -> runCatching { controller.setAppSelection(s) }.isSuccess },
            // Recorded against what the running tunnel was built with: ticking an app and unticking it again is no change.
            onSaved = { before, after -> restart.noteTunnelChange(PendingReason.Apps, "apps", before?.canonical(), after.canonical()) },
        )
    }
    val exit: ExitProbe by lazy { ExitProbe(CoreProbeBackend(session)) }
    val subscriptions: SubscriptionsModel by lazy { SubscriptionsModel(session, scope, SubFailureStore(appContext), ::serversChanged) }

    /**
     * Owned by the app, not by its screen, so a restore that is running outlives the screen and its result is kept.
     * After a restore the server ids are different: the remembered choice would point at someone else's server.
     */
    val backup: BackupModel by lazy {
        BackupModel(
            session, ResolverBackupFiles(appContext.contentResolver), scope,
            // Before the request, not after it: if the app dies during or right after the restore, the old id must not survive.
            onBeforeReplace = { update { it.copy(selectedProfileId = 0L) } },
            onRestored = {
                servers.refresh()
                connection.refresh(0L)
                // The result is also told here: the person may be on another screen by now.
                toast.show(appContext.getString(R.string.bk_done))
            },
        )
    }

    /** Replaces the settings and saves them; the UI recomposes from the new immutable value. May be called from any thread. */
    @Synchronized
    fun update(change: (UiSettings) -> UiSettings) {
        settings = change(settings).also(prefs::save)
    }

    fun close() = scope.cancel()
}

val LocalEnv = compositionLocalOf<AppEnv> { error("AppEnv is provided by MainActivity") }
