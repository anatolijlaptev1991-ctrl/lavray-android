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
 *
 * The holder also remembers what the interface was built from (its spec). A new run of the core that asks for exactly
 * the same interface gets the one that is held ([reuseIf]) instead of a new one: Android replaces an interface by
 * moving the VPN's routing rules from the old device to the new one, and for a few hundred milliseconds in between the
 * apps' connections leave through the physical network. An interface that is not replaced has no such moment.
 */
internal class TunHolder<T : Closeable> {
    private var current: T? = null
    private var currentSpec: Any? = null
    private var generation = 0

    /**
     * Holds [next], closes the previous one, and returns the new generation. [spec] is everything the interface was
     * built from, compared with `==`; null means "never hand this one to another run".
     */
    @Synchronized
    fun replace(next: T, spec: Any? = null): Int {
        val old = current
        current = next
        currentSpec = spec
        generation++
        closeQuietly(old)
        return generation
    }

    /**
     * The held interface for a new run, if it was built from [spec]: [take] makes the run's own copy of it (under the
     * lock, so a release cannot close it in between) and the generation moves on, exactly as for a replacement, so a late
     * release decided against the previous run cannot close what the new run now uses. Null when nothing is held or the
     * held interface was built from something else.
     */
    @Synchronized
    fun <R> reuseIf(spec: Any, take: (T) -> R): Pair<R, Int>? {
        val held = current ?: return null
        if (currentSpec == null || currentSpec != spec) return null
        val taken = take(held)
        generation++
        return taken to generation
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
        currentSpec = null
        closeQuietly(old)
    }

    @Synchronized
    fun isHeld(): Boolean = current != null

    /** What the held interface was built from; null when nothing is held or that is not known. */
    @Synchronized
    fun heldSpec(): Any? = if (current != null) currentSpec else null

    private fun closeQuietly(resource: T?) {
        try {
            resource?.close()
        } catch (_: java.io.IOException) {
            // Closing a descriptor that is already gone changes nothing for the caller.
        }
    }
}
