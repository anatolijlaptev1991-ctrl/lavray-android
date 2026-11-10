// SPDX-License-Identifier: GPL-3.0-only
package io.github.anatolijlaptev1991_ctrl.lavray.vpn

import io.github.anatolijlaptev1991_ctrl.lavray.api.gen.CoreState
import io.github.anatolijlaptev1991_ctrl.lavray.ipc.VpnState
import io.github.anatolijlaptev1991_ctrl.lavray.ipc.VpnStatus
import io.github.anatolijlaptev1991_ctrl.lavray.ipc.VpnStatusWire
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test

class StatusModelTest {
    private fun state(i: StatusInputs) = StatusModel.compute(i).state

    @Test
    fun healthErrorPassesOnlyWhileConnected() {
        val connected = StatusModel.compute(StatusInputs(coreState = CoreState.CORE_STATE_CONNECTED, healthError = "no_traffic"))
        assertEquals(VpnState.CONNECTED, connected.state)
        assertEquals("no_traffic", connected.healthError)
        val blank = StatusModel.compute(StatusInputs(coreState = CoreState.CORE_STATE_CONNECTED, healthError = ""))
        assertNull(blank.healthError)
        val connecting = StatusModel.compute(StatusInputs(coreState = CoreState.CORE_STATE_CONNECTING, healthError = "no_traffic"))
        assertNull(connecting.healthError)
    }

    @Test
    fun deliberateDisconnectNeverShowsAnError() {
        val s = StatusInputs(coreState = CoreState.CORE_STATE_ERROR, coreError = "context canceled", phase = AutoPhase.STOPPED)
        assertEquals(VpnState.IDLE, state(s))
        assertNull(StatusModel.compute(s).error)
    }

    @Test
    fun errorOutsideDeliberateStopIsShown() {
        val s = StatusModel.compute(StatusInputs(coreState = CoreState.CORE_STATE_ERROR, coreError = "boom", phase = AutoPhase.CONNECTED))
        assertEquals(VpnState.ERROR, s.state)
        assertEquals("boom", s.error)
    }

    @Test
    fun graceShowsConnectingNotIdle() {
        val s = StatusInputs(coreState = CoreState.CORE_STATE_IDLE, phase = AutoPhase.CONNECTED, inGrace = true)
        assertEquals(VpnState.CONNECTING, state(s))
    }

    @Test
    fun runningAttemptHidesStaleCoreError() {
        assertEquals(VpnState.CONNECTING, state(StatusInputs(coreState = CoreState.CORE_STATE_ERROR, phase = AutoPhase.ATTEMPTING)))
    }

    @Test
    fun permissionBeatsEverything() {
        val s = StatusModel.compute(StatusInputs(coreState = CoreState.CORE_STATE_CONNECTED, needsPermission = true))
        assertEquals(VpnState.NEEDS_PERMISSION, s.state)
        assertTrue(s.needsPermission)
    }

    @Test
    fun disconnectingWhileTheCoreIsBeingAsked() {
        assertEquals(VpnState.DISCONNECTING, state(StatusInputs(coreState = CoreState.CORE_STATE_CONNECTED, disconnecting = true)))
    }

    @Test
    fun waitingAndFailedPhases() {
        assertEquals(VpnState.WAITING_NETWORK, state(StatusInputs(phase = AutoPhase.BACKING_OFF)))
        val failed = StatusModel.compute(StatusInputs(phase = AutoPhase.FAILED, lastFailure = "no server", failureKind = "no_server"))
        assertEquals(VpnState.ERROR, failed.state)
        assertEquals("no server", failed.error)
        assertEquals("no_server", failed.failureKind)
    }

    @Test
    fun extrasAreCarried() {
        val s = StatusModel.compute(
            StatusInputs(
                unexpectedStop = true, stopReason = "low_memory", skippedPackages = listOf("a.b"), blackholeUntilMs = 99L,
                lastFailure = "x",
            ),
        )
        assertTrue(s.unexpectedStop)
        assertEquals("low_memory", s.stopReason)
        assertEquals(listOf("a.b"), s.skippedPackages)
        assertTrue(s.trafficBlocked)
        assertEquals("x", s.lastFailure)
        assertNull(StatusModel.compute(StatusInputs(stopReason = "low_memory")).stopReason)
        assertFalse(StatusModel.compute(StatusInputs()).trafficBlocked)
    }

    @Test
    fun wireRoundTripKeepsEveryField() {
        val status = VpnStatus(
            VpnState.CONNECTED, null, "no_traffic", true, "low_memory", false, "msg", "tunnel_lost", listOf("p.q", "r.s"), 1234L,
        )
        val back = VpnStatusWire.decode(status.state.code, status.error, VpnStatusWire.detailOf(status))
        assertEquals(status, back)
    }

    @Test
    fun wireToleratesMissingUnknownAndBrokenDetail() {
        assertEquals(VpnStatus(VpnState.ERROR, "e"), VpnStatusWire.decode(6, "e", null))
        assertEquals(VpnStatus(VpnState.IDLE), VpnStatusWire.decode(0, null, "{not json"))
        assertEquals(true, VpnStatusWire.decode(0, null, """{"unexpectedStop":true,"future":1}""").unexpectedStop)
    }
}
