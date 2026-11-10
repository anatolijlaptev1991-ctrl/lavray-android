// SPDX-License-Identifier: GPL-3.0-only
package io.github.anatolijlaptev1991_ctrl.lavray.vpn

import org.junit.Assert.assertEquals
import org.junit.Test

class BlackholePolicyTest {
    private val policy = BlackholePolicy(capMs = 30_000)

    private fun input(
        held: Boolean = true,
        active: Boolean = false,
        established: Boolean = true,
        grace: Boolean = false,
        phase: AutoPhase = AutoPhase.CONNECTED,
    ) = HoldInput(held, active, established, grace, phase)

    @Test
    fun nothingHeldMeansNothingToDecide() {
        assertEquals(HoldDecision.None, policy.evaluate(0, input(held = false)))
    }

    @Test
    fun coreUsingTheInterfaceIsNotABlackhole() {
        assertEquals(HoldDecision.None, policy.evaluate(0, input(active = true)))
    }

    @Test
    fun graceAfterACoreDropHolds() {
        assertEquals(HoldDecision.Hold(30_000), policy.evaluate(0, input(grace = true)))
        assertEquals(HoldDecision.Hold(30_000), policy.evaluate(10_000, input(grace = true)))
    }

    @Test
    fun reconnectAttemptHolds() {
        assertEquals(HoldDecision.Hold(30_000), policy.evaluate(0, input(phase = AutoPhase.ATTEMPTING)))
    }

    @Test
    fun capReleasesAndSaysSo() {
        policy.evaluate(0, input(grace = true))
        assertEquals(HoldDecision.Release(capExpired = true), policy.evaluate(30_000, input(grace = true)))
    }

    @Test
    fun waitingForANetworkNeverHolds() {
        assertEquals(HoldDecision.Release(false), policy.evaluate(0, input(phase = AutoPhase.WAITING_NETWORK)))
    }

    @Test
    fun longBackoffNeverHolds() {
        policy.evaluate(0, input(phase = AutoPhase.ATTEMPTING))
        assertEquals(HoldDecision.Release(false), policy.evaluate(1_000, input(phase = AutoPhase.BACKING_OFF)))
    }

    @Test
    fun deliberateStopAndFailureNeverHold() {
        assertEquals(HoldDecision.Release(false), policy.evaluate(0, input(phase = AutoPhase.STOPPED, established = false)))
        assertEquals(HoldDecision.Release(false), policy.evaluate(0, input(phase = AutoPhase.FAILED)))
    }

    @Test
    fun aFirstAttemptThatNeverConnectedDoesNotHold() {
        assertEquals(HoldDecision.Release(false), policy.evaluate(0, input(phase = AutoPhase.ATTEMPTING, established = false)))
    }

    @Test
    fun capCountsFromTheBeginningOfEachHold() {
        policy.evaluate(0, input(grace = true))
        policy.evaluate(10_000, input(active = true))
        assertEquals(HoldDecision.Hold(70_000), policy.evaluate(40_000, input(grace = true)))
    }
}
