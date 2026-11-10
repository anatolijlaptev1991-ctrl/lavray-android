// SPDX-License-Identifier: GPL-3.0-only
package io.github.anatolijlaptev1991_ctrl.lavray.ui.screens.settings

import io.github.anatolijlaptev1991_ctrl.lavray.ipc.VpnState
import io.github.anatolijlaptev1991_ctrl.lavray.ui.AppEnv
import kotlinx.coroutines.CoroutineStart
import kotlinx.coroutines.async
import kotlinx.coroutines.coroutineScope
import kotlinx.coroutines.flow.Flow
import kotlinx.coroutines.flow.map
import kotlinx.coroutines.flow.firstOrNull
import kotlinx.coroutines.withTimeoutOrNull

/**
 * The tunnel as the rebuild sees it: the states it passes through, and the one step that triggers the rebuild.
 * [bounce] is the only place that knows HOW the tunnel is rebuilt (today: disconnect, wait for it to be down, connect);
 * whether the rebuild succeeded is decided from the observed states alone ([rebuildOutcome]), so the trigger can be
 * replaced (by a service-side "reconnect") without touching the decision.
 */
internal interface TunnelControl {
    val states: Flow<VpnState>

    /** Tears the tunnel down and brings it up again. False when that could not even be started. */
    suspend fun bounce(): Boolean
}

/** Rebuilds the tunnel with the last server so a setting that is read when the tunnel is built takes effect now. */
object Reconnect {
    private const val UP_TIMEOUT_MS = 30_000L

    /** True when the tunnel left its state and is up again with the new settings; false in every other case. */
    suspend fun now(env: AppEnv): Boolean = rebuildTunnel(DisconnectConnect(env), UP_TIMEOUT_MS)
}

/** Disconnect, wait until the tunnel is fully down (connecting while it is still going down is taken as "already connected"), connect. */
internal class DisconnectConnect(private val env: AppEnv, private val downTimeoutMs: Long = DOWN_TIMEOUT_MS) : TunnelControl {
    override val states: Flow<VpnState> get() = env.vpn.status.map { it.state }

    override suspend fun bounce(): Boolean {
        env.controller.disconnect()
        val down = withTimeoutOrNull(downTimeoutMs) { env.vpn.status.firstOrNull { isDown(it.state) } }
        // The tunnel did not go down: connect would be answered "already connected" and nothing would be rebuilt.
        if (down == null) return false
        env.controller.connect(0L)
        return true
    }

    private fun isDown(state: VpnState): Boolean =
        state == VpnState.IDLE || state == VpnState.ERROR || state == VpnState.NEEDS_PERMISSION

    private companion object {
        const val DOWN_TIMEOUT_MS = 8_000L
    }
}

/**
 * Triggers the rebuild and judges it from the states seen after the trigger. A rebuild that was never triggered, or
 * that did not take the tunnel through a different state, or that did not end in CONNECTED within [upTimeoutMs], is a
 * failure: success is never assumed.
 */
internal suspend fun rebuildTunnel(control: TunnelControl, upTimeoutMs: Long): Boolean = coroutineScope {
    // Observation starts before the trigger, so no state of the rebuild is missed.
    val outcome = async(start = CoroutineStart.UNDISPATCHED) {
        withTimeoutOrNull(upTimeoutMs) { control.states.rebuildOutcome() } ?: false
    }
    if (control.bounce()) {
        outcome.await()
    } else {
        outcome.cancel()
        false
    }
}

/**
 * The first emission is the state when the rebuild began. True when afterwards the tunnel went through a different
 * state (down, or connecting again) and then reached CONNECTED; false when it ended in an error, a missing permission,
 * or back at IDLE after it had started connecting, or when the flow ends first. A tunnel that never leaves CONNECTED
 * is not a rebuild: the wait goes on until the caller's timeout.
 */
internal suspend fun Flow<VpnState>.rebuildOutcome(): Boolean {
    var baseline: VpnState? = null
    var left = false
    var started = false
    var result = false
    val end = firstOrNull { state ->
        val first = baseline
        if (first == null) {
            baseline = state
            left = state == VpnState.IDLE || state == VpnState.DISCONNECTING
            return@firstOrNull false
        }
        when (state) {
            VpnState.DISCONNECTING -> { left = true; false }
            VpnState.IDLE -> if (started) { result = false; true } else { left = true; false }
            VpnState.CONNECTING, VpnState.WAITING_NETWORK -> {
                // Connecting again after being connected (or after going down) is the rebuild; connecting from the start is not.
                if (left || first == VpnState.CONNECTED) { left = true; started = true }
                false
            }
            VpnState.CONNECTED -> if (left) { result = true; true } else false
            VpnState.ERROR, VpnState.NEEDS_PERMISSION -> { result = false; true }
        }
    }
    return end != null && result
}
