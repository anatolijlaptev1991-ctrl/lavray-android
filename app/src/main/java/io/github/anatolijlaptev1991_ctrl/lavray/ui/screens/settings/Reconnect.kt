// SPDX-License-Identifier: GPL-3.0-only
package io.github.anatolijlaptev1991_ctrl.lavray.ui.screens.settings

import io.github.anatolijlaptev1991_ctrl.lavray.ipc.VpnState
import io.github.anatolijlaptev1991_ctrl.lavray.ui.AppEnv
import kotlinx.coroutines.flow.Flow
import kotlinx.coroutines.flow.first
import kotlinx.coroutines.flow.map
import kotlinx.coroutines.withTimeoutOrNull

/** Rebuilds the tunnel with the last server so a setting that is read when the tunnel is built takes effect now. */
object Reconnect {
    private const val DOWN_TIMEOUT_MS = 8_000L
    private const val UP_TIMEOUT_MS = 30_000L

    /** True when the tunnel is up again with the new settings; false when it did not come up (the person is told). */
    suspend fun now(env: AppEnv): Boolean {
        env.controller.disconnect()
        // Wait for the tunnel to be fully down: connecting while it is still going down would be taken as "already connected".
        withTimeoutOrNull(DOWN_TIMEOUT_MS) { env.vpn.status.first { isDown(it.state) } }
        env.controller.connect(0L)
        return withTimeoutOrNull(UP_TIMEOUT_MS) { env.vpn.status.map { it.state }.upOrFailed() } ?: false
    }

    private fun isDown(state: VpnState): Boolean =
        state == VpnState.IDLE || state == VpnState.ERROR || state == VpnState.NEEDS_PERMISSION
}

/**
 * Follows a connection attempt that starts from a down tunnel: true when it reaches CONNECTED, false when it ends in
 * an error, a missing permission, or back at IDLE after it had started. The wait itself is bounded by the caller.
 */
suspend fun Flow<VpnState>.upOrFailed(): Boolean {
    var started = false
    val last = first { state ->
        when (state) {
            VpnState.CONNECTING, VpnState.WAITING_NETWORK -> { started = true; false }
            VpnState.CONNECTED, VpnState.ERROR, VpnState.NEEDS_PERMISSION -> true
            VpnState.IDLE -> started
            VpnState.DISCONNECTING -> false
        }
    }
    return last == VpnState.CONNECTED
}
