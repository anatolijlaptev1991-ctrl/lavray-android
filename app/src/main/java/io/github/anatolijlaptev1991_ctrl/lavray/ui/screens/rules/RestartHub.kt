// SPDX-License-Identifier: GPL-3.0-only
package io.github.anatolijlaptev1991_ctrl.lavray.ui.screens.rules

import androidx.annotation.StringRes
import androidx.compose.runtime.Immutable
import io.github.anatolijlaptev1991_ctrl.lavray.R
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
import kotlinx.coroutines.launch

/** What was saved and is not in the running tunnel yet. */
enum class PendingReason {
    /** The route preset: the core rebuilds its config on a profile restart. */
    Routing,
    Dns,

    /** Another setting the core read when it built its config (inbound, log level): a profile restart is enough. */
    Core,

    /** A setting the tunnel is built from (MTU, IPv6, FakeIP, excluded addresses, mux, TLS). */
    Tunnel,

    /** The per-app choice, which the VPN service puts into the interface when it is built. */
    Apps;

    /** True when restarting the core's profile is enough; the rest needs the tunnel itself to be built again. */
    val coreRestartSuffices: Boolean get() = this == Routing || this == Dns || this == Core
}

/**
 * The toast of a saved change. [needsRestart] is what THIS response said (the core's needs_profile_restart): only then
 * does the toast say "applies after restart". A global "something is pending" flag must not decide it: an unrelated
 * earlier change would make every later save claim a restart it does not need.
 */
@StringRes
fun savedOrPending(needsRestart: Boolean, @StringRes plain: Int): Int = if (needsRestart) R.string.toast_savedRestart else plain

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
 * "later". It lasts until the tunnel is restarted or a tunnel goes down or comes up (a tunnel built later starts from
 * the saved settings, so anything recorded before it is moot), and survives navigation because it lives as long as
 * the app.
 *
 * [live] says whether a tunnel is up or on its way up; [reconnect] rebuilds the tunnel and reports whether it really
 * was rebuilt and came up. Only one restart runs at a time. All state changes happen under one lock, so a change
 * recorded while a restart runs is never lost, and a second tap never starts a second restart.
 */
class RestartHub(
    private val session: CoreSession,
    private val scope: CoroutineScope,
    private val live: StateFlow<Boolean>,
    private val reconnect: suspend () -> Boolean,
) {
    private class Slot(val reason: PendingReason, val baseline: Any?)

    private val lock = Any()
    private val mutable = MutableStateFlow(RestartState())
    private val mutableEvents = MutableSharedFlow<RestartEvent>(extraBufferCapacity = 4)

    private var explicit = emptySet<PendingReason>()
    private val slots = LinkedHashMap<String, Slot>()
    private var reissued = emptySet<PendingReason>()
    private var snoozed = false
    private var busy = false

    val state: StateFlow<RestartState> = mutable.asStateFlow()
    val events: SharedFlow<RestartEvent> = mutableEvents.asSharedFlow()

    init {
        // The starting value is taken here, not in the coroutine: a need recorded right after construction must not be
        // taken for the edge "a tunnel came up" when the collector delivers the current value.
        var wasUp = live.value
        scope.launch {
            live.collect { up ->
                // A tunnel that went away or a new one that came up: what was recorded is moot (unless a restart of ours is running).
                if (up != wasUp) synchronized(lock) { if (!busy) resetLocked() }
                wasUp = up
            }
        }
    }

    private fun resetLocked() {
        explicit = emptySet()
        slots.clear()
        reissued = emptySet()
        snoozed = false
        publishLocked()
    }

    private fun publishLocked() {
        mutable.value = RestartState(reasons = explicit + slots.values.map { it.reason }, snoozed = snoozed, busy = busy)
    }

    /** The core said the change needs a profile restart (so a tunnel is running): always recorded. */
    fun need(reason: PendingReason = PendingReason.Routing) = synchronized(lock) {
        explicit = explicit + reason
        if (busy) reissued = reissued + reason
        snoozed = false
        publishLocked()
    }

    /** The client knows the change is read only when the tunnel is built: recorded only while a tunnel is up. */
    fun needIfLive(reason: PendingReason) {
        if (live.value) need(reason)
    }

    /**
     * A change to a value that is read when the tunnel is built, identified by [slot] (a setting key, "apps"). It is
     * recorded relative to what the running tunnel was built with: the first change remembers [before], and a change
     * back to that value withdraws the need again. Ignored while no tunnel is up.
     */
    fun noteTunnelChange(reason: PendingReason, slot: String, before: Any?, after: Any?) = synchronized(lock) {
        if (!live.value) return@synchronized
        val baseline = if (slot in slots) slots.getValue(slot).baseline else before
        if (after == baseline) {
            slots.remove(slot)
        } else {
            slots[slot] = Slot(reason, baseline)
            if (busy) reissued = reissued + reason
            snoozed = false
        }
        publishLocked()
    }

    /** "Later": the banner folds away, the need stays. */
    fun postpone() = synchronized(lock) {
        if (explicit.isNotEmpty() || slots.isNotEmpty()) {
            snoozed = true
            publishLocked()
        }
    }

    /** Applies what is pending. The need is dropped only when the change really was applied. */
    fun restart() {
        val before = synchronized(lock) {
            if (busy || mutable.value.reasons.isEmpty()) return
            busy = true
            publishLocked()
            mutable.value
        }
        scope.launch(Dispatchers.IO) {
            val outcome = if (before.needsReconnect) rebuildTunnel() else restartProfile()
            synchronized(lock) {
                busy = false
                when {
                    // A tunnel that is down after the attempt starts from the saved settings next time: nothing is pending.
                    !live.value -> resetLocked()
                    // Applied: what the restart covered goes, what was raised again while it ran stays.
                    outcome is RestartEvent.Done -> {
                        explicit = (explicit - before.reasons) + reissued
                        slots.entries.removeAll { it.value.reason in before.reasons && it.value.reason !in reissued }
                        reissued = emptySet()
                        snoozed = false
                        publishLocked()
                    }
                    // Not applied and the tunnel is up: it still runs the old settings, so the need stays.
                    else -> {
                        reissued = emptySet()
                        snoozed = false
                        publishLocked()
                    }
                }
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
