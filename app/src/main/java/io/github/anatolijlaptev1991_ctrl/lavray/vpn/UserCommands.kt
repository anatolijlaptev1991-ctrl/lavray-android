// SPDX-License-Identifier: GPL-3.0-only
package io.github.anatolijlaptev1991_ctrl.lavray.vpn

import io.github.anatolijlaptev1991_ctrl.lavray.ipc.FailureKind
import java.util.concurrent.atomic.AtomicLong

/**
 * The entry points of the person's commands (Binder `connect`, `reconnect`, `disconnect`, the notification's Disconnect):
 * each takes the next number, writes the wish with commit(), and only then lets the command on its way. The numbers
 * order the commands against each other in [SessionEngine]. Free of Android types; the service gives it the way to
 * start itself and to post events.
 */
internal class UserCommands(
    private val seq: AtomicLong,
    private val wish: TunnelIntent,
    /** Starts the service with the command; throws a RuntimeException when the system refuses. */
    private val startService: (action: String?, profileId: Long, seq: Long) -> Unit,
    private val post: (StartCommand, Long) -> Unit,
    /** The start was refused: tell the person, and let the loop know the numbered command will never come. */
    private val onStartRefused: (message: String?, seq: Long) -> Unit,
) {
    fun connect(profileId: Long) = start(null, profileId)

    fun reconnect(profileId: Long) = start(StartIntent.ACTION_RECONNECT, profileId)

    fun disconnect() {
        post(StartCommand.Disconnect, takeDisconnect())
    }

    /** A Disconnect that arrives as a start (the notification's action): the number and the wish, nothing posted. */
    fun takeDisconnect(): Long {
        val n = seq.incrementAndGet()
        wish.onUserDisconnect()
        return n
    }

    private fun start(action: String?, profileId: Long) {
        // The wish is written here, before the start, with commit(): a process that dies a moment later must still
        // be restored. The number orders this command against later ones.
        val n = seq.incrementAndGet()
        wish.onConnectRequested()
        try {
            startService(action, profileId, n)
        } catch (e: RuntimeException) {
            wish.onGaveUp(FailureKind.RESTRICTED_START, e.message, wasEstablished = false)
            onStartRefused(e.message, n)
        }
    }
}

/** What a service start turned into. */
internal sealed interface StartRoute {
    /** Hand the command to the loop; the service is sticky. */
    data class Deliver(val command: StartCommand, val seq: Long) : StartRoute

    /** The foreground promise of a connect could not be kept: nothing is started. Not sticky. */
    data class RefusedForeground(val seq: Long) : StartRoute

    /** The keep-alive start could not be foreground: the holds are dropped and the started state ends. Not sticky. */
    data object KeepAliveRefused : StartRoute
}

internal object StartRouter {
    /**
     * [enterForeground] returns false when the system refuses. A start that was promised to become foreground must do
     * so within seconds; when it cannot, nothing may be started. [takeDisconnect] numbers a Disconnect that arrived as
     * a start; [clearBusy] drops every busy hold.
     */
    fun route(
        command: StartCommand,
        seqFromIntent: Long,
        enterForeground: () -> Boolean,
        takeDisconnect: () -> Long,
        clearBusy: () -> Unit,
    ): StartRoute = when (command) {
        is StartCommand.Connect, is StartCommand.Reconnect ->
            if (enterForeground()) StartRoute.Deliver(command, seqFromIntent) else StartRoute.RefusedForeground(seqFromIntent)
        // A background restart is best effort: the tunnel matters more than the notification.
        StartCommand.Resume -> {
            enterForeground()
            StartRoute.Deliver(command, 0)
        }
        StartCommand.KeepAlive ->
            if (enterForeground()) {
                StartRoute.Deliver(command, 0)
            } else {
                // A service that never calls startForeground after startForegroundService is killed with a crash.
                clearBusy()
                StartRoute.KeepAliveRefused
            }
        StartCommand.Disconnect -> StartRoute.Deliver(command, takeDisconnect())
    }
}
