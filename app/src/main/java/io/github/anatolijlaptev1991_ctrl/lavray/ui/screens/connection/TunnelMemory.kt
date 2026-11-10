// SPDX-License-Identifier: GPL-3.0-only
package io.github.anatolijlaptev1991_ctrl.lavray.ui.screens.connection

import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.flow.update

/**
 * What the Connection screen must not forget when its Activity is recreated (Back from the root screen and reopening, a
 * rotation the manifest does not absorb) while the same tunnel and the same :vpn process carry on: the notices the person
 * closed, the health basis of the running tunnel and the tunnel whose core notes were closed.
 *
 * It lives for the process, not for the Activity (`AppEnv` dies with the Activity), and holds nothing but booleans, small
 * numbers and a timestamp: no server, address, name, link or text of the service. Nothing is written to disk: when the UI
 * process is replaced the service state is re-read and a closed notice may be shown once more, which is the safe direction.
 * A tunnel is recognised by the core's own start time ([HealthBasis.startedAtMs]), so a new Activity adopts the basis and the
 * closed notes of the same core run, and a restarted core (another start time) starts clean.
 */
class TunnelMemory {
    private val mutableDismissed = MutableStateFlow(Dismissed())

    /** The closed notices. */
    val dismissed: StateFlow<Dismissed> = mutableDismissed.asStateFlow()

    @Volatile var basis: HealthBasis? = null
        private set

    /** The core start time of the tunnel whose notes the person closed. */
    @Volatile var warningsClosedFor: Long? = null
        private set

    @Volatile private var leftAtMs: Long? = null

    fun update(change: (Dismissed) -> Dismissed) = mutableDismissed.update(change)

    /** The screen went away at [nowMs] (a monotonic clock): the controller is unbound, nothing the service does is seen. */
    fun screenLeft(nowMs: Long) {
        leftAtMs = nowMs
    }

    /**
     * The screen is back. A closed notice is kept over a recreated Activity or a short trip away, where nothing can have
     * happened behind the screen's back. After [UNSEEN_MS] or more without watching, a tunnel may have come up and died
     * again unseen: the closed stop and failure notices are forgotten, so a stop that is reported now is shown. If it is
     * the old one, the notice is still true (the tunnel is still down); hiding a new one would leave the person without
     * the only signal a process death has.
     */
    fun screenBack(nowMs: Long) {
        val left = leftAtMs ?: return
        leftAtMs = null
        if (nowMs - left >= UNSEEN_MS) mutableDismissed.update { it.forgotten() }
    }

    fun remember(basis: HealthBasis) {
        this.basis = basis
    }

    fun closeWarningsFor(startedAtMs: Long?) {
        warningsClosedFor = startedAtMs
    }

    /** The basis taken for the same core run, if one was; [tunnel] is the number the caller now gives that tunnel. */
    fun basisFor(startedAtMs: Long?, tunnel: Int): HealthBasis? =
        basis?.takeIf { startedAtMs != null && it.startedAtMs == startedAtMs }?.copy(tunnel = tunnel)

    companion object {
        /** The one instance of the app process. */
        val process = TunnelMemory()

        /** Shorter than any connect-and-die of a tunnel, longer than a rotation or Back-and-reopen. */
        const val UNSEEN_MS = 30_000L
    }
}
