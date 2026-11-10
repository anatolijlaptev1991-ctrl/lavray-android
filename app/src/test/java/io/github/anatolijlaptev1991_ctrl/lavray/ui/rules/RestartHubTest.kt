// SPDX-License-Identifier: GPL-3.0-only
package io.github.anatolijlaptev1991_ctrl.lavray.ui.rules

import io.github.anatolijlaptev1991_ctrl.lavray.api.Connection
import io.github.anatolijlaptev1991_ctrl.lavray.api.CoreEndpoint
import io.github.anatolijlaptev1991_ctrl.lavray.api.LavrayClient
import io.github.anatolijlaptev1991_ctrl.lavray.ipc.VpnState
import io.github.anatolijlaptev1991_ctrl.lavray.ui.core.CoreSession
import io.github.anatolijlaptev1991_ctrl.lavray.ui.screens.rules.PendingReason
import io.github.anatolijlaptev1991_ctrl.lavray.ui.screens.rules.RestartEvent
import io.github.anatolijlaptev1991_ctrl.lavray.ui.screens.rules.RestartHub
import io.github.anatolijlaptev1991_ctrl.lavray.ui.screens.settings.upOrFailed
import kotlinx.coroutines.CompletableDeferred
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.CoroutineStart
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.async
import kotlinx.coroutines.coroutineScope
import kotlinx.coroutines.SupervisorJob
import kotlinx.coroutines.cancel
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.first
import kotlinx.coroutines.flow.flowOf
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

/** E-04, E-22: the "saved, not yet in the running tunnel" state is global, outlives "later", and ends with the tunnel. */
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

    private fun hub(live: MutableStateFlow<Boolean>, core: Boolean = true, reconnect: suspend () -> Boolean = { true }) =
        RestartHub(session(core), scope, live, reconnect)

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
        assertFalse(hub.state.value.needsReconnect)
        val event = hub.restartAndAwait()
        assertEquals(RestartEvent.Done(false), event)
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
        val s = hub.settled()
        assertEquals(setOf(PendingReason.Apps), s.reasons)
        scope.cancel()
    }

    @Test fun aRebuildThatDidNotComeUpIsReportedAsFailed() = runBlocking {
        val hub = hub(MutableStateFlow(true), reconnect = { false })
        hub.need(PendingReason.Tunnel)
        assertTrue(hub.restartAndAwait() is RestartEvent.Failed)
        scope.cancel()
    }

    @Test fun aReconnectEndsWhenTheTunnelIsUpOrFailsToStart() = runBlocking {
        assertTrue(flowOf(VpnState.IDLE, VpnState.CONNECTING, VpnState.CONNECTED).upOrFailed())
        assertTrue(flowOf(VpnState.IDLE, VpnState.CONNECTED).upOrFailed())
        assertFalse(flowOf(VpnState.IDLE, VpnState.CONNECTING, VpnState.IDLE).upOrFailed())
        assertFalse(flowOf(VpnState.IDLE, VpnState.CONNECTING, VpnState.ERROR).upOrFailed())
        assertFalse(flowOf(VpnState.IDLE, VpnState.NEEDS_PERMISSION).upOrFailed())
        // The IDLE before the attempt is not a failure.
        assertTrue(flowOf(VpnState.IDLE, VpnState.IDLE, VpnState.CONNECTING, VpnState.CONNECTED).upOrFailed())
    }
}
