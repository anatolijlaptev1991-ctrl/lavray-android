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
import io.github.anatolijlaptev1991_ctrl.lavray.ui.screens.backup.ServiceHold
import io.github.anatolijlaptev1991_ctrl.lavray.ui.screens.connection.TunnelMemory
import io.github.anatolijlaptev1991_ctrl.lavray.ui.screens.connection.buildsIn
import io.github.anatolijlaptev1991_ctrl.lavray.ui.screens.connection.liveIn
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
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.distinctUntilChanged
import kotlinx.coroutines.flow.map
import kotlinx.coroutines.launch

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

    init {
        // The service cut the Go core loose and started it anew: the endpoint (and its token) is not the one the client holds.
        scope.launch { controller.status.map { it.coreEpoch }.distinctUntilChanged().collect(session::onCoreEpoch) }
    }

    val connection: ConnectionModel by lazy { ConnectionModel(session, scope, vpn.status, memory = TunnelMemory.process) }
    val servers: ServersModel by lazy { ServersModel(session, scope) }
    /** Raised when servers or subscriptions change behind a screen's back (import); screens that list them reload on it. */
    val data = DataChanges()
    val importer: ImportModel by lazy { ImportModel(session, scope, subscriptions, ::serversChanged) }

    private fun serversChanged() {
        servers.refresh()
        data.notifyChanged()
    }

    /** True while a tunnel is up or on its way up; the pending-changes state ends with it. */
    private val tunnelLive: StateFlow<Boolean> by lazy { vpn.status.liveIn(scope) }

    /** The builds of the tunnel ([TunnelSight.builds]): a rebuild or a server switch never leaves "live" but is a new build all the same. */
    private val tunnelsBuilt: StateFlow<Int> by lazy { vpn.status.buildsIn(scope) }
    val restart: RestartHub by lazy { RestartHub(session, scope, tunnelLive, reconnect = { Reconnect.now(this) }, built = tunnelsBuilt) }
    val appSelection: AppSelectionSaver by lazy {
        AppSelectionSaver(
            read = { runCatching { controller.appSelection() }.getOrNull() },
            write = { s -> runCatching { controller.setAppSelection(s) }.isSuccess },
            // Recorded against what the running tunnel was built with: ticking an app and unticking it again is no change.
            onSaved = { before, after -> restart.noteTunnelChange(PendingReason.Apps, "apps", before?.canonical(), after.canonical()) },
        )
    }
    // The core's latency test is single-flight: the exit probe leaves its delay measurement out while "test all" runs.
    val exit: ExitProbe by lazy { ExitProbe(CoreProbeBackend(session), canMeasureLatency = { !servers.state.value.test.running }) }
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
                // One signal for "the data changed": the servers list and the Connection screen both reload on it.
                serversChanged()
                // The result is also told here: the person may be on another screen by now.
                toast.show(appContext.getString(R.string.bk_done))
            },
            hold = object : ServiceHold {
                // False means the service could not keep itself alive: the same as a failed call, the restore must not run.
                override suspend fun begin(token: String, timeoutMs: Long) =
                    check(controller.beginBusy(token, timeoutMs)) { "the service refused the hold" }
                override suspend fun end(token: String) = controller.endBusy(token)
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
