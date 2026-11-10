// SPDX-License-Identifier: GPL-3.0-only
package io.github.anatolijlaptev1991_ctrl.lavray.vpn

import android.os.ParcelFileDescriptor
import java.io.Closeable
import java.io.IOException

/** A descriptor that can be duplicated and detached; the platform's ParcelFileDescriptor, or a fake in tests. */
internal interface DupableFd : Closeable {
    fun dupFd(): DupableFd

    /** Gives up ownership and returns the raw descriptor number. */
    fun detach(): Int
}

internal class PfdFd(val pfd: ParcelFileDescriptor) : DupableFd {
    override fun dupFd(): DupableFd = PfdFd(pfd.dup())
    override fun detach(): Int = pfd.detachFd()
    override fun close() = pfd.close()
}

/**
 * Hands the built interface to the core: Kotlin keeps [built] in the holder (the interface lives as long as it is
 * held), the core gets an independent duplicate it owns. Every failure on the way closes what would otherwise leak.
 */
internal class TunHandoff<T : DupableFd>(private val holder: TunHolder<T>) {
    /**
     * [afterReplace] runs once the holder has [built]; if it throws, the duplicate is closed and the error propagates
     * (the interface stays held: the caller's connect fails and its own release decision applies).
     */
    fun hand(built: T, afterReplace: (generation: Int) -> Unit): Int {
        val forCore = try {
            built.dupFd()
        } catch (e: IOException) {
            closeQuietly(built)
            throw e
        }
        val generation = holder.replace(built)
        try {
            afterReplace(generation)
        } catch (e: Throwable) {
            closeQuietly(forCore)
            throw e
        }
        return forCore.detach()
    }

    private fun closeQuietly(fd: Closeable) {
        try {
            fd.close()
        } catch (_: IOException) {
            // Already gone: nothing left to release.
        }
    }
}
