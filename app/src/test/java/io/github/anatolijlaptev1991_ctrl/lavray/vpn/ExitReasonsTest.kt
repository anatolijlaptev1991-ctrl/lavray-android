// SPDX-License-Identifier: GPL-3.0-only
package io.github.anatolijlaptev1991_ctrl.lavray.vpn

import org.junit.Assert.assertEquals
import org.junit.Assert.assertNull
import org.junit.Test

class ExitReasonsTest {
    @Test
    fun knownReasonsHaveStableCodes() {
        assertEquals("low_memory", ExitReasons.code(3))
        assertEquals("crash_native", ExitReasons.code(5))
        assertEquals("user_stopped", ExitReasons.code(11))
        assertEquals("package_updated", ExitReasons.code(15))
    }

    @Test
    fun unknownReasonIsNull() {
        assertNull(ExitReasons.code(0))
        assertNull(ExitReasons.code(13))
        assertNull(ExitReasons.code(999))
    }
}
