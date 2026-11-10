// SPDX-License-Identifier: GPL-3.0-only
package io.github.anatolijlaptev1991_ctrl.lavray.ui.connection

import io.github.anatolijlaptev1991_ctrl.lavray.api.gen.DnsSettings
import io.github.anatolijlaptev1991_ctrl.lavray.api.gen.LeakReport
import io.github.anatolijlaptev1991_ctrl.lavray.ui.core.CoreResult
import io.github.anatolijlaptev1991_ctrl.lavray.ui.core.ErrorKind
import io.github.anatolijlaptev1991_ctrl.lavray.ui.screens.connection.ExitProbe
import io.github.anatolijlaptev1991_ctrl.lavray.ui.screens.connection.ProbeBackend
import io.github.anatolijlaptev1991_ctrl.lavray.ui.screens.connection.ProbeState
import io.github.anatolijlaptev1991_ctrl.lavray.ui.screens.connection.probeKey
import io.github.anatolijlaptev1991_ctrl.lavray.ui.screens.servers.LatencyCell
import io.github.anatolijlaptev1991_ctrl.lavray.ui.screens.servers.LatencyStatus
import kotlinx.coroutines.CompletableDeferred
import kotlinx.coroutines.CoroutineStart
import kotlinx.coroutines.async
import kotlinx.coroutines.cancelAndJoin
import kotlinx.coroutines.launch
import kotlinx.coroutines.runBlocking
import kotlinx.coroutines.yield
import org.junit.Assert.assertEquals
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test
import java.util.concurrent.atomic.AtomicInteger

class ExitProbeTests {
    private val clean = LeakReport(tunnelIp = "198.51.100.9", directIp = "203.0.113.5", tunnelActive = true)

    private class Fake(var leaks: suspend () -> CoreResult<LeakReport>) : ProbeBackend {
        val leakCalls = AtomicInteger()
        val dnsCalls = AtomicInteger()
        val latencyCalls = AtomicInteger()

        override suspend fun leaks(): CoreResult<LeakReport> {
            leakCalls.incrementAndGet()
            return leaks.invoke()
        }

        override suspend fun dns(): CoreResult<DnsSettings> {
            dnsCalls.incrementAndGet()
            return CoreResult.Ok(DnsSettings(choice = "tunnel"))
        }

        override suspend fun latency(profileId: Long): LatencyCell? {
            latencyCalls.incrementAndGet()
            return LatencyCell(LatencyStatus.Ok, 87)
        }
    }

    private fun fake() = Fake { CoreResult.Ok(clean) }

    @Test fun theDelayIsNotMeasuredWhileAnotherLatencyTestRunsAndTheRestOfThePassStillIs() = runBlocking {
        // The core cuts an older latency batch when a newer one shares a server: the probe must not cut "test all".
        val fake = fake()
        var testRunning = true
        val probe = ExitProbe(fake, canMeasureLatency = { !testRunning })
        probe.ensure("1:1", 1)
        assertEquals("no latency batch was started", 0, fake.latencyCalls.get())
        val first = (probe.state.value as ProbeState.Ready).report
        assertNull("the report says nothing about a delay it did not measure", first.latencyMs)
        assertEquals(1, fake.leakCalls.get())
        // Once the test is over a manual refresh measures it.
        testRunning = false
        probe.refresh("1:1", 1)
        assertEquals(1, fake.latencyCalls.get())
        assertEquals(87, (probe.state.value as ProbeState.Ready).report.latencyMs)
    }

    @Test fun aKeyNeedsAServerAndAStartTime() {
        assertEquals("3:1700000", probeKey(3, 1_700_000))
        assertNull(probeKey(0, 1_700_000))
        assertNull(probeKey(3, 0))
    }

    @Test fun oneConnectionIsProbedOnceAndKeptAcrossScreenVisits() = runBlocking {
        val fake = fake()
        val probe = ExitProbe(fake) { 1234L }
        probe.ensure("1:100", 1L)
        probe.ensure("1:100", 1L)
        probe.ensure("1:100", 1L)
        assertEquals(1, fake.leakCalls.get())
        assertEquals(1, fake.dnsCalls.get())
        assertEquals(1, fake.latencyCalls.get())
        val ready = probe.state.value as ProbeState.Ready
        assertEquals("198.51.100.9", ready.report.exitIp)
        assertEquals(87, ready.report.latencyMs)
        assertEquals(1234L, ready.report.checkedAtMs)
    }

    @Test fun aReconnectIsANewConnectionAndGetsItsOwnPass() = runBlocking {
        val fake = fake()
        val probe = ExitProbe(fake)
        probe.ensure("1:100", 1L)
        probe.ensure("1:200", 1L)
        assertEquals(2, fake.leakCalls.get())
        assertEquals("1:200", probe.state.value.key)
    }

    @Test fun refreshAsksAgainEvenWhenAReportIsKept() = runBlocking {
        val fake = fake()
        val probe = ExitProbe(fake)
        probe.ensure("1:100", 1L)
        probe.refresh("1:100", 1L)
        assertEquals(2, fake.leakCalls.get())
    }

    @Test fun aFailedPassIsQuietAndIsTriedAgainWhenTheScreenReturns() = runBlocking {
        val fake = Fake { CoreResult.Fail(ErrorKind.CoreUnavailable) }
        val probe = ExitProbe(fake)
        probe.ensure("1:100", 1L)
        val failed = probe.state.value as ProbeState.Failed
        assertEquals(ErrorKind.CoreUnavailable, failed.kind)
        fake.leaks = { CoreResult.Ok(clean) }
        probe.ensure("1:100", 1L)
        assertTrue(probe.state.value is ProbeState.Ready)
        assertEquals(2, fake.leakCalls.get())
    }

    @Test fun cancellingMidPassLeavesNoHalfAnswer() = runBlocking {
        val gate = CompletableDeferred<CoreResult<LeakReport>>()
        val probe = ExitProbe(Fake { gate.await() })
        val job = launch(start = CoroutineStart.UNDISPATCHED) { probe.ensure("1:100", 1L) }
        assertTrue(probe.state.value is ProbeState.Running)
        job.cancelAndJoin()
        assertEquals(ProbeState.Idle, probe.state.value)
    }

    @Test fun cancellingARefreshBringsTheEarlierReportBack() = runBlocking {
        val fake = fake()
        val probe = ExitProbe(fake)
        probe.ensure("1:100", 1L)
        val before = probe.state.value
        val gate = CompletableDeferred<CoreResult<LeakReport>>()
        fake.leaks = { gate.await() }
        val job = launch(start = CoroutineStart.UNDISPATCHED) { probe.refresh("1:100", 1L) }
        assertTrue(probe.state.value is ProbeState.Running)
        job.cancelAndJoin()
        assertEquals(before, probe.state.value)
    }

    @Test fun aSecondPassIsNotStartedWhileOneRuns() = runBlocking {
        val gate = CompletableDeferred<CoreResult<LeakReport>>()
        val fake = Fake { gate.await() }
        val probe = ExitProbe(fake)
        val first = async(start = CoroutineStart.UNDISPATCHED) { probe.ensure("1:100", 1L) }
        probe.refresh("1:100", 1L)
        yield()
        assertEquals(1, fake.leakCalls.get())
        gate.complete(CoreResult.Ok(clean))
        first.await()
        assertTrue(probe.state.value is ProbeState.Ready)
    }

    @Test fun resetForgetsTheFinishedConnection() = runBlocking {
        val probe = ExitProbe(fake())
        probe.ensure("1:100", 1L)
        probe.reset()
        assertEquals(ProbeState.Idle, probe.state.value)
    }
}
