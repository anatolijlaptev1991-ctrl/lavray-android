// SPDX-License-Identifier: GPL-3.0-only
package io.github.anatolijlaptev1991_ctrl.lavray.ui.settings

import io.github.anatolijlaptev1991_ctrl.lavray.ipc.VpnState
import io.github.anatolijlaptev1991_ctrl.lavray.ui.screens.settings.TunnelControl
import io.github.anatolijlaptev1991_ctrl.lavray.ui.screens.settings.rebuildOutcome
import io.github.anatolijlaptev1991_ctrl.lavray.ui.screens.settings.rebuildTunnel
import kotlinx.coroutines.delay
import kotlinx.coroutines.flow.Flow
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.flowOf
import kotlinx.coroutines.runBlocking
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test
import java.util.concurrent.atomic.AtomicInteger

/**
 * Review H3 and L9: the decision "the tunnel was rebuilt" comes from the states seen after the trigger, never from the
 * trigger having been sent, so a tunnel that stayed up on its old settings is reported as not rebuilt.
 */
class ReconnectTest {
    private class FakeTunnel(
        start: VpnState = VpnState.CONNECTED,
        private val triggered: Boolean = true,
        private val script: suspend (MutableStateFlow<VpnState>) -> Unit = {},
    ) : TunnelControl {
        val state = MutableStateFlow(start)
        val bounces = AtomicInteger()
        override val states: Flow<VpnState> get() = state

        override suspend fun bounce(): Boolean {
            bounces.incrementAndGet()
            if (triggered) script(state)
            return triggered
        }
    }

    private suspend fun MutableStateFlow<VpnState>.step(next: VpnState) {
        delay(40)
        value = next
    }

    @Test fun aTunnelThatWentDownAndCameBackIsRebuilt() = runBlocking {
        val tunnel = FakeTunnel { s ->
            s.step(VpnState.DISCONNECTING); s.step(VpnState.IDLE); s.step(VpnState.CONNECTING); s.step(VpnState.CONNECTED)
        }
        assertTrue(rebuildTunnel(tunnel, 5_000))
    }

    @Test fun aRebuildWithoutAGapThroughConnectingCountsToo() = runBlocking {
        val tunnel = FakeTunnel { s -> s.step(VpnState.CONNECTING); s.step(VpnState.CONNECTED) }
        assertTrue(rebuildTunnel(tunnel, 5_000))
    }

    @Test fun aTunnelThatNeverLeftConnectedIsNotReportedAsRebuilt() = runBlocking {
        // The trigger was sent (and answered "already connected"), nothing changed: the old settings are still in force.
        val tunnel = FakeTunnel { s -> s.step(VpnState.CONNECTED) }
        assertFalse(rebuildTunnel(tunnel, 400))
    }

    @Test fun aTriggerThatCouldNotBeStartedIsAFailureAtOnce() = runBlocking {
        val tunnel = FakeTunnel(triggered = false)
        val started = System.nanoTime()
        assertFalse(rebuildTunnel(tunnel, 5_000))
        assertTrue("it did not wait for the long timeout", (System.nanoTime() - started) / 1_000_000 < 2_000)
        assertEquals(1, tunnel.bounces.get())
    }

    @Test fun aTunnelStuckConnectingIsNotReportedAsUp() = runBlocking {
        val tunnel = FakeTunnel { s -> s.step(VpnState.DISCONNECTING); s.step(VpnState.IDLE); s.step(VpnState.CONNECTING) }
        assertFalse(rebuildTunnel(tunnel, 500))
    }

    @Test fun aConnectThatFailsAfterTheTunnelWentDownIsAFailure() = runBlocking {
        val tunnel = FakeTunnel { s -> s.step(VpnState.IDLE); s.step(VpnState.CONNECTING); s.step(VpnState.IDLE) }
        assertFalse(rebuildTunnel(tunnel, 5_000))
        val withError = FakeTunnel { s -> s.step(VpnState.IDLE); s.step(VpnState.CONNECTING); s.step(VpnState.ERROR) }
        assertFalse(rebuildTunnel(withError, 5_000))
        val withoutConsent = FakeTunnel { s -> s.step(VpnState.IDLE); s.step(VpnState.NEEDS_PERMISSION) }
        assertFalse(rebuildTunnel(withoutConsent, 5_000))
    }

    @Test fun theOutcomeOfAStateSequence() = runBlocking {
        assertTrue(flowOf(VpnState.CONNECTED, VpnState.DISCONNECTING, VpnState.IDLE, VpnState.CONNECTING, VpnState.CONNECTED).rebuildOutcome())
        assertTrue(flowOf(VpnState.CONNECTED, VpnState.CONNECTING, VpnState.CONNECTED).rebuildOutcome())
        assertTrue("a tunnel that was already down and came up", flowOf(VpnState.IDLE, VpnState.CONNECTING, VpnState.CONNECTED).rebuildOutcome())
        assertFalse("never left", flowOf(VpnState.CONNECTED, VpnState.CONNECTED).rebuildOutcome())
        assertFalse(flowOf(VpnState.CONNECTED, VpnState.DISCONNECTING, VpnState.IDLE, VpnState.CONNECTING, VpnState.IDLE).rebuildOutcome())
        assertFalse(flowOf(VpnState.CONNECTED, VpnState.IDLE, VpnState.CONNECTING, VpnState.ERROR).rebuildOutcome())
        assertFalse("the flow ended before the tunnel was up", flowOf(VpnState.CONNECTED, VpnState.DISCONNECTING).rebuildOutcome())
        assertFalse("a tunnel that was only connecting is not a rebuild", flowOf(VpnState.CONNECTING, VpnState.CONNECTED).rebuildOutcome())
    }
}
