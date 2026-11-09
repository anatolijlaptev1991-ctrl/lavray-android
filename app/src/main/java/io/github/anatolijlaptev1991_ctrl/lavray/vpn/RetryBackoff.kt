// SPDX-License-Identifier: GPL-3.0-only
package io.github.anatolijlaptev1991_ctrl.lavray.vpn

/**
 * Retry delays 5 s, 15 s, 45 s, 2 min, then 5 min forever, each spread by +-20 %.
 * [random] returns a value in [0, 1) and is injectable for tests.
 */
class RetryBackoff(private val random: () -> Double = Math::random) {
    private var failures = 0

    fun nextDelayMs(): Long {
        val base = SCHEDULE_MS[minOf(failures, SCHEDULE_MS.lastIndex)]
        failures++
        val spread = 1.0 + (random() * 2.0 - 1.0) * JITTER
        return (base * spread).toLong()
    }

    fun reset() {
        failures = 0
    }

    companion object {
        val SCHEDULE_MS = longArrayOf(5_000, 15_000, 45_000, 120_000, 300_000)
        const val JITTER = 0.2
    }
}
