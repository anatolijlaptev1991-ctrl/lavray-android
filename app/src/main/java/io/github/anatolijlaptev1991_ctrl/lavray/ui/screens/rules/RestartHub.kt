// SPDX-License-Identifier: GPL-3.0-only
package io.github.anatolijlaptev1991_ctrl.lavray.ui.screens.rules

import androidx.compose.runtime.Immutable
import io.github.anatolijlaptev1991_ctrl.lavray.api.gen.coreRestartProfile
import io.github.anatolijlaptev1991_ctrl.lavray.ui.core.CoreResult
import io.github.anatolijlaptev1991_ctrl.lavray.ui.core.CoreSession
import io.github.anatolijlaptev1991_ctrl.lavray.ui.core.ErrorKind
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.flow.MutableSharedFlow
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.SharedFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asSharedFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.flow.update
import kotlinx.coroutines.launch

/** What was saved and is not in the running tunnel yet. */
enum class PendingReason {
    /** The route preset: the core rebuilds its config on a profile restart. */
    Routing,
    Dns,

    /** A setting the tunnel is built from (MTU, IPv6, FakeIP, excluded addresses, mux, TLS). */
    Tunnel,

    /** The per-app choice, which the VPN service puts into the interface when it is built. */
    Apps;

    /** True when restarting the core's profile is enough; the rest needs the tunnel itself to be built again. */
    val coreRestartSuffices: Boolean get() = this == Routing || this == Dns
}

/** The toast of a saved change: while the tunnel does not have it yet, "saved, applies after restart" rather than the plain text. */
@androidx.annotation.StringRes
fun savedOrPending(pending: Boolean, @androidx.annotation.StringRes plain: Int): Int =
    if (pending) io.github.anatolijlaptev1991_ctrl.lavray.R.string.toast_savedRestart else plain

@Immutable
data class RestartState(
    val reasons: Set<PendingReason> = emptySet(),
    /** "Later": the banner folds, the need stays (the Connection screen still shows it). */
    val snoozed: Boolean = false,
    val busy: Boolean = false,
) {
    val needed: Boolean get() = reasons.isNotEmpty()

    /** The tunnel has to be rebuilt, not just the core's profile restarted. */
    val needsReconnect: Boolean get() = reasons.any { !it.coreRestartSuffices }
}

sealed interface RestartEvent {
    /** [tunnelRebuilt]: the tunnel itself was built again, not only the core profile restarted. */
    data class Done(val tunnelRebuilt: Boolean) : RestartEvent
    data class Failed(val kind: ErrorKind) : RestartEvent
}

/**
 * "A saved change is not in the running tunnel yet", for the whole app: routes, DNS, tunnel settings and the per-app
 * choice raise it, the banner in the shell offers the restart, and the Connection screen keeps showing it after
 * "later". It lasts until the tunnel is restarted or goes down (the next connect starts with the saved settings), and
 * survives navigation because it lives as long as the app.
 *
 * [live] says whether a tunnel is up or on its way up; [reconnect] rebuilds the tunnel and reports whether it came up.
 * Only one restart runs at a time: a second tap while one is under way does nothing.
 */
class RestartHub(
    private val session: CoreSession,
    private val scope: CoroutineScope,
    private val live: StateFlow<Boolean>,
    private val reconnect: suspend () -> Boolean,
) {
    private val mutable = MutableStateFlow(RestartState())
    private val mutableEvents = MutableSharedFlow<RestartEvent>(extraBufferCapacity = 4)

    val state: StateFlow<RestartState> = mutable.asStateFlow()
    val events: SharedFlow<RestartEvent> = mutableEvents.asSharedFlow()

    init {
        scope.launch { live.collect { up -> if (!up) tunnelWentDown() } }
    }

    /** The core said the change needs a profile restart (so a tunnel is running): always recorded. */
    fun need(reason: PendingReason = PendingReason.Routing) {
        mutable.update { it.copy(reasons = it.reasons + reason, snoozed = false) }
    }

    /** The client knows the change is read only when the tunnel is built: recorded only while a tunnel is up. */
    fun needIfLive(reason: PendingReason) {
        if (live.value) need(reason)
    }

    /** "Later": the banner folds away, the need stays. */
    fun postpone() {
        mutable.update { if (it.needed) it.copy(snoozed = true) else it }
    }

    private fun tunnelWentDown() {
        // A restart in progress takes the tunnel down on purpose; its own end settles the state.
        mutable.update { if (it.busy) it else RestartState() }
    }

    /** Applies what is pending. The need is dropped only when the change really was applied. */
    fun restart() {
        val before = mutable.value
        if (before.busy || !before.needed) return
        mutable.value = before.copy(busy = true)
        scope.launch(Dispatchers.IO) {
            val outcome = if (before.needsReconnect) rebuildTunnel() else restartProfile()
            // Changes made while this ran are not covered by it and stay pending.
            mutable.update { s ->
                val left = if (outcome is RestartEvent.Failed && !before.needsReconnect) s.reasons else s.reasons - before.reasons
                // A tunnel that is down after the attempt starts from the saved settings next time: nothing is pending.
                if (live.value) s.copy(reasons = left, busy = false, snoozed = false) else RestartState()
            }
            mutableEvents.tryEmit(outcome)
        }
    }

    private suspend fun rebuildTunnel(): RestartEvent =
        if (reconnect()) RestartEvent.Done(tunnelRebuilt = true) else RestartEvent.Failed(ErrorKind.Generic)

    private suspend fun restartProfile(): RestartEvent = when (val r = session.call { coreRestartProfile() }) {
        is CoreResult.Fail -> RestartEvent.Failed(r.kind)
        is CoreResult.Ok -> RestartEvent.Done(tunnelRebuilt = false)
    }
}
