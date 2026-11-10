// SPDX-License-Identifier: GPL-3.0-only
package io.github.anatolijlaptev1991_ctrl.lavray.ui.diag

import io.github.anatolijlaptev1991_ctrl.lavray.api.gen.StatsSnapshot
import io.github.anatolijlaptev1991_ctrl.lavray.api.gen.TagStat
import io.github.anatolijlaptev1991_ctrl.lavray.ui.screens.connection.TrafficReading
import io.github.anatolijlaptev1991_ctrl.lavray.ui.screens.diag.StatsMapper
import io.github.anatolijlaptev1991_ctrl.lavray.ui.screens.diag.StatsState
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test

/** E-18: when a shown session ends the stored totals have to be read again, and only then. */
class StatsSessionTest {
    private val frame = StatsSnapshot(tags = listOf(TagStat("proxy", 1.0, 2.0, 10, 20)))

    @Test fun aFrameMarksTheTotalsAsOutOfDateAfterTheSession() {
        val s = StatsMapper.afterFrame(StatsState(loaded = true), frame)
        assertTrue(s.sessionSeen)
        assertEquals(1, s.tags.size)
    }

    @Test fun theLivePartGoesWhenTheTunnelLeavesConnectedButTheTotalsStayOutOfDateUntilReloaded() {
        val live = StatsMapper.afterFrame(StatsState(loaded = true), frame)
        val ended = StatsMapper.afterEnd(live)
        // The core writes the end of the session after the stop completes: the first non-connected frame is too early to reload.
        assertTrue(ended.sessionSeen)
        assertEquals(TrafficReading.Empty, ended.session)
        assertTrue(ended.tags.isEmpty())
        assertTrue(ended.loaded)
    }

    @Test fun afterTheReloadNothingIsWaitingAnyMore() {
        val ended = StatsMapper.afterEnd(StatsMapper.afterFrame(StatsState(loaded = true), frame))
        assertFalse(StatsMapper.afterReload(ended).sessionSeen)
    }

    @Test fun aScreenThatNeverSawASessionHasNothingToReload() {
        assertFalse(StatsMapper.afterEnd(StatsState(loaded = true)).sessionSeen)
        assertFalse(StatsState().sessionSeen)
    }
}
