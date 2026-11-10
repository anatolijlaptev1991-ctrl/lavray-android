// SPDX-License-Identifier: GPL-3.0-only
package io.github.anatolijlaptev1991_ctrl.lavray.ui.diag

import io.github.anatolijlaptev1991_ctrl.lavray.ui.screens.diag.ChecksState
import io.github.anatolijlaptev1991_ctrl.lavray.ui.screens.diag.CheckSummary
import io.github.anatolijlaptev1991_ctrl.lavray.ui.screens.diag.IpReading
import io.github.anatolijlaptev1991_ctrl.lavray.ui.screens.diag.LeakVerdict
import org.junit.Assert.assertEquals
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test

/** E-10: nothing measured through the tunnel survives the tunnel. */
class ChecksResetTest {
    private val shown = ChecksState(
        summary = CheckSummary.AllGood,
        direct = IpReading.Value("198.51.100.7", false),
        tunnel = IpReading.Value("203.0.113.9", false),
        leak = LeakVerdict.of(io.github.anatolijlaptev1991_ctrl.lavray.api.gen.LeakReport()),
        leakRunning = true,
    )

    @Test fun theTunnelAddressTheVerdictAndTheSelfCheckAreDropped() {
        val after = shown.withoutTunnelResults()
        assertEquals(IpReading.Idle, after.tunnel)
        assertNull(after.leak)
        assertNull(after.summary)
        assertTrue(after.rows.isEmpty())
        assertEquals(false, after.leakRunning)
    }

    @Test fun aDirectProbeThatWasRunningIsNotLeftRunningWhenItsJobIsCancelled() {
        val running = shown.copy(direct = IpReading.Loading)
        assertEquals(IpReading.Idle, running.withoutTunnelResults().direct)
    }

    @Test fun theDirectAddressStaysBecauseItDoesNotDependOnTheTunnel() {
        assertEquals(IpReading.Value("198.51.100.7", false), shown.withoutTunnelResults().direct)
    }
}
