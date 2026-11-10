// SPDX-License-Identifier: GPL-3.0-only
package io.github.anatolijlaptev1991_ctrl.lavray.vpn

import org.junit.Assert.assertEquals
import org.junit.Assert.assertNull
import org.junit.Test

class ExitReasonsTest {
    // The numbers are the platform's (ApplicationExitInfo, developer.android.com): 15 is PACKAGE_STATE_CHANGE and 16 is
    // PACKAGE_UPDATED (both added in API 34); an earlier mapping had them the other way round.
    @Test
    fun codesFollowThePlatformConstants() {
        assertEquals("low_memory", ExitReasons.code(3))
        assertEquals("crash_native", ExitReasons.code(5))
        assertEquals("user_requested", ExitReasons.code(10))
        assertEquals("user_stopped", ExitReasons.code(11))
        assertEquals("other", ExitReasons.code(13))
        assertEquals("package_state_change", ExitReasons.code(15))
        assertEquals("package_updated", ExitReasons.code(16))
        assertEquals("memory_limiter", ExitReasons.code(17))
        assertEquals("anomaly", ExitReasons.code(18))
    }

    @Test
    fun unknownReasonIsNull() {
        assertNull(ExitReasons.code(0))
        assertNull(ExitReasons.code(999))
    }

    private val vpn = "io.example:vpn"

    @Test
    fun theNewestRecordOfTheVpnProcessWins() {
        val records = listOf(
            ExitRecord(vpn, 1_000, 3),
            ExitRecord(vpn, 3_000, 4),
            ExitRecord(vpn, 2_000, 10),
        )
        assertEquals(4, ExitReasons.latest(records, vpn, 0)!!.reason)
    }

    @Test
    fun otherProcessesOfThePackageAreIgnored() {
        val records = listOf(
            ExitRecord("io.example", 9_000, 10), // the UI process, swiped away
            ExitRecord(vpn, 1_000, 3),
        )
        assertEquals(3, ExitReasons.latest(records, vpn, 0)!!.reason)
        assertNull(ExitReasons.latest(records, "io.example:other", 0))
    }

    // An old record cannot explain a death of this boot: an old force-stop must not be taken for the latest exit.
    @Test
    fun recordsFromBeforeThisBootAreIgnored() {
        val records = listOf(ExitRecord(vpn, 500, 10))
        assertNull(ExitReasons.latest(records, vpn, notBeforeMs = 1_000))
        assertEquals(10, ExitReasons.latest(records, vpn, notBeforeMs = 500)!!.reason)
    }

    @Test
    fun noRecordsMeansUnknown() {
        assertNull(ExitReasons.latest(emptyList(), vpn, 0))
    }
}
