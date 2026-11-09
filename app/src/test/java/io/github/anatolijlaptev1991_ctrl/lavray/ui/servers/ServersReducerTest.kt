// SPDX-License-Identifier: GPL-3.0-only
package io.github.anatolijlaptev1991_ctrl.lavray.ui.servers

import io.github.anatolijlaptev1991_ctrl.lavray.api.gen.ProfileType
import io.github.anatolijlaptev1991_ctrl.lavray.ui.core.ErrorKind
import io.github.anatolijlaptev1991_ctrl.lavray.ui.prefs.ServerSort
import io.github.anatolijlaptev1991_ctrl.lavray.ui.screens.servers.GroupInfo
import io.github.anatolijlaptev1991_ctrl.lavray.ui.screens.servers.GroupScope
import io.github.anatolijlaptev1991_ctrl.lavray.ui.screens.servers.LatencyCell
import io.github.anatolijlaptev1991_ctrl.lavray.ui.screens.servers.LatencyStatus
import io.github.anatolijlaptev1991_ctrl.lavray.ui.screens.servers.ListItem
import io.github.anatolijlaptev1991_ctrl.lavray.ui.screens.servers.ServerFilter
import io.github.anatolijlaptev1991_ctrl.lavray.ui.screens.servers.ServerRow
import io.github.anatolijlaptev1991_ctrl.lavray.ui.screens.servers.ServersAction
import io.github.anatolijlaptev1991_ctrl.lavray.ui.screens.servers.ServersReducer
import io.github.anatolijlaptev1991_ctrl.lavray.ui.screens.servers.ServersState
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNotNull
import org.junit.Assert.assertTrue
import org.junit.Test

class ServersReducerTest {
    private fun row(id: Long, group: Long = 1, name: String = "s$id", ms: Int = 0, status: LatencyStatus = LatencyStatus.Unknown, fav: Boolean = false) =
        ServerRow(
            id = id, groupId = group, name = name, address = "h$id.example", port = 443, type = ProfileType.PROFILE_TYPE_VLESS,
            favorite = fav, tags = emptyList(), unsupported = false, latency = LatencyCell(status, ms),
        )

    private val groups = listOf(GroupInfo(1, "Manual", subscription = false), GroupInfo(2, "Provider", subscription = true))

    private fun loaded(rows: List<ServerRow>, base: ServersState = ServersState()): ServersState =
        ServersReducer.reduce(base, ServersAction.Loaded(rows, groups))

    private fun ids(s: ServersState): List<Long> = s.items.filterIsInstance<ListItem.Row>().map { it.row.id }

    @Test
    fun loadingClearsTheSpinnerAndGroupsRows() {
        val s = loaded(listOf(row(1, 1), row(2, 2), row(3, 1)))
        assertFalse(s.loading)
        assertEquals(listOf("g1", "p1", "p3", "g2", "p2"), s.items.map { it.key })
        assertEquals(3, s.visibleCount)
    }

    @Test
    fun flatListWhenGroupingIsOffOrAScopeIsChosen() {
        val rows = listOf(row(1, 1), row(2, 2))
        val off = ServersReducer.reduce(loaded(rows), ServersAction.GroupBy(false))
        assertEquals(listOf("p1", "p2"), off.items.map { it.key })
        val scoped = ServersReducer.reduce(loaded(rows), ServersAction.Scope(GroupScope.One(2)))
        assertEquals(listOf(2L), ids(scoped))
        assertTrue(scoped.items.none { it is ListItem.Header })
    }

    @Test
    fun manualScopeExcludesSubscriptionGroups() {
        val s = ServersReducer.reduce(loaded(listOf(row(1, 1), row(2, 2))), ServersAction.Scope(GroupScope.Manual))
        assertEquals(listOf(1L), ids(s))
    }

    @Test
    fun searchMatchesNameAndAddressIgnoringCase() {
        val s0 = loaded(listOf(row(1, name = "Berlin"), row(2, name = "Tokyo")))
        assertEquals(listOf(1L), ids(ServersReducer.reduce(s0, ServersAction.Query("berl"))))
        assertEquals(listOf(2L), ids(ServersReducer.reduce(s0, ServersAction.Query("H2.EXAMPLE"))))
        assertEquals(emptyList<Long>(), ids(ServersReducer.reduce(s0, ServersAction.Query("nothing"))))
    }

    @Test
    fun fastFilterNeedsAnOkResultBelowTheThreshold() {
        val rows = listOf(
            row(1, ms = 120, status = LatencyStatus.Ok),
            row(2, ms = 900, status = LatencyStatus.Ok),
            row(3, status = LatencyStatus.Down),
            row(4),
        )
        val s = ServersReducer.reduce(loaded(rows), ServersAction.Filter(ServerFilter.Fast))
        assertEquals(listOf(1L), ids(s))
        assertEquals(listOf(3L), ids(ServersReducer.reduce(loaded(rows), ServersAction.Filter(ServerFilter.Down))))
    }

    @Test
    fun favouritesFilter() {
        val s = ServersReducer.reduce(loaded(listOf(row(1, fav = true), row(2))), ServersAction.Filter(ServerFilter.Favorites))
        assertEquals(listOf(1L), ids(s))
    }

    @Test
    fun latencySortPutsDownAndUntestedLast() {
        val rows = listOf(
            row(1, name = "a", status = LatencyStatus.Down),
            row(2, name = "b", ms = 300, status = LatencyStatus.Ok),
            row(3, name = "c"),
            row(4, name = "d", ms = 50, status = LatencyStatus.Ok),
        )
        val s = ServersReducer.reduce(
            ServersReducer.reduce(loaded(rows), ServersAction.GroupBy(false)),
            ServersAction.Sort(ServerSort.Latency),
        )
        assertEquals(listOf(4L, 2L, 1L, 3L), ids(s))
    }

    @Test
    fun nameSortIgnoresCase() {
        val s = ServersReducer.reduce(
            ServersReducer.reduce(loaded(listOf(row(1, name = "beta"), row(2, name = "Alpha"))), ServersAction.GroupBy(false)),
            ServersAction.Sort(ServerSort.Name),
        )
        assertEquals(listOf(2L, 1L), ids(s))
    }

    @Test
    fun selectionTogglesSelectsAllVisibleAndClears() {
        val s0 = ServersReducer.reduce(loaded(listOf(row(1), row(2), row(3))), ServersAction.Query("s"))
        val s1 = ServersReducer.reduce(s0, ServersAction.Toggle(2))
        assertTrue(s1.selecting)
        assertEquals(setOf(2L), s1.selection)
        assertEquals(setOf(1L, 2L, 3L), ServersReducer.reduce(s1, ServersAction.SelectAllVisible).selection)
        assertEquals(emptySet<Long>(), ServersReducer.reduce(s1, ServersAction.Toggle(2)).selection)
        assertFalse(ServersReducer.reduce(s1, ServersAction.ClearSelection).selecting)
    }

    @Test
    fun removalDropsRowsAndTheirSelection() {
        val s = ServersReducer.reduce(
            ServersReducer.reduce(loaded(listOf(row(1), row(2))), ServersAction.Toggle(1)),
            ServersAction.Removed(setOf(1L)),
        )
        assertEquals(listOf(2L), ids(s))
        assertTrue(s.selection.isEmpty())
    }

    @Test
    fun latencyBatchesDoNotReorderTheListWhileATestRuns() {
        val rows = listOf(row(1, name = "a"), row(2, name = "b"), row(3, name = "c"))
        var s = ServersReducer.reduce(
            ServersReducer.reduce(loaded(rows), ServersAction.GroupBy(false)),
            ServersAction.Sort(ServerSort.Latency),
        )
        s = ServersReducer.reduce(s, ServersAction.TestStarted(3))
        val before = ids(s)
        s = ServersReducer.reduce(s, ServersAction.Latency(mapOf(3L to LatencyCell(LatencyStatus.Ok, 10)), 1, 3))
        assertEquals(before, ids(s))
        assertEquals(10, (s.items.first { it.key == "p3" } as ListItem.Row).row.latency.ms)
        assertEquals(1, s.test.done)
        s = ServersReducer.reduce(s, ServersAction.TestFinished)
        assertFalse(s.test.running)
        assertEquals(3L, ids(s).first())
    }

    @Test
    fun failureKeepsRowsAndRecordsTheKind() {
        val s = ServersReducer.reduce(loaded(listOf(row(1))), ServersAction.Failed(ErrorKind.CoreUnavailable))
        assertEquals(ErrorKind.CoreUnavailable, s.error)
        assertEquals(1, s.rows.size)
        assertNotNull(s.items)
    }

    @Test
    fun latencyCellClassifiesCoreResults() {
        assertEquals(LatencyStatus.Ok, LatencyCell.from(80, null).status)
        assertEquals(LatencyStatus.Down, LatencyCell.from(null, "connection refused").status)
        assertEquals(LatencyStatus.NotApplicable, LatencyCell.from(null, "n/a: udp protocol").status)
        assertEquals(LatencyStatus.Unknown, LatencyCell.from(-1, null).status)
        assertEquals(LatencyStatus.Unknown, LatencyCell.from(null, null).status)
    }

    @Test
    fun twoThousandServersAreFilteredAndSortedQuickly() {
        val rows = (1L..2000L).map { row(it, group = if (it % 2 == 0L) 1 else 2, name = "server-${2000 - it}", ms = (it % 700).toInt() + 1, status = LatencyStatus.Ok) }
        val start = System.nanoTime()
        var s = loaded(rows)
        s = ServersReducer.reduce(s, ServersAction.Sort(ServerSort.Latency))
        s = ServersReducer.reduce(s, ServersAction.Query("server-1"))
        val ms = (System.nanoTime() - start) / 1_000_000
        assertTrue("took $ms ms", ms < 1500)
        assertTrue(s.visibleCount in 1..2000)
        val keys = s.items.map { it.key }
        assertEquals("keys must be unique for LazyColumn", keys.size, keys.toSet().size)
    }
}
