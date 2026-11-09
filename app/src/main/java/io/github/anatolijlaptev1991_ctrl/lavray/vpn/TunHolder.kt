// SPDX-License-Identifier: GPL-3.0-only
package io.github.anatolijlaptev1991_ctrl.lavray.vpn

import java.io.Closeable

/**
 * Owns the descriptor VpnService.Builder.establish returned. The core duplicates it and
 * closes only its copy, so the interface (and the device's VPN, which blackholes every
 * route until it is gone) lives until the original is closed here. At most one is held:
 * a newer tunnel replaces and closes the older, and [release] closes the current one.
 */
internal class TunHolder<T : Closeable> {
    private var current: T? = null

    @Synchronized
    fun replace(next: T) {
        val old = current
        current = next
        closeQuietly(old)
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
