// SPDX-License-Identifier: GPL-3.0-only
package io.github.anatolijlaptev1991_ctrl.lavray.ui.connection

import io.github.anatolijlaptev1991_ctrl.lavray.R
import io.github.anatolijlaptev1991_ctrl.lavray.api.ApiException
import io.github.anatolijlaptev1991_ctrl.lavray.api.Connection
import io.github.anatolijlaptev1991_ctrl.lavray.api.ConnectCode
import io.github.anatolijlaptev1991_ctrl.lavray.api.CoreEndpoint
import io.github.anatolijlaptev1991_ctrl.lavray.api.LavrayClient
import io.github.anatolijlaptev1991_ctrl.lavray.ipc.VpnState
import io.github.anatolijlaptev1991_ctrl.lavray.ipc.VpnStatus
import io.github.anatolijlaptev1991_ctrl.lavray.ui.core.CoreSession
import io.github.anatolijlaptev1991_ctrl.lavray.ui.core.ErrorKind
import io.github.anatolijlaptev1991_ctrl.lavray.ui.core.StreamFailure
import io.github.anatolijlaptev1991_ctrl.lavray.ui.rules.hubOver
import io.github.anatolijlaptev1991_ctrl.lavray.ui.screens.connection.ConnectionModel
import io.github.anatolijlaptev1991_ctrl.lavray.ui.screens.connection.FailureInfo
import io.github.anatolijlaptev1991_ctrl.lavray.ui.screens.connection.HealthBasis
import io.github.anatolijlaptev1991_ctrl.lavray.ui.screens.connection.HealthRules
import io.github.anatolijlaptev1991_ctrl.lavray.ui.screens.connection.HealthSignal
import io.github.anatolijlaptev1991_ctrl.lavray.ui.screens.connection.LinkHealth
import io.github.anatolijlaptev1991_ctrl.lavray.ui.screens.connection.Phase
import io.github.anatolijlaptev1991_ctrl.lavray.ui.screens.connection.TunnelMemory
import io.github.anatolijlaptev1991_ctrl.lavray.ui.screens.connection.TunnelSight
import io.github.anatolijlaptev1991_ctrl.lavray.ui.screens.connection.stateSubRes
import io.github.anatolijlaptev1991_ctrl.lavray.ui.screens.connection.stopReasonText
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.SupervisorJob
import kotlinx.coroutines.cancel
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.first
import kotlinx.coroutines.runBlocking
import kotlinx.coroutines.withTimeout
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test
import java.io.ByteArrayInputStream
import java.io.ByteArrayOutputStream
import java.io.InputStream
import java.io.OutputStream
import java.util.IdentityHashMap
import java.util.concurrent.atomic.AtomicInteger

/** Review R6 of the integration commits: M3 (hub vs CONNECTING), L1, L3, L4, L6 and the stop reasons of L12. */
class ReviewR6Tests {
    private fun states(vararg s: VpnState): List<TunnelSight> = s.runningFold(TunnelSight()) { sight, st -> sight.next(st) }

    // ---- the one place that decides a tunnel's identity

    @Test fun buildsCountTheStartOfABuildAndTunnelsCountTheConnectedEdge() {
        val sights = states(VpnState.IDLE, VpnState.CONNECTING, VpnState.CONNECTING, VpnState.CONNECTED, VpnState.CONNECTED)
        assertEquals(1, sights.last().builds)
        assertEquals(1, sights.last().tunnel)
        val rebuilt = states(VpnState.CONNECTING, VpnState.CONNECTED, VpnState.CONNECTING, VpnState.CONNECTED)
        assertEquals(2, rebuilt.last().builds)
        assertEquals(2, rebuilt.last().tunnel)
        // A conflated flow that never showed CONNECTING still counts the build when the tunnel is up.
        val conflated = states(VpnState.IDLE, VpnState.CONNECTED)
        assertEquals(1, conflated.last().builds)
        // WAITING_NETWORK between attempts is the same build.
        assertEquals(1, states(VpnState.CONNECTING, VpnState.WAITING_NETWORK, VpnState.CONNECTING, VpnState.CONNECTED).last().builds)
    }

    @Test fun theIdentityIsReadFromTheStatusByOneFunction() {
        assertEquals(TunnelSight().next(VpnState.CONNECTED), TunnelSight().after(VpnStatus(VpnState.CONNECTED, healthError = "x")))
    }

    private fun statuses(vararg s: Pair<VpnState, Int>): TunnelSight =
        s.fold(TunnelSight()) { sight, (state, seq) -> sight.after(VpnStatus(state, tunnelSeq = seq)) }

    @Test fun aRebuildTheFlowHidIsANewTunnelByTheServicesNumber() {
        // CONNECTED, then CONNECTED again with the service's number moved: no edge was seen, the tunnel is new all the same.
        val hidden = statuses(VpnState.CONNECTING to 0, VpnState.CONNECTED to 1, VpnState.CONNECTED to 2)
        assertEquals(2, hidden.tunnel)
        assertEquals(2, hidden.builds)
        // The same number again (a status re-broadcast, a health change) is the same tunnel.
        val same = statuses(VpnState.CONNECTING to 0, VpnState.CONNECTED to 1, VpnState.CONNECTED to 1, VpnState.CONNECTED to 1)
        assertEquals(1, same.tunnel)
        assertEquals(1, same.builds)
    }

    @Test fun aRebuildSeenByItsEdgesIsCountedOnceWhateverTheNumberDoes() {
        val seen = statuses(VpnState.CONNECTING to 0, VpnState.CONNECTED to 1, VpnState.CONNECTING to 1, VpnState.CONNECTED to 2)
        assertEquals(2, seen.tunnel)
        assertEquals(2, seen.builds)
        // A new :vpn process starts its numbers again: the edge alone decides, the smaller number changes nothing more.
        val newProcess = statuses(VpnState.CONNECTED to 5, VpnState.IDLE to 0, VpnState.CONNECTING to 0, VpnState.CONNECTED to 1)
        assertEquals(2, newProcess.tunnel)
        assertEquals(2, newProcess.builds)
        // A service that sends no number (0 throughout) behaves as before.
        val unnumbered = statuses(VpnState.CONNECTED to 0, VpnState.CONNECTED to 0, VpnState.CONNECTING to 0, VpnState.CONNECTED to 0)
        assertEquals(2, unnumbered.tunnel)
    }

    // ---- M3: the restart hub against the real sequence of states

    @Test fun aChangeSavedWhileTheTunnelIsConnectingSurvivesItsConnectedEdge() = runBlocking {
        val (hub, feed, scope) = hubOver()
        feed(VpnState.CONNECTED)
        feed(VpnState.CONNECTING) // a rebuild starts: the build reads the settings in force now
        hub.need(io.github.anatolijlaptev1991_ctrl.lavray.ui.screens.rules.PendingReason.Dns) // saved during the build
        feed(VpnState.CONNECTED)
        kotlinx.coroutines.delay(150)
        assertTrue("the running tunnel was built before the change: it is still pending", hub.state.value.needed)
        scope.cancel()
    }

    @Test fun aChangeSavedBeforeARebuildStartsEndsWithItsBuild() = runBlocking {
        val (hub, feed, scope) = hubOver()
        feed(VpnState.CONNECTED)
        hub.need(io.github.anatolijlaptev1991_ctrl.lavray.ui.screens.rules.PendingReason.Dns) // saved while connected
        feed(VpnState.CONNECTING) // a server switch rebuilds: the new build reads it
        withTimeout(5_000) { hub.state.first { !it.needed } }
        feed(VpnState.CONNECTED)
        kotlinx.coroutines.delay(100)
        assertFalse(hub.state.value.needed)
        scope.cancel()
    }

    // ---- L1: the epoch and the cache are one step

    @Test fun noClientBuiltBeforeAnEpochChangeIsServedAfterIt() {
        val latest = AtomicInteger(0)
        val tokens = IdentityHashMap<LavrayClient, Int>()
        val session = CoreSession(
            { CoreEndpoint("/s", "t${latest.get()}") },
            { ep ->
                LavrayClient(ep.token, { Conn(ByteArrayInputStream(ByteArray(0))) }, Dispatchers.IO).also { synchronized(tokens) { tokens[it] = ep.token.removePrefix("t").toInt() } }
            },
        )
        val stop = java.util.concurrent.atomic.AtomicBoolean(false)
        val hammer = Thread { while (!stop.get()) runBlocking { session.client() } }
        hammer.start()
        try {
            for (i in 1..3_000) {
                latest.set(i)
                session.onCoreEpoch(i)
                val served = runBlocking { session.client() }
                val built = synchronized(tokens) { tokens[served] }!!
                assertTrue("epoch $i served a client built for epoch $built", built >= i)
            }
        } finally {
            stop.set(true)
            hammer.join()
        }
    }

    private class Conn(override val input: InputStream) : Connection {
        override val output: OutputStream = ByteArrayOutputStream()
        override fun abort() = Unit
        override fun close() = Unit
    }

    // ---- L3: a wall clock that went backwards

    @Test fun theVerdictIsAskedAgainWhenTheClockWasSteppedBackDuringTheSleep() = runBlocking {
        val signal = HealthSignal(true, null, 1)
        val basis = HealthBasis.firstSight(1, 1_000L, true, 2_000L)
        var now = 2_000L + 10_000L
        val slept = mutableListOf<Long>()
        HealthRules.awaitVerdict(signal, basis, { now }) { ms ->
            slept += ms
            // The first sleep ends with the wall clock set BACK by a minute (an NTP step), the second sees it run on.
            now = if (slept.size == 1) now - 60_000L + ms else now + ms
        }
        assertTrue("one sleep was not trusted: $slept", slept.size >= 2)
        assertTrue(HealthRules.of(signal, basis, now) != LinkHealth.Checking)
    }

    @Test fun nothingIsAwaitedWhenThereIsNothingToWaitFor() = runBlocking {
        var slept = 0
        HealthRules.awaitVerdict(HealthSignal.None, null, { 0L }) { slept++ }
        assertEquals(0, slept)
    }

    // ---- L4: what an Activity recreation must not forget

    private fun up(started: Long = 5_000L, warnings: List<String> = emptyList()) = VpnStatus(VpnState.CONNECTED, warnings = warnings)

    private fun model(status: MutableStateFlow<VpnStatus>, scope: CoroutineScope, memory: TunnelMemory, enabled: Boolean? = true, startedAt: Long? = 5_000L, now: Long = 6_000L) =
        ConnectionModel(CoreSession({ CoreEndpoint("/x", "t") }), scope, status, { enabled }, { startedAt }, { now }, memory)

    @Test fun aRecreatedScreenKeepsTheClosedNoticesAndTheBasisOfTheSameTunnel() = runBlocking {
        val memory = TunnelMemory()
        val scope = CoroutineScope(SupervisorJob() + Dispatchers.Default)
        val first = model(MutableStateFlow(up()), scope, memory)
        val basis = withTimeout(5_000) { first.healthBasis.first { it != null } }!!
        first.dismissStop()
        first.dismissFailure("protection_off")
        first.dismissWarnings()

        // The Activity is recreated: a new model, a controller that knows only IDLE at first, then the same tunnel.
        val status = MutableStateFlow(VpnStatus.IDLE)
        // Ten minutes later: a screen that took the basis afresh would call the tunnel old and untrusted.
        val second = model(status, scope, memory, now = 6_000L + 600_000L)
        assertTrue("closed stop notice kept", second.dismissed.value.stop)
        status.value = up()
        val adopted = withTimeout(5_000) { second.healthBasis.first { it != null } }!!
        assertTrue("the same core run: still trusted", adopted.trusted)
        assertEquals(basis.readyAtMs, adopted.readyAtMs)
        assertTrue("the notes closed for this core run stay closed", second.dismissed.value.warningsOfTunnel == second.health.value.tunnel)
        scope.cancel()
    }

    @Test fun aTunnelWithAnotherCoreStartTimeStartsClean() = runBlocking {
        val memory = TunnelMemory()
        val scope = CoroutineScope(SupervisorJob() + Dispatchers.Default)
        val first = model(MutableStateFlow(up()), scope, memory, startedAt = 5_000L)
        withTimeout(5_000) { first.healthBasis.first { it != null } }
        first.dismissWarnings()
        val second = model(MutableStateFlow(up()), scope, memory, startedAt = 90_000L, now = 91_000L)
        val basis = withTimeout(5_000) { second.healthBasis.first { it != null } }!!
        assertEquals(91_000L, basis.readyAtMs)
        assertEquals("the notes of the old core run do not hide the new run's", 0, second.dismissed.value.warningsOfTunnel)
        scope.cancel()
    }

    @Test fun theMemoryHoldsNoTextOfTheService() {
        // Documented by shape: booleans, numbers and a timestamp only.
        val fields = TunnelMemory::class.java.declaredFields.map { it.name }
        assertFalse(fields.any { it.contains("name", ignoreCase = true) || it.contains("url", ignoreCase = true) || it.contains("error", ignoreCase = true) })
    }

    // ---- L6 and the stop reasons

    @Test fun aStaleFailureKindNeverContradictsThePermissionBanner() {
        assertEquals(R.string.fail_permission, stateSubRes(Phase.NeedsPermission, LinkHealth.Unknown, FailureInfo("connect_failed", "x", "no_server")))
        assertEquals(R.string.fail_permission, stateSubRes(Phase.NeedsPermission, LinkHealth.Unknown, FailureInfo.None))
    }

    @Test fun stopReasonsThatAreFailureKindsGetNoBatteryHint() {
        assertEquals(R.string.nt_stop_lost to false, stopReasonText("connect_failed"))
        assertEquals(R.string.nt_stop_other to false, stopReasonText("core_stop_timeout"))
        assertEquals(R.string.nt_stop_crash to false, stopReasonText("repeated_crash"))
        assertEquals(true, stopReasonText("low_memory").second)
    }

    @Test fun aFrameTooLargeForThePhoneIsTheCoresAnswerNotTheProvidersRefusal() {
        val failure = StreamFailure(ErrorKind.TooLarge, ApiException(ConnectCode.RESOURCE_EXHAUSTED, "frame", reason = ApiException.REASON_TOO_LARGE))
        val out = io.github.anatolijlaptev1991_ctrl.lavray.ui.screens.subscriptions.UpdateOutcome()
        out.failed(failure)
        val r = out.toResult() as io.github.anatolijlaptev1991_ctrl.lavray.ui.screens.subscriptions.FetchResult.Failed
        assertEquals(ErrorKind.TooLarge, r.coreProblem)
        assertNull(null)
    }
}
