// SPDX-License-Identifier: GPL-3.0-only
package io.github.anatolijlaptev1991_ctrl.lavray.ui

import io.github.anatolijlaptev1991_ctrl.lavray.ipc.VpnState
import org.junit.Assert.assertEquals
import org.junit.Test

class SwitchPlanTest {
    @Test
    fun aTunnelThatIsDefinitelyDownIsConnectedAfresh() {
        for (s in listOf(VpnState.IDLE, VpnState.ERROR, VpnState.NEEDS_PERMISSION)) {
            assertEquals(s.name, SwitchPlan.Action.CONNECT, SwitchPlan.decide(s))
        }
    }

    // E-23: no connect is ever issued into a tunnel that is up, coming up, waiting, or still tearing down.
    @Test
    fun everyOtherStateGoesThroughReconnect() {
        for (s in listOf(VpnState.CONNECTED, VpnState.CONNECTING, VpnState.WAITING_NETWORK, VpnState.DISCONNECTING)) {
            assertEquals(s.name, SwitchPlan.Action.RECONNECT, SwitchPlan.decide(s))
        }
    }

    @Test
    fun noStateIsLeftUndecided() {
        for (s in VpnState.entries) SwitchPlan.decide(s)
    }
}
