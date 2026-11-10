// SPDX-License-Identifier: GPL-3.0-only
package io.github.anatolijlaptev1991_ctrl.lavray.ui.rules

import io.github.anatolijlaptev1991_ctrl.lavray.api.Connection
import io.github.anatolijlaptev1991_ctrl.lavray.api.CoreEndpoint
import io.github.anatolijlaptev1991_ctrl.lavray.api.LavrayClient
import io.github.anatolijlaptev1991_ctrl.lavray.ui.core.CoreSession
import io.github.anatolijlaptev1991_ctrl.lavray.ui.screens.connection.Phase
import io.github.anatolijlaptev1991_ctrl.lavray.ui.screens.rules.PendingReason
import io.github.anatolijlaptev1991_ctrl.lavray.ui.screens.rules.RestartBannerRules
import io.github.anatolijlaptev1991_ctrl.lavray.ui.screens.rules.RestartEvent
import io.github.anatolijlaptev1991_ctrl.lavray.ui.screens.rules.RestartHub
import io.github.anatolijlaptev1991_ctrl.lavray.ui.screens.rules.RestartState
import kotlinx.coroutines.CompletableDeferred
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.CoroutineStart
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.SupervisorJob
import kotlinx.coroutines.async
import kotlinx.coroutines.cancel
import kotlinx.coroutines.coroutineScope
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.first
import kotlinx.coroutines.runBlocking
import kotlinx.coroutines.withTimeout
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test
import java.io.ByteArrayInputStream
import java.io.ByteArrayOutputStream
import java.io.IOException
import java.io.InputStream
import java.io.OutputStream
import java.util.concurrent.atomic.AtomicInteger

/** E-04, E-22 and review M3/L6/L8: the "saved, not yet in the running tunnel" state is global and never lies. */
class RestartHubTest {
    private class Conn(override val input: InputStream) : Connection {
        override val output: OutputStream = ByteArrayOutputStream()
        override fun abort() = Unit
        override fun close() = Unit
    }

    private val body = "{}".toByteArray()
    private val ok = ("HTTP/1.1 200 OK\r\nContent-Type: application/json\r\nContent-Length: ${body.size}\r\n\r\n").toByteArray() + body
    private val scope = CoroutineScope(SupervisorJob() + Dispatchers.Default)

    private fun session(core: Boolean = true) = CoreSession(
        { CoreEndpoint("/x", "t") },
        { ep -> LavrayClient(ep.token, { if (core) Conn(ByteArrayInputStream(ok)) else throw IOException("down") }, Dispatchers.IO) },
    )

    private fun hub(
        live: MutableStateFlow<Boolean>, core: Boolean = true, reconnect: suspend () -> Boolean = { true },
        built: MutableStateFlow<Int> = MutableStateFlow(0),
    ) = RestartHub(session(core), scope, live, reconnect, built)

    private suspend fun RestartHub.settled() = withTimeout(5_000) { state.first { !it.busy } }

    /** Starts the restart with a listener already attached: the events are not replayed to a late collector. */
    private suspend fun RestartHub.restartAndAwait(): RestartEvent = coroutineScope {
        val event = async(start = CoroutineStart.UNDISPATCHED) { events.first() }
        restart()
        withTimeout(5_000) { event.await() }
    }

    @Test fun aNeedSurvivesLaterAndIsOnlyFolded() {
        val hub = hub(MutableStateFlow(true))
        hub.need(PendingReason.Routing)
        hub.postpone()
        assertTrue(hub.state.value.needed)
        assertTrue(hub.state.value.snoozed)
        hub.need(PendingReason.Dns)
        assertFalse("a new change unfolds the banner", hub.state.value.snoozed)
        scope.cancel()
    }

    @Test fun theNeedEndsWhenTheTunnelGoesDown() = runBlocking {
        val live = MutableStateFlow(true)
        val hub = hub(live)
        hub.need(PendingReason.Routing)
        live.value = false
        withTimeout(5_000) { hub.state.first { !it.needed } }
        scope.cancel()
    }

    @Test fun aRebuiltTunnelEndsTheNeedEvenThoughItNeverLeftLive() = runBlocking {
        // A server switch is a reconnect: CONNECTED, CONNECTING, CONNECTED. The new tunnel is built from the saved settings.
        val live = MutableStateFlow(true)
        val built = MutableStateFlow(1)
        val hub = hub(live, built = built)
        hub.need(PendingReason.Dns)
        assertTrue(hub.state.value.needed)
        built.value = 2
        withTimeout(5_000) { hub.state.first { !it.needed } }
        scope.cancel()
    }

    @Test fun aTunnelBuiltByOurOwnRestartDoesNotEndTheNeedBeforeTheRestartSettles() = runBlocking {
        val live = MutableStateFlow(true)
        val built = MutableStateFlow(1)
        val hub = hub(live, reconnect = { built.value = 2; true }, built = built)
        hub.need(PendingReason.Tunnel)
        val event = hub.restartAndAwait()
        assertTrue(event is RestartEvent.Done)
        assertFalse("applied, so nothing is pending", hub.settled().needed)
        scope.cancel()
    }

    @Test fun aNeedRecordedWhileTheTunnelWasStoppingIsDroppedWhenTheNextTunnelComesUp() = runBlocking {
        // The core can answer "needs restart" while it is already stopping: the flag is down, so there is no later down-edge.
        val live = MutableStateFlow(false)
        val hub = hub(live)
        hub.need(PendingReason.Routing)
        assertTrue(hub.state.value.needed)
        live.value = true
        withTimeout(5_000) { hub.state.first { !it.needed } }
        scope.cancel()
    }

    @Test fun aClientSideNeedIsRecordedOnlyWhileATunnelIsUp() {
        val down = hub(MutableStateFlow(false))
        down.needIfLive(PendingReason.Tunnel)
        assertFalse(down.state.value.needed)
        val up = hub(MutableStateFlow(true))
        up.needIfLive(PendingReason.Tunnel)
        assertEquals(setOf(PendingReason.Tunnel), up.state.value.reasons)
        scope.cancel()
    }

    @Test fun routesAndDnsAreAppliedByARestartOfTheCoreAndTheNeedIsCleared() = runBlocking {
        val calls = AtomicInteger()
        val hub = hub(MutableStateFlow(true), reconnect = { calls.incrementAndGet(); true })
        hub.need(PendingReason.Routing)
        hub.need(PendingReason.Dns)
        hub.need(PendingReason.Core)
        assertFalse(hub.state.value.needsReconnect)
        assertEquals(RestartEvent.Done(false), hub.restartAndAwait())
        assertFalse(hub.settled().needed)
        assertEquals("the tunnel was not rebuilt", 0, calls.get())
        scope.cancel()
    }

    @Test fun aRestartThatTheCoreRefusesKeepsTheNeed() = runBlocking {
        val hub = hub(MutableStateFlow(true), core = false)
        hub.need(PendingReason.Routing)
        assertTrue(hub.restartAndAwait() is RestartEvent.Failed)
        val s = hub.settled()
        assertTrue("the running tunnel still has the old config", s.needed)
        assertFalse(s.snoozed)
        scope.cancel()
    }

    @Test fun tunnelSettingsAreAppliedByRebuildingTheTunnel() = runBlocking {
        val calls = AtomicInteger()
        val hub = hub(MutableStateFlow(true), reconnect = { calls.incrementAndGet(); true })
        hub.need(PendingReason.Routing)
        hub.need(PendingReason.Tunnel)
        assertTrue(hub.state.value.needsReconnect)
        assertEquals(RestartEvent.Done(true), hub.restartAndAwait())
        assertEquals(1, calls.get())
        assertFalse(hub.settled().needed)
        scope.cancel()
    }

    @Test fun aRebuildThatDidNotHappenKeepsTheNeedWhileTheTunnelStillRunsTheOldSettings() = runBlocking {
        val hub = hub(MutableStateFlow(true), reconnect = { false })
        hub.need(PendingReason.Tunnel)
        assertTrue(hub.restartAndAwait() is RestartEvent.Failed)
        val s = hub.settled()
        assertEquals("the tunnel is up and was not rebuilt: the need must stay", setOf(PendingReason.Tunnel), s.reasons)
        scope.cancel()
    }

    @Test fun aRebuildThatFailedAndLeftNoTunnelClearsTheNeed() = runBlocking {
        val live = MutableStateFlow(true)
        val hub = hub(live, reconnect = { live.value = false; false })
        hub.need(PendingReason.Tunnel)
        assertTrue(hub.restartAndAwait() is RestartEvent.Failed)
        assertFalse(hub.settled().needed)
        scope.cancel()
    }

    @Test fun aSecondTapWhileOneRestartRunsDoesNothing() = runBlocking {
        val gate = CompletableDeferred<Boolean>()
        val calls = AtomicInteger()
        val hub = hub(MutableStateFlow(true), reconnect = { calls.incrementAndGet(); gate.await() })
        hub.need(PendingReason.Apps)
        hub.restart()
        hub.restart()
        hub.restart()
        gate.complete(true)
        hub.settled()
        assertEquals(1, calls.get())
        scope.cancel()
    }

    @Test fun aChangeMadeDuringARestartStaysPending() = runBlocking {
        val gate = CompletableDeferred<Boolean>()
        val hub = hub(MutableStateFlow(true), reconnect = { gate.await() })
        hub.need(PendingReason.Tunnel)
        hub.restart()
        hub.need(PendingReason.Apps)
        gate.complete(true)
        assertEquals(setOf(PendingReason.Apps), hub.settled().reasons)
        scope.cancel()
    }

    @Test fun theSameReasonRaisedAgainDuringARestartIsNotDropped() = runBlocking {
        val gate = CompletableDeferred<Boolean>()
        val hub = hub(MutableStateFlow(true), reconnect = { gate.await() })
        hub.need(PendingReason.Tunnel)
        hub.restart()
        hub.need(PendingReason.Tunnel) // a second change of the same kind while the first is being applied
        gate.complete(true)
        assertEquals(setOf(PendingReason.Tunnel), hub.settled().reasons)
        scope.cancel()
    }

    @Test fun aChangeThatIsChangedBackWithdrawsTheNeed() {
        val hub = hub(MutableStateFlow(true))
        hub.noteTunnelChange(PendingReason.Apps, "apps", before = "all", after = "only a.b")
        assertTrue(hub.state.value.needed)
        hub.noteTunnelChange(PendingReason.Apps, "apps", before = "only a.b", after = "all")
        assertFalse("back to what the tunnel was built with", hub.state.value.needed)
        scope.cancel()
    }

    @Test fun theBaselineIsTheValueTheTunnelWasBuiltWithNotTheLastOne() {
        val hub = hub(MutableStateFlow(true))
        hub.noteTunnelChange(PendingReason.Tunnel, "tun.mtu", before = 9000L, after = 1400L)
        hub.noteTunnelChange(PendingReason.Tunnel, "tun.mtu", before = 1400L, after = 1300L)
        assertTrue(hub.state.value.needed)
        hub.noteTunnelChange(PendingReason.Tunnel, "tun.mtu", before = 1300L, after = 9000L)
        assertFalse(hub.state.value.needed)
        scope.cancel()
    }

    @Test fun oneChangedBackStillLeavesTheOtherPending() {
        val hub = hub(MutableStateFlow(true))
        hub.noteTunnelChange(PendingReason.Tunnel, "tun.mtu", 9000L, 1400L)
        hub.noteTunnelChange(PendingReason.Tunnel, "tun.ipv6", false, true)
        hub.noteTunnelChange(PendingReason.Tunnel, "tun.mtu", 1400L, 9000L)
        assertTrue(hub.state.value.needed)
        scope.cancel()
    }

    @Test fun aTunnelChangeIsIgnoredWhileNoTunnelIsUp() {
        val hub = hub(MutableStateFlow(false))
        hub.noteTunnelChange(PendingReason.Tunnel, "tun.mtu", 9000L, 1400L)
        assertFalse(hub.state.value.needed)
        scope.cancel()
    }

    @Test fun theBannerIsShownOnlyWithARestartableTunnelAndAnUnfoldedNeed() {
        val pending = RestartState(reasons = setOf(PendingReason.Routing))
        val folded = pending.copy(snoozed = true)
        listOf(Phase.On, Phase.Connecting, Phase.WaitingNetwork).forEach { phase ->
            assertTrue("$phase", RestartBannerRules.visible(pending, phase, showWhenSnoozed = false))
        }
        listOf(Phase.Off, Phase.Disconnecting, Phase.Error, Phase.NeedsPermission).forEach { phase ->
            assertFalse("$phase", RestartBannerRules.visible(pending, phase, showWhenSnoozed = true))
        }
        assertFalse("nothing pending", RestartBannerRules.visible(RestartState(), Phase.On, true))
        assertFalse("folded by Later", RestartBannerRules.visible(folded, Phase.On, showWhenSnoozed = false))
        assertTrue("the Connection screen shows it again", RestartBannerRules.visible(folded, Phase.On, showWhenSnoozed = true))
    }
}
