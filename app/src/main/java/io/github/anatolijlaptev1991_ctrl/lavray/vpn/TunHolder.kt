// SPDX-License-Identifier: GPL-3.0-only
package io.github.anatolijlaptev1991_ctrl.lavray.vpn

import java.io.Closeable

/**
 * Owns the descriptor VpnService.Builder.establish returned. The core duplicates it and
 * closes only its copy, so the interface (and the device's VPN, which blackholes every
 * route until it is gone) lives until the original is closed here. At most one is held:
 * a newer tunnel replaces and closes the older, and [release] closes the current one.
 *
 * Every replacement gets a generation. A release that was decided against an older generation
 * ([releaseIfCurrent]) is ignored, so a late event of a previous run can never close the interface
 * of the run that replaced it.
 */
internal class TunHolder<T : Closeable> {
    private var current: T? = null
    private var generation = 0

    /** Holds [next], closes the previous one, and returns the new generation. */
    @Synchronized
    fun replace(next: T): Int {
        val old = current
        current = next
        generation++
        closeQuietly(old)
        return generation
    }

    /** The generation of the interface that is held now (0 before the first). */
    @Synchronized
    fun currentGeneration(): Int = generation

    /** Closes the held interface only if it is still the one of [expected]; true when it was closed. */
    @Synchronized
    fun releaseIfCurrent(expected: Int): Boolean {
        if (generation != expected || current == null) return false
        release()
        return true
    }

    @Synchronized
    fun release() {
        val old = current
        current = null
        closeQuietly(old)
    }

    @Synchronized
    fun isHeld(): Boolean = current != null

    private fun closeQuietly(resource: T?) {
        try {
            resource?.close()
        } catch (_: java.io.IOException) {
            // Closing a descriptor that is already gone changes nothing for the caller.
        }
    }
}
