// SPDX-License-Identifier: GPL-3.0-only
package io.github.anatolijlaptev1991_ctrl.lavray.ui.diag

import io.github.anatolijlaptev1991_ctrl.lavray.api.gen.GetTrafficSummaryResponse
import io.github.anatolijlaptev1991_ctrl.lavray.api.gen.GroupTrafficSummary
import io.github.anatolijlaptev1991_ctrl.lavray.api.gen.LogLevel
import io.github.anatolijlaptev1991_ctrl.lavray.api.gen.LogLine
import io.github.anatolijlaptev1991_ctrl.lavray.api.gen.StatsSnapshot
import io.github.anatolijlaptev1991_ctrl.lavray.api.gen.TagStat
import io.github.anatolijlaptev1991_ctrl.lavray.api.gen.TrafficCounters
import io.github.anatolijlaptev1991_ctrl.lavray.ui.screens.diag.LogBuffer
import io.github.anatolijlaptev1991_ctrl.lavray.ui.screens.diag.LogFilter
import io.github.anatolijlaptev1991_ctrl.lavray.ui.screens.diag.LogSeverity
import io.github.anatolijlaptev1991_ctrl.lavray.ui.screens.diag.StatsMapper
import io.github.anatolijlaptev1991_ctrl.lavray.ui.screens.diag.TrafficPair
import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Test

class LogAndStatsTests {
    private fun line(i: Int, level: LogLevel = LogLevel.LOG_LEVEL_INFO, source: String = "core", msg: String = "m$i") =
        LogLine(1_700_000_000_000L + i, level, source, msg)

    @Test fun levelsMapAndPanicIsAnError() {
        assertEquals(LogSeverity.Trace, LogSeverity.of(LogLevel.LOG_LEVEL_TRACE))
        assertEquals(LogSeverity.Error, LogSeverity.of(LogLevel.LOG_LEVEL_PANIC))
        assertEquals(LogSeverity.Info, LogSeverity.of(LogLevel.LOG_LEVEL_UNSPECIFIED))
    }

    @Test fun theBufferKeepsOnlyTheNewestLines() {
        val buffer = LogBuffer(5)
        (1..12).forEach { buffer.push(line(it)) }
        val shown = buffer.snapshot()
        assertEquals(listOf("m8", "m9", "m10", "m11", "m12"), shown.map { it.message })
        assertEquals((8L..12L).toList(), shown.map { it.seq })
        buffer.clear()
        assertTrue(buffer.snapshot().isEmpty())
    }

    @Test fun filterByLevelAndTextIgnoringCase() {
        val buffer = LogBuffer(10)
        buffer.push(line(1, LogLevel.LOG_LEVEL_DEBUG, msg = "dns query"))
        buffer.push(line(2, LogLevel.LOG_LEVEL_WARN, source = "Router", msg = "handshake failed"))
        buffer.push(line(3, LogLevel.LOG_LEVEL_ERROR, msg = "boom"))
        val all = buffer.snapshot()
        assertEquals(3, LogFilter.apply(all, LogSeverity.Trace, "").size)
        assertEquals(listOf("handshake failed", "boom"), LogFilter.apply(all, LogSeverity.Warn, "").map { it.message })
        assertEquals(listOf("handshake failed"), LogFilter.apply(all, LogSeverity.Trace, "ROUTER").map { it.message })
    }

    @Test fun exportTakesTheNewestLinesThatFitAndKeepsRedactedTextUntouched() {
        val buffer = LogBuffer(100)
        (1..50).forEach { buffer.push(line(it, msg = "token=•••• line $it")) }
        val all = buffer.snapshot()
        val full = LogFilter.export(all)
        assertEquals(50, full.lines().size)
        assertTrue(full.lines().all { "••••" in it })
        val limited = LogFilter.export(all, maxChars = 200)
        assertTrue(limited.length <= 200)
        assertTrue(limited.lines().last().endsWith("line 50"))
        assertTrue(limited.lines().first().contains(" INFO core: "))
        assertEquals(1, LogFilter.export(all, maxChars = 1).lines().size)
    }

    @Test fun trafficTotalsAddUpAcrossServers() {
        val total = StatsMapper.total(mapOf(1L to TrafficCounters(10, 100), 2L to TrafficCounters(5, 50)))
        assertEquals(TrafficPair(15, 150), total)
        assertEquals(165, total.total)
        assertEquals(TrafficPair(), StatsMapper.total(emptyMap()))
    }

    @Test fun groupsAreNamedSortedAndEmptyOnesDropped() {
        val summary = GetTrafficSummaryResponse(
            listOf(
                GroupTrafficSummary(1, 10, 20, 2, null, 5_000),
                GroupTrafficSummary(2, 0, 0, 0),
                GroupTrafficSummary(3, 500, 500, 1),
            ),
        )
        val lines = StatsMapper.groups(summary, mapOf(1L to "Manual"))
        assertEquals(listOf(3L, 1L), lines.map { it.groupId })
        assertEquals("Manual", lines[1].name)
        assertEquals(null, lines[0].name)
    }

    @Test fun finalFrameClearsTheOutboundList() {
        val tags = listOf(TagStat("proxy", 1.0, 2.0, 10, 20), TagStat("direct", 0.0, 0.0, 100, 200))
        assertEquals(listOf("direct", "proxy"), StatsMapper.tags(StatsSnapshot(tags = tags)).map { it.tag })
        assertTrue(StatsMapper.tags(StatsSnapshot(tags = tags, final = true)).isEmpty())
    }
}
