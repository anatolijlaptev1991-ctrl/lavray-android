// SPDX-License-Identifier: GPL-3.0-only
package io.github.anatolijlaptev1991_ctrl.lavray.vpn

/** What a service start asks for. Parsed from plain values so it runs on the JVM. */
sealed interface StartCommand {
    data class Connect(val source: StartSource, val profileId: Long?) : StartCommand
    data object Disconnect : StartCommand

    /** Switch server or rebuild the tunnel: the interface stays blocked, not open, until the new one replaces it. */
    data class Reconnect(val profileId: Long?) : StartCommand

    /** The system restarted the service after the process died (START_STICKY, null intent): resume only if it was wanted. */
    data object Resume : StartCommand
}

object StartIntent {
    const val ACTION_DISCONNECT = "io.github.anatolijlaptev1991_ctrl.lavray.action.DISCONNECT"
    const val ACTION_RECONNECT = "io.github.anatolijlaptev1991_ctrl.lavray.action.RECONNECT"

    /** By name, so the UI process can address the service without loading its class. */
    const val SERVICE_CLASS = "io.github.anatolijlaptev1991_ctrl.lavray.vpn.LavrayVpnService"
    const val EXTRA_SOURCE = "source"
    const val EXTRA_PROFILE_ID = "profile_id"

    /**
     * Disconnect and reconnect actions win. A null intent is the system restarting a killed service
     * ([StartCommand.Resume]). Otherwise a start carrying our source extra is ours; a start without it is
     * the system launching Always-on VPN and connects the last server.
     */
    fun parse(action: String?, source: String?, profileId: Long, hasIntent: Boolean = true): StartCommand = when {
        !hasIntent -> StartCommand.Resume
        action == ACTION_DISCONNECT -> StartCommand.Disconnect
        action == ACTION_RECONNECT -> StartCommand.Reconnect(profileId.takeIf { it > 0 })
        else -> StartCommand.Connect(StartSource.fromWire(source), profileId.takeIf { it > 0 })
    }
}
