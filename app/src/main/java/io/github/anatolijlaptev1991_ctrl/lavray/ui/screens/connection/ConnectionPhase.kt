// SPDX-License-Identifier: GPL-3.0-only
package io.github.anatolijlaptev1991_ctrl.lavray.ui.screens.connection

import io.github.anatolijlaptev1991_ctrl.lavray.ipc.VpnState
import io.github.anatolijlaptev1991_ctrl.lavray.ui.design.MarkState

/** What the connection card says, derived from the service state. Pure, so it is unit tested. */
enum class Phase(val mark: MarkState, val canConnect: Boolean, val canDisconnect: Boolean) {
    Off(MarkState.Off, canConnect = true, canDisconnect = false),
    Connecting(MarkState.Connecting, canConnect = false, canDisconnect = true),
    WaitingNetwork(MarkState.Connecting, canConnect = false, canDisconnect = true),
    On(MarkState.On, canConnect = false, canDisconnect = true),
    Disconnecting(MarkState.Connecting, canConnect = false, canDisconnect = false),
    NeedsPermission(MarkState.Error, canConnect = true, canDisconnect = false),
    Error(MarkState.Error, canConnect = true, canDisconnect = false),
}

fun phaseOf(state: VpnState): Phase = when (state) {
    VpnState.IDLE -> Phase.Off
    VpnState.CONNECTING -> Phase.Connecting
    VpnState.WAITING_NETWORK -> Phase.WaitingNetwork
    VpnState.CONNECTED -> Phase.On
    VpnState.DISCONNECTING -> Phase.Disconnecting
    VpnState.NEEDS_PERMISSION -> Phase.NeedsPermission
    VpnState.ERROR -> Phase.Error
}

/** True while the tunnel is connected: the live statistics and the tunnel address are worth asking for only then. */
val Phase.isLive: Boolean get() = this == Phase.On

/**
 * True from the moment a connection is started until the tunnel is fully down, so also while it is coming up, waiting
 * for the network or going down. The core refuses operations that need "no connection" (restoring a backup) in all of
 * these, not only when connected.
 */
val Phase.hasTunnel: Boolean get() = this == Phase.On || this == Phase.Connecting || this == Phase.WaitingNetwork || this == Phase.Disconnecting
