// SPDX-License-Identifier: GPL-3.0-only
package io.github.anatolijlaptev1991_ctrl.lavray.vpn

import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Test

class RetryBackoffTest {
    @Test
    fun scheduleThenFiveMinuteCap() {
        val b = RetryBackoff { 0.5 }
        assertEquals(listOf(5_000L, 15_000L, 45_000L, 120_000L, 300_000L, 300_000L), List(6) { b.nextDelayMs() })
    }

    @Test
    fun jitterStaysWithinTwentyPercent() {
        val low = RetryBackoff { 0.0 }
        val high = RetryBackoff { 0.999999 }
        assertEquals(4_000L, low.nextDelayMs())
        assertTrue(high.nextDelayMs() in 5_990L..6_000L)
        repeat(20) {
            val d = RetryBackoff().nextDelayMs()
            assertTrue(d in 4_000L..6_000L)
        }
    }

    @Test
    fun resetRestartsTheSchedule() {
        val b = RetryBackoff { 0.5 }
        b.nextDelayMs()
        b.nextDelayMs()
        b.reset()
        assertEquals(5_000L, b.nextDelayMs())
    }
}
