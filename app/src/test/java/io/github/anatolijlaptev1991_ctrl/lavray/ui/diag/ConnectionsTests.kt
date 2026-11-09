// SPDX-License-Identifier: GPL-3.0-only
package io.github.anatolijlaptev1991_ctrl.lavray.ui.diag

import io.github.anatolijlaptev1991_ctrl.lavray.api.gen.Connection
import io.github.anatolijlaptev1991_ctrl.lavray.ui.screens.diag.ConnChip
import io.github.anatolijlaptev1991_ctrl.lavray.ui.screens.diag.ConnFilter
import io.github.anatolijlaptev1991_ctrl.lavray.ui.screens.diag.ConnFrame
import io.github.anatolijlaptev1991_ctrl.lavray.ui.screens.diag.ConnRow
import io.github.anatolijlaptev1991_ctrl.lavray.ui.screens.diag.ConnStatus
import io.github.anatolijlaptev1991_ctrl.lavray.ui.screens.diag.ConnsAction
import io.github.anatolijlaptev1991_ctrl.lavray.ui.screens.diag.ConnsReducer
import io.github.anatolijlaptev1991_ctrl.lavray.ui.screens.diag.ConnsState
import io.github.anatolijlaptev1991_ctrl.lavray.ui.screens.diag.StreamBackoff
import io.github.anatolijlaptev1991_ctrl.lavray.ui.core.ErrorKind
import io.github.anatolijlaptev1991_ctrl.lavray.ui.core.StreamFailure
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.flow.flow
import kotlinx.coroutines.flow.flowOf
import kotlinx.coroutines.runBlocking
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test

class ConnectionsTests {
    private fun row(id: String, dest: String = "$id.example", outbound: String = "proxy", chain: List<String> = emptyList(), status: ConnStatus = ConnStatus.Active, process: String? = null) =
        ConnRow(id, status, dest, null, outbound, chain, null, process, null, "tcp", 1, 2, 1_000, null)

    private fun frame(vararg active: ConnRow, closed: Set<String> = emptySet(), truncated: Boolean = false) = ConnFrame(active.toList(), closed, truncated)

    private fun state(vararg frames: ConnFrame): ConnsState =
        frames.foldIndexed(ConnsState()) { i, s, f -> ConnsReducer.reduce(s, ConnsAction.Frame(f, 10_000L + i)) }

    @Test fun aFrameReplacesTheActiveListAndMovesVanishedOnesToHistory() {
        val s = state(frame(row("a"), row("b")), frame(row("b"), row("c")))
        assertEquals(listOf("b", "c"), s.live.active.map { it.id })
        assertEquals(listOf("a"), s.live.closed.map { it.id })
        val closed = s.live.closed.single()
        assertEquals(ConnStatus.Closed, closed.status)
        assertEquals(10_001L, closed.endedAtMs)
    }

    @Test fun aBlockedConnectionStaysBlockedInHistory() {
        val s = state(frame(row("a", status = ConnStatus.Blocked)), frame())
        assertEquals(ConnStatus.Blocked, s.live.closed.single().status)
    }

    @Test fun truncatedFramesOnlyCloseWhatTheyName() {
        val s = state(frame(row("a"), row("b")), frame(row("b"), truncated = true, closed = emptySet()))
        assertTrue(s.live.closed.isEmpty())
        val named = state(frame(row("a"), row("b")), frame(row("b"), truncated = true, closed = setOf("a")))
        assertEquals(listOf("a"), named.live.closed.map { it.id })
    }

    @Test fun historyIsCappedAndNeverRepeatsAnId() {
        val many = (1..ConnsReducer.HISTORY_CAPACITY + 50).map { row("c$it") }
        val s = state(frame(*many.toTypedArray()), frame())
        assertEquals(ConnsReducer.HISTORY_CAPACITY, s.live.closed.size)
        val again = ConnsReducer.reduce(s, ConnsAction.Frame(frame(many.first()), 20_000)).let { ConnsReducer.reduce(it, ConnsAction.Frame(frame(), 21_000)) }
        assertEquals(ConnsReducer.HISTORY_CAPACITY, again.live.closed.size)
        assertEquals(again.live.closed.map { it.id }.toSet().size, again.live.closed.size)
    }

    @Test fun pausingFreezesWhatIsShownButKeepsReceiving() {
        var s = state(frame(row("a")))
        s = ConnsReducer.reduce(s, ConnsAction.Pause(true))
        s = ConnsReducer.reduce(s, ConnsAction.Frame(frame(row("a"), row("b")), 11_000))
        assertTrue(s.paused)
        assertEquals(listOf("a"), s.shown.active.map { it.id })
        assertEquals(listOf("a", "b"), s.live.active.map { it.id })
        s = ConnsReducer.reduce(s, ConnsAction.Pause(false))
        assertEquals(listOf("a", "b"), s.shown.active.map { it.id })
    }

    @Test fun resetClearsEverything() {
        val s = ConnsReducer.reduce(state(frame(row("a")), frame()), ConnsAction.Reset)
        assertEquals(ConnsState(), s)
    }

    @Test fun chipsFollowTheFinalOutboundOfTheChain() {
        assertEquals(ConnChip.Direct, row("a", outbound = "g-1", chain = listOf("g-1", "direct")).kind)
        assertEquals(ConnChip.Direct, row("a", outbound = "bypass").kind)
        assertEquals(ConnChip.Block, row("a", outbound = "block").kind)
        assertEquals(ConnChip.Block, row("a", status = ConnStatus.Blocked).kind)
        assertEquals(ConnChip.Proxy, row("a", outbound = "direct", chain = listOf("direct", "proxy")).kind)
    }

    @Test fun searchMatchesAddressChainRuleAndApp() {
        val list = listOf(row("a", dest = "Video.Example.com"), row("b", dest = "mail.test", process = "org.mail.app"))
        assertEquals(listOf("a"), ConnFilter.apply(list, ConnChip.All, " video ").map { it.id })
        assertEquals(listOf("b"), ConnFilter.apply(list, ConnChip.All, "ORG.MAIL").map { it.id })
        assertEquals(2, ConnFilter.apply(list, ConnChip.All, "").size)
        assertTrue(ConnFilter.apply(list, ConnChip.Direct, "").isEmpty())
    }

    @Test fun theCoreMessageIsMappedOnce() {
        val c = Connection(
            id = "x", startedAtMs = 5_000, status = Connection.Status.STATUS_CLOSED, outbound = "proxy", destination = "a.example:443",
            resolvedDestination = "a.example:443", process = "", uploadBytes = 7, downloadBytes = 9, network = "udp", chain = listOf("proxy"),
        )
        val r = ConnRow.from(c)
        assertEquals(ConnStatus.Closed, r.status)
        assertNull(r.resolved)
        assertNull(r.process)
        assertEquals(9, r.downloadBytes)
        assertEquals(3L, ConnRow.from(c.copy(endedAtMs = 8_000)).durationSeconds(99_999))
    }

    @Test fun backoffDoublesUpToThirtySeconds() {
        assertEquals(2_000L, StreamBackoff.next(1_000))
        assertEquals(30_000L, StreamBackoff.next(20_000))
        assertEquals(30_000L, StreamBackoff.next(30_000))
    }

    @Test fun aBrokenStreamIsReportedAndReopened() = runBlocking {
        var opens = 0
        val errors = mutableListOf<ErrorKind>()
        val frames = mutableListOf<Int>()
        try {
            StreamBackoff.run(
                open = {
                    opens += 1
                    if (opens == 1) flow<Int> { throw StreamFailure(ErrorKind.CoreUnavailable, RuntimeException("x")) } else flowOf(7)
                },
                onError = { errors += it },
                onFrame = { frames += it; if (frames.size == 1) throw CancellationException("stop") },
            )
        } catch (_: CancellationException) {
            // The screen leaving the composition ends the loop; here the frame handler does it.
        }
        assertEquals(listOf(ErrorKind.CoreUnavailable), errors)
        assertEquals(listOf(7), frames)
        assertFalse(opens < 2)
    }
}
