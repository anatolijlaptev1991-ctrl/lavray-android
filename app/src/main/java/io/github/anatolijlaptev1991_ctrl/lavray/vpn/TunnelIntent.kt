// SPDX-License-Identifier: GPL-3.0-only
package io.github.anatolijlaptev1991_ctrl.lavray.vpn

/**
 * What survives the death of the :vpn process: whether the person wants the tunnel on, and the
 * reasons the UI shows when it is not. Written with commit(), never apply(): a process that is
 * killed a moment later must not lose the write.
 */
data class PersistedTunnel(
    val wantConnected: Boolean = false,
    val unexpectedStop: Boolean = false,
    val stopReason: String? = null,
    val needsPermission: Boolean = false,
    val lastFailure: String? = null,
    val failureKind: String? = null,
)

interface TunnelStore {
    fun load(): PersistedTunnel

    /** Synchronous: returns after the write reached storage. */
    fun save(state: PersistedTunnel)
}

/**
 * Transitions of [PersistedTunnel]. The invariant: whenever the service gives up or stops on its own,
 * [PersistedTunnel.wantConnected] is false, so a `true` found in a fresh process means the previous
 * run died without anyone deciding it.
 */
class TunnelIntent(private val store: TunnelStore) {
    /** Read through on every use: the boot receiver and the service share one file in one process. */
    val snapshot: PersistedTunnel @Synchronized get() = store.load()

    /** A new service process starts: a stale `true` is a death nobody decided. */
    @Synchronized
    fun onProcessStarted(exitReason: String?) = update {
        if (it.wantConnected && !it.unexpectedStop) it.copy(unexpectedStop = true, stopReason = exitReason ?: REASON_PROCESS_DIED) else it
    }

    /** A start that will connect (user, boot, system, restart): clears the old complaints, not [PersistedTunnel.unexpectedStop]. */
    @Synchronized
    fun onConnectRequested() = update {
        it.copy(wantConnected = true, needsPermission = false, lastFailure = null, failureKind = null)
    }

    @Synchronized
    fun onConnected() = update {
        it.copy(wantConnected = true, unexpectedStop = false, stopReason = null, needsPermission = false, lastFailure = null, failureKind = null)
    }

    @Synchronized
    fun onUserDisconnect() = update {
        it.copy(wantConnected = false, unexpectedStop = false, stopReason = null)
    }

    /** Another VPN took over (or the user revoked the consent): the tunnel is over, nobody died. */
    @Synchronized
    fun onRevoked() = update {
        it.copy(wantConnected = false, unexpectedStop = false, stopReason = null, needsPermission = true)
    }

    @Synchronized
    fun onPermissionMissing() = update {
        it.copy(wantConnected = false, needsPermission = true)
    }

    @Synchronized
    fun onPermissionGranted() = update { it.copy(needsPermission = false) }

    /** The service stops trying. [wasEstablished]: the tunnel had been up, so this is an unexpected stop. */
    @Synchronized
    fun onGaveUp(kind: String, message: String?, wasEstablished: Boolean) = update {
        it.copy(
            wantConnected = false,
            unexpectedStop = wasEstablished || it.unexpectedStop,
            stopReason = if (wasEstablished) kind else it.stopReason,
            lastFailure = message ?: it.lastFailure,
            failureKind = kind,
        )
    }

    /** Something went wrong that does not end the session (for example the transition block ran into its cap). */
    @Synchronized
    fun onFailureNoted(kind: String, message: String?) = update { it.copy(lastFailure = message, failureKind = kind) }

    /** A boot without auto-connect: an intent from before the reboot does not survive it. */
    @Synchronized
    fun onBootWithoutAutoConnect() = update {
        it.copy(wantConnected = false, unexpectedStop = false, stopReason = null)
    }

    private inline fun update(change: (PersistedTunnel) -> PersistedTunnel) {
        val current = store.load()
        val next = change(current)
        if (next != current) store.save(next)
    }

    companion object {
        const val REASON_PROCESS_DIED = "process_died"
    }
}
