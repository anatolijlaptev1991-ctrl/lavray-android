// SPDX-License-Identifier: GPL-3.0-only
package io.github.anatolijlaptev1991_ctrl.lavray.vpn

import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test

class AutoConnectMachineTest {
    private val machine = AutoConnectMachine(RetryBackoff { 0.5 }) // 0.5 = no jitter

    private fun start(source: StartSource, consent: Boolean = true, network: Boolean = true) =
        machine.handle(AutoEvent.Start(source, consent, network))

    private fun delayOf(effects: List<AutoEffect>): Long = effects.filterIsInstance<AutoEffect.ScheduleRetry>().single().delayMs

    @Test
    fun userStartAttemptsOnceAndFailureIsFinal() {
        assertEquals(listOf(AutoEffect.StartAttempt), start(StartSource.USER))
        val fail = machine.handle(AutoEvent.AttemptFailed(permanent = false, message = "timeout"))
        assertEquals(AutoPhase.FAILED, machine.state.phase)
        assertTrue(fail.contains(AutoEffect.Fail("timeout")))
        assertTrue(fail.none { it is AutoEffect.ScheduleRetry })
    }

    @Test
    fun userStartWithoutNetworkStillAttempts() {
        assertEquals(listOf(AutoEffect.StartAttempt), start(StartSource.USER, network = false))
    }

    @Test
    fun missingConsentNotifiesAndDoesNotAttempt() {
        assertEquals(listOf(AutoEffect.NotifyNeedsPermission), start(StartSource.BOOT, consent = false))
        assertEquals(AutoPhase.IDLE, machine.state.phase)
        assertFalse(machine.state.pending)
    }

    @Test
    fun bootWithoutNetworkWaitsForIt() {
        assertEquals(listOf(AutoEffect.WatchNetwork(true)), start(StartSource.BOOT, network = false))
        assertEquals(AutoPhase.WAITING_NETWORK, machine.state.phase)
        assertTrue(machine.state.pending)
        val effects = machine.handle(AutoEvent.NetworkAvailable)
        assertEquals(listOf(AutoEffect.CancelRetry, AutoEffect.StartAttempt), effects)
        assertEquals(AutoPhase.ATTEMPTING, machine.state.phase)
    }

    @Test
    fun retryDelaysFollowTheSchedule() {
        start(StartSource.SYSTEM)
        val seen = ArrayList<Long>()
        repeat(7) {
            val effects = machine.handle(AutoEvent.AttemptFailed(false, "x"))
            seen += delayOf(effects)
            assertEquals(AutoPhase.BACKING_OFF, machine.state.phase)
            assertEquals(listOf(AutoEffect.StartAttempt), machine.handle(AutoEvent.TimerFired))
        }
        assertEquals(listOf(5_000L, 15_000L, 45_000L, 120_000L, 300_000L, 300_000L, 300_000L), seen)
    }

    @Test
    fun networkReturnResetsTheDelayAndAttemptsAtOnce() {
        start(StartSource.BOOT)
        repeat(3) {
            machine.handle(AutoEvent.AttemptFailed(false, "x"))
            machine.handle(AutoEvent.TimerFired)
        }
        machine.handle(AutoEvent.AttemptFailed(false, "x")) // 4th failure, now backing off 120 s
        assertEquals(listOf(AutoEffect.CancelRetry, AutoEffect.StartAttempt), machine.handle(AutoEvent.NetworkAvailable))
        val next = machine.handle(AutoEvent.AttemptFailed(false, "x"))
        assertEquals(5_000L, delayOf(next))
    }

    @Test
    fun networkLossWhileBackingOffWaitsAndTimerIsCancelled() {
        start(StartSource.BOOT)
        machine.handle(AutoEvent.AttemptFailed(false, "x"))
        assertEquals(listOf(AutoEffect.CancelRetry), machine.handle(AutoEvent.NetworkLost))
        assertEquals(AutoPhase.WAITING_NETWORK, machine.state.phase)
        assertTrue(machine.handle(AutoEvent.TimerFired).isEmpty())
    }

    @Test
    fun failureWithoutNetworkWaitsInsteadOfBackingOff() {
        start(StartSource.BOOT)
        machine.handle(AutoEvent.NetworkLost)
        val effects = machine.handle(AutoEvent.AttemptFailed(false, "x"))
        assertEquals(listOf(AutoEffect.WatchNetwork(true)), effects)
        assertEquals(AutoPhase.WAITING_NETWORK, machine.state.phase)
    }

    @Test
    fun permanentFailureStopsRetryingEvenForSystemStarts() {
        start(StartSource.SYSTEM)
        val effects = machine.handle(AutoEvent.AttemptFailed(permanent = true, message = "no server selected"))
        assertEquals(AutoPhase.FAILED, machine.state.phase)
        assertTrue(effects.contains(AutoEffect.Fail("no server selected")))
        assertTrue(effects.contains(AutoEffect.WatchNetwork(false)))
    }

    @Test
    fun successStopsWatchingAndClearsTheBackoff() {
        start(StartSource.BOOT)
        machine.handle(AutoEvent.AttemptFailed(false, "x"))
        machine.handle(AutoEvent.TimerFired)
        val ok = machine.handle(AutoEvent.AttemptSucceeded)
        assertEquals(listOf(AutoEffect.CancelRetry, AutoEffect.WatchNetwork(false)), ok)
        assertEquals(AutoPhase.CONNECTED, machine.state.phase)
        assertFalse(machine.state.pending)
    }

    @Test
    fun droppedConnectionIsRetriedForSystemStartsOnly() {
        start(StartSource.BOOT)
        machine.handle(AutoEvent.AttemptSucceeded)
        assertEquals(listOf(AutoEffect.WatchNetwork(true), AutoEffect.StartAttempt), machine.handle(AutoEvent.ConnectionLost))

        val user = AutoConnectMachine(RetryBackoff { 0.5 })
        user.handle(AutoEvent.Start(StartSource.USER, true, true))
        user.handle(AutoEvent.AttemptSucceeded)
        assertTrue(user.handle(AutoEvent.ConnectionLost).isEmpty())
        assertEquals(AutoPhase.IDLE, user.state.phase)
    }

    @Test
    fun manualDisconnectCancelsEverythingAndLaterEventsAreIgnored() {
        start(StartSource.BOOT)
        machine.handle(AutoEvent.AttemptFailed(false, "x"))
        assertEquals(listOf(AutoEffect.CancelRetry, AutoEffect.WatchNetwork(false)), machine.handle(AutoEvent.ManualDisconnect))
        assertEquals(AutoPhase.STOPPED, machine.state.phase)
        assertTrue(machine.handle(AutoEvent.TimerFired).isEmpty())
        assertTrue(machine.handle(AutoEvent.NetworkAvailable).isEmpty())
        assertTrue(machine.handle(AutoEvent.ConnectionLost).isEmpty())
        assertFalse(machine.state.pending)
    }

    @Test
    fun revokedConsentStopsAndNotifies() {
        start(StartSource.BOOT)
        machine.handle(AutoEvent.AttemptSucceeded)
        val effects = machine.handle(AutoEvent.ConsentRevoked)
        assertTrue(effects.contains(AutoEffect.NotifyNeedsPermission))
        assertEquals(AutoPhase.STOPPED, machine.state.phase)
    }

    @Test
    fun staleResultsOutsideAnAttemptAreIgnored() {
        assertTrue(machine.handle(AutoEvent.AttemptSucceeded).isEmpty())
        assertTrue(machine.handle(AutoEvent.AttemptFailed(false, "x")).isEmpty())
    }

    @Test
    fun aNewStartAfterManualDisconnectRunsAgain() {
        start(StartSource.USER)
        machine.handle(AutoEvent.ManualDisconnect)
        assertEquals(listOf(AutoEffect.StartAttempt), start(StartSource.USER))
    }
}
