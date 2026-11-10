// SPDX-License-Identifier: GPL-3.0-only
package io.github.anatolijlaptev1991_ctrl.lavray.ui.screens.settings

import io.github.anatolijlaptev1991_ctrl.lavray.ipc.VpnState
import io.github.anatolijlaptev1991_ctrl.lavray.ui.AppEnv
import kotlinx.coroutines.flow.first
import kotlinx.coroutines.withTimeoutOrNull

/** Rebuilds the tunnel with the last server so a setting that is read when the tunnel is built takes effect now. */
object Reconnect {
    private const val DOWN_TIMEOUT_MS = 8_000L

    suspend fun now(env: AppEnv) {
        env.controller.reconnect(0L)
    }

    private fun isUp(state: VpnState): Boolean =
        state == VpnState.CONNECTED || state == VpnState.CONNECTING || state == VpnState.WAITING_NETWORK
}
