// SPDX-License-Identifier: GPL-3.0-only
package io.github.anatolijlaptev1991_ctrl.lavray.vpn

import io.github.anatolijlaptev1991_ctrl.lavray.ipc.FailureKind
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test
import java.util.concurrent.atomic.AtomicLong

class UserCommandsTest {
    private val store = MemoryTunnelStore()
    private val seq = AtomicLong(0)
    private val started = ArrayList<Triple<String?, Long, Long>>()
    private val posted = ArrayList<Pair<StartCommand, Long>>()
    private val refused = ArrayList<Pair<String?, Long>>()
    private var startFails: RuntimeException? = null

    private val commands = UserCommands(
        seq, TunnelIntent(store),
        { action, profile, n -> startFails?.let { throw it }; started += Triple(action, profile, n) },
        { c, n -> posted += c to n },
        { m, n -> refused += m to n },
    )

    @Test
    fun connectNumbersWritesTheWishThenStarts() {
        commands.connect(5)
        assertEquals(listOf(Triple<String?, Long, Long>(null, 5, 1)), started)
        assertTrue(store.value.wantConnected)
    }

    @Test
    fun reconnectIsItsOwnAction() {
        commands.reconnect(0)
        assertEquals(StartIntent.ACTION_RECONNECT, started.single().first)
    }

    @Test
    fun disconnectClearsTheWishAndPostsWithItsNumber() {
        commands.connect(5)
        commands.disconnect()
        assertFalse(store.value.wantConnected)
        assertEquals(listOf<Pair<StartCommand, Long>>(StartCommand.Disconnect to 2), posted)
    }

    @Test
    fun numbersNeverRepeatAndFollowTheOrderOfTheCalls() {
        commands.connect(1)
        commands.disconnect()
        commands.reconnect(2)
        assertEquals(listOf(1L, 3L), started.map { it.third })
        assertEquals(2L, posted.single().second)
        assertEquals(3L, seq.get())
    }

    // The wiring of a refused start: the failure is recorded, the person told, the loop released from waiting for the number.
    @Test
    fun aRefusedStartIsRecordedReportedAndAbandoned() {
        startFails = IllegalStateException("not allowed")
        commands.connect(5)
        assertFalse(store.value.wantConnected)
        assertEquals(FailureKind.RESTRICTED_START, store.value.failureKind)
        assertEquals(listOf<Pair<String?, Long>>("not allowed" to 1), refused)
    }

    @Test
    fun theNotificationDisconnectTakesANumberWithoutPosting() {
        commands.connect(5)
        val n = commands.takeDisconnect()
        assertEquals(2L, n)
        assertTrue(posted.isEmpty())
        assertFalse(store.value.wantConnected)
    }
}

class StartRouterTest {
    private var cleared = 0
    private fun route(command: StartCommand, fg: Boolean, seq: Long = 7) =
        StartRouter.route(command, seq, { fg }, { 99 }, { cleared++ })

    @Test
    fun connectAndReconnectAreDeliveredWhenForegroundIsEntered() {
        assertEquals(StartRoute.Deliver(StartCommand.Connect(StartSource.USER, 5), 7), route(StartCommand.Connect(StartSource.USER, 5), true))
        assertEquals(StartRoute.Deliver(StartCommand.Reconnect(5), 7), route(StartCommand.Reconnect(5), true))
    }

    @Test
    fun aRefusedForegroundDeliversNothing() {
        assertEquals(StartRoute.RefusedForeground(7), route(StartCommand.Connect(StartSource.SYSTEM, null), false))
        assertEquals(StartRoute.RefusedForeground(7), route(StartCommand.Reconnect(null), false))
    }

    @Test
    fun resumeIsDeliveredEvenWithoutForeground() {
        assertEquals(StartRoute.Deliver(StartCommand.Resume, 0), route(StartCommand.Resume, false))
    }

    @Test
    fun aRefusedKeepAliveDropsTheHoldsAndEndsTheStart() {
        assertEquals(StartRoute.KeepAliveRefused, route(StartCommand.KeepAlive, false))
        assertEquals(1, cleared)
        assertEquals(StartRoute.Deliver(StartCommand.KeepAlive, 0), route(StartCommand.KeepAlive, true))
        assertEquals(1, cleared)
    }

    @Test
    fun aDisconnectStartGetsItsNumberAndNeedsNoForeground() {
        assertEquals(StartRoute.Deliver(StartCommand.Disconnect, 99), route(StartCommand.Disconnect, false))
    }
}
