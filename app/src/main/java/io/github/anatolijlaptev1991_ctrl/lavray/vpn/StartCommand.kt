// SPDX-License-Identifier: GPL-3.0-only
package io.github.anatolijlaptev1991_ctrl.lavray.vpn

/** What a service start asks for. Parsed from plain values so it runs on the JVM. */
sealed interface StartCommand {
    data class Connect(val source: StartSource, val profileId: Long?) : StartCommand
    data object Disconnect : StartCommand
}

object StartIntent {
    const val ACTION_DISCONNECT = "io.github.anatolijlaptev1991_ctrl.lavray.action.DISCONNECT"
    /** By name, so the UI process can address the service without loading its class. */
    const val SERVICE_CLASS = "io.github.anatolijlaptev1991_ctrl.lavray.vpn.LavrayVpnService"
    const val EXTRA_SOURCE = "source"
    const val EXTRA_PROFILE_ID = "profile_id"

    /**
     * Disconnect action wins. Otherwise a start carrying our source extra is ours; a start
     * without it (including a null intent) is the system launching Always-on VPN and
     * connects the last server.
     */
    fun parse(action: String?, source: String?, profileId: Long): StartCommand =
        if (action == ACTION_DISCONNECT) {
            StartCommand.Disconnect
        } else {
            StartCommand.Connect(StartSource.fromWire(source), profileId.takeIf { it > 0 })
        }
}
