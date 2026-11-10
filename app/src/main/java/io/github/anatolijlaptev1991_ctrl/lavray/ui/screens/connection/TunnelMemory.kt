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

    fun update(change: (Dismissed) -> Dismissed) = mutableDismissed.update(change)

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
    }
}
