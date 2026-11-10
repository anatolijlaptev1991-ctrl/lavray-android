// SPDX-License-Identifier: GPL-3.0-only
package io.github.anatolijlaptev1991_ctrl.lavray.ui

import io.github.anatolijlaptev1991_ctrl.lavray.ipc.VpnState
import io.github.anatolijlaptev1991_ctrl.lavray.ipc.VpnStatus
import kotlinx.coroutines.CompletableDeferred
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.SupervisorJob
import kotlinx.coroutines.cancel
import kotlinx.coroutines.delay
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.runBlocking
import kotlinx.coroutines.withTimeout
import org.junit.After
import org.junit.Assert.assertEquals
import org.junit.Test
import java.io.IOException
import java.util.concurrent.CopyOnWriteArrayList

/** L-7 of the service review, UI half: choosing a server writes it down first, and that moment must not reorder the person's taps. */
class VpnActionsTest {
    private val scope = CoroutineScope(SupervisorJob() + Dispatchers.Default)
    private val log = CopyOnWriteArrayList<String>()

    private class Fake(val log: MutableList<String>, state: VpnState) : TunnelCommands {
        override val status: StateFlow<VpnStatus> = MutableStateFlow(VpnStatus(state))
        override fun connect(profileId: Long) { log += "connect:$profileId" }
        override fun reconnect(profileId: Long) { log += "reconnect:$profileId" }
        override fun disconnect() { log += "disconnect" }
    }

    @After
    fun stop() = scope.cancel()

    private fun waitFor(count: Int) = runBlocking { withTimeout(5_000) { while (log.size < count) delay(5) } }

    @Test fun aServerChosenInATunnelThatIsDownIsConnectedAndInOneThatIsUpIsRebuilt() {
        val down = VpnActions(Fake(log, VpnState.IDLE), { }, scope) { }
        down.connect(3)
        waitFor(1)
        val up = VpnActions(Fake(log, VpnState.CONNECTED), { }, scope) { }
        up.connect(4)
        waitFor(2)
        assertEquals(listOf("connect:3", "reconnect:4"), log.toList())
    }

    @Test fun aSlowWriteOfTheChoiceDoesNotLetALaterCommandOvertake() {
        val gate = CompletableDeferred<Unit>()
        val written = CopyOnWriteArrayList<Long>()
        val actions = VpnActions(Fake(log, VpnState.CONNECTED), { id -> if (id == 1L) gate.await(); written += id }, scope) { }
        actions.connect(1)
        actions.connect(2)
        actions.disconnect()
        runBlocking { delay(100) }
        assertEquals("nothing ran while the first write is pending", emptyList<String>(), log.toList())
        gate.complete(Unit)
        waitFor(3)
        assertEquals(listOf("reconnect:1", "reconnect:2", "disconnect"), log.toList())
        assertEquals(listOf(1L, 2L), written.toList())
    }

    @Test fun theChosenServerIsReportedAtOnce() {
        val chosen = CopyOnWriteArrayList<Long>()
        val gate = CompletableDeferred<Unit>()
        val actions = VpnActions(Fake(log, VpnState.IDLE), { gate.await() }, scope) { chosen += it }
        actions.connect(9)
        assertEquals("the selection is shown before the core answers", listOf(9L), chosen.toList())
        gate.complete(Unit)
        waitFor(1)
    }

    @Test fun aWriteThatFailsDoesNotHoldBackTheCommandNorTheOnesBehindIt() {
        val actions = VpnActions(Fake(log, VpnState.IDLE), { throw IOException("core down") }, scope) { }
        actions.connect(5)
        actions.disconnect()
        waitFor(2)
        assertEquals(listOf("connect:5", "disconnect"), log.toList())
    }
}
