// SPDX-License-Identifier: GPL-3.0-only
package io.github.anatolijlaptev1991_ctrl.lavray.vpn

/**
 * What survives the death of the :vpn process: whether the person wants the tunnel on, and the
 * reasons the UI shows when it is not. Written with commit(), never apply(): a process that is
 * killed a moment later must not lose the write.
 *
 * [wantConnected], [unexpectedStop], [stopReason] and [resumeAttempts] describe the runtime of ONE boot of the
 * phone: they carry the boot count they were written under ([bootCount]) and are read as cleared once the phone
 * has rebooted. The only thing that may carry the intent across a reboot is the auto-connect setting.
 */
data class PersistedTunnel(
    val wantConnected: Boolean = false,
    val unexpectedStop: Boolean = false,
    val stopReason: String? = null,
    val needsPermission: Boolean = false,
    val lastFailure: String? = null,
    val failureKind: String? = null,
    val failureReason: String? = null,
    /** Restarts by the system since the tunnel was last CONNECTED; stops the crash loop (see [ResumePolicy]). */
    val resumeAttempts: Int = 0,
    /** Settings.Global.BOOT_COUNT of the boot the fields above belong to; -1 when unknown. */
    val bootCount: Int = -1,
    /**
     * The kernel's boot id of that boot (/proc/sys/kernel/random/boot_id), null when unreadable. A ROM whose BOOT_COUNT is
     * missing, constant, or not bumped by a fast-boot still tells one boot from the next by it.
     */
    val bootId: String? = null,
)

interface TunnelStore {
    fun load(): PersistedTunnel

    /** Synchronous: returns after the write reached storage. */
    fun save(state: PersistedTunnel)
}

/**
 * Transitions of [PersistedTunnel]. The invariant: whenever the service gives up or stops on its own,
 * [PersistedTunnel.wantConnected] is false, so a `true` found in a fresh process of the same boot means the
 * previous run died without anyone deciding it. The receivers and the service each hold their own instance over
 * the same file; one process-wide lock orders their read-modify-write.
 */
class TunnelIntent(
    private val store: TunnelStore,
    private val bootCount: () -> Int = { -1 },
    private val bootId: () -> String? = { null },
) {
    /** Read through on every use, and read as cleared when it was written under an earlier boot. */
    val snapshot: PersistedTunnel get() = synchronized(LOCK) { current() }

    /** A new service process starts: a stale `true` is a death nobody decided. [exitReason] is read only then. */
    fun onProcessStarted(exitReason: () -> String?) = update {
        if (it.wantConnected && !it.unexpectedStop) {
            val reason = exitReason()
            // A force-stop (or the person ending the process) is a deliberate act, like Disconnect: the wish ends with it.
            if (ExitReasons.isDeliberate(reason)) {
                it.copy(wantConnected = false, resumeAttempts = 0)
            } else {
                it.copy(unexpectedStop = true, stopReason = reason ?: REASON_PROCESS_DIED)
            }
        } else {
            it
        }
    }

    /**
     * A start that carries the intent to connect (the user's tap, boot, the system's Always-on, a system restart): sets
     * the wish and clears the old complaints, not [PersistedTunnel.unexpectedStop].
     * Only entry points that carry intent call it; a start that merely executes an earlier request must not.
     */
    fun onConnectRequested() = update {
        it.copy(wantConnected = true, needsPermission = false, lastFailure = null, failureKind = null, failureReason = null)
    }

    /**
     * The core reached CONNECTED. Clears the complaints (the crash counter waits for [onStable]); never writes the wish: a deliberate
     * disconnect that is already recorded must not be undone by a result that was queued before it.
     */
    fun onConnected() = update {
        it.copy(unexpectedStop = false, stopReason = null, needsPermission = false, lastFailure = null, failureKind = null, failureReason = null)
    }

    /** The tunnel stayed CONNECTED long enough ([SessionEngine.STABLE_MS]): earlier deaths no longer count toward [ResumePolicy]. */
    fun onStable() = update { it.copy(resumeAttempts = 0) }

    fun onUserDisconnect() = update {
        it.copy(wantConnected = false, unexpectedStop = false, stopReason = null, resumeAttempts = 0)
    }

    /** Another VPN took over (or the user revoked the consent): the tunnel is over, nobody died. */
    fun onRevoked() = update {
        it.copy(wantConnected = false, unexpectedStop = false, stopReason = null, needsPermission = true, resumeAttempts = 0)
    }

    fun onPermissionMissing() = update { it.copy(wantConnected = false, needsPermission = true) }

    fun onPermissionGranted() = update { it.copy(needsPermission = false) }

    /** The service stops trying. [wasEstablished]: the tunnel had been up, so this is an unexpected stop. */
    fun onGaveUp(kind: String, message: String?, wasEstablished: Boolean, reason: String? = null) = update {
        it.copy(
            wantConnected = false,
            unexpectedStop = wasEstablished || it.unexpectedStop,
            stopReason = if (wasEstablished) kind else it.stopReason,
            lastFailure = message ?: it.lastFailure,
            failureKind = kind,
            failureReason = reason,
            resumeAttempts = 0,
        )
    }

    /** The service stops without anything having gone wrong: only the wish is cleared. */
    fun onServiceStopped() = update { it.copy(wantConnected = false, resumeAttempts = 0) }

    /** Something went wrong that does not end the session (for example the transition block ran into its cap). */
    fun onFailureNoted(kind: String, message: String?) = update { it.copy(lastFailure = message, failureKind = kind) }

    /** The system restarted the service after a death: counts toward [ResumePolicy.MAX_ATTEMPTS]. */
    fun onResumeAttempt() = update { it.copy(resumeAttempts = it.resumeAttempts + 1) }

    private fun current(): PersistedTunnel {
        val saved = store.load()
        val boot = bootCount()
        val id = bootId()
        val rebooted = (boot >= 0 && saved.bootCount != boot) || (id != null && saved.bootId != null && saved.bootId != id)
        if (rebooted) {
            return saved.copy(
                wantConnected = false, unexpectedStop = false, stopReason = null, resumeAttempts = 0,
                bootCount = if (boot >= 0) boot else saved.bootCount, bootId = id ?: saved.bootId,
            )
        }
        // First sight of an id: adopt it, nothing is cleared.
        return if (id != null && saved.bootId == null) saved.copy(bootId = id) else saved
    }

    private inline fun update(change: (PersistedTunnel) -> PersistedTunnel) = synchronized(LOCK) {
        val next = change(current())
        if (next != store.load()) store.save(next)
    }

    companion object {
        const val REASON_PROCESS_DIED = "process_died"
        private val LOCK = Any()
    }
}

enum class ResumeAction {
    /** Reconnect: the tunnel was wanted and consent is there. */
    CONNECT,

    /** Nobody asked for it: stop at once. */
    STOP_QUIET,

    /** It was wanted but the VPN consent is gone: say so. */
    NEEDS_PERMISSION,

    /** The process died repeatedly before the tunnel came up: stop restarting it. */
    GIVE_UP,
}

/** What a system restart (START_STICKY, null intent) does. */
object ResumePolicy {
    const val MAX_ATTEMPTS = 3

    fun decide(state: PersistedTunnel, consentGranted: Boolean): ResumeAction = when {
        !state.wantConnected -> ResumeAction.STOP_QUIET
        !consentGranted -> ResumeAction.NEEDS_PERMISSION
        state.resumeAttempts >= MAX_ATTEMPTS -> ResumeAction.GIVE_UP
        else -> ResumeAction.CONNECT
    }
}
