// SPDX-License-Identifier: GPL-3.0-only
package io.github.anatolijlaptev1991_ctrl.lavray.ui.screens.connection

import androidx.compose.runtime.Immutable
import io.github.anatolijlaptev1991_ctrl.lavray.ipc.VpnState
import io.github.anatolijlaptev1991_ctrl.lavray.ipc.VpnStatus
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.flow.SharingStarted
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.map
import kotlinx.coroutines.flow.scan
import kotlinx.coroutines.flow.stateIn

/**
 * WHICH TUNNEL A STATUS BELONGS TO, decided in one place: [after]. Everything that must not mix two tunnels up (the health
 * verdict and its basis, "the core's notes once per connection", the pending-changes state of the restart hub) takes its
 * identity from here, so the day the service puts a tunnel sequence number into [VpnStatus] only [after] changes.
 *
 * Today it is read from the edges of the status flow, with the limits that has: a flow that conflates states can hide a very
 * short rebuild.
 *
 * - [tunnel] counts the tunnels that came up: a new number each time the state turns CONNECTED after being anything else.
 *   The service shows CONNECTING for a rebuild and for the grace period after the core fell over, so a rebuilt tunnel is a
 *   new one here, exactly when the core starts a new health check.
 * - [builds] counts the starts of a build: a new number when the state enters CONNECTING (or WAITING_NETWORK) from anything
 *   else, and also when CONNECTED shows up with no build seen before it. A setting saved while a tunnel is CONNECTING is
 *   after the start of that build, so the number it was saved under is the one the finished tunnel carries: it is not
 *   "built from" the saved settings, and the pending change must survive.
 */
@Immutable
data class TunnelSight(val tunnel: Int = 0, val up: Boolean = false, val builds: Int = 0, val building: Boolean = false) {
    /** The only function that reads a tunnel's identity out of a status. */
    fun after(status: VpnStatus): TunnelSight = next(status.state)

    internal fun next(state: VpnState): TunnelSight = when (state) {
        VpnState.CONNECTING, VpnState.WAITING_NETWORK ->
            copy(up = false, building = true, builds = if (building) builds else builds + 1)
        VpnState.CONNECTED -> TunnelSight(
            tunnel = if (up) tunnel else tunnel + 1,
            up = true,
            builds = if (building || up) builds else builds + 1,
            building = false,
        )
        else -> copy(up = false, building = false)
    }
}

/** The builds of the tunnel as a flow ([TunnelSight.builds]); what the pending-changes state of the restart hub ends with. */
fun StateFlow<VpnStatus>.buildsIn(scope: CoroutineScope): StateFlow<Int> =
    scan(TunnelSight()) { sight, status -> sight.after(status) }.map { it.builds }.stateIn(scope, SharingStarted.Eagerly, 0)

/** True while a tunnel is up or on its way up; the pending-changes state ends with it. */
fun StateFlow<VpnStatus>.liveIn(scope: CoroutineScope): StateFlow<Boolean> =
    map { phaseOf(it.state).let { p -> p == Phase.On || p == Phase.Connecting || p == Phase.WaitingNetwork } }
        .stateIn(scope, SharingStarted.Eagerly, false)
