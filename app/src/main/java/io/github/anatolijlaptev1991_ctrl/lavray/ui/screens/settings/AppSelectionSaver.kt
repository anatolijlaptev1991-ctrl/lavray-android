// SPDX-License-Identifier: GPL-3.0-only
package io.github.anatolijlaptev1991_ctrl.lavray.ui.screens.settings

import io.github.anatolijlaptev1991_ctrl.lavray.vpn.AppMode
import io.github.anatolijlaptev1991_ctrl.lavray.vpn.AppSelection
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.CoroutineStart
import kotlinx.coroutines.DelicateCoroutinesApi
import kotlinx.coroutines.Deferred
import kotlinx.coroutines.async
import kotlinx.coroutines.sync.Mutex
import kotlinx.coroutines.sync.withLock
import kotlinx.coroutines.withTimeoutOrNull

/** What the VPN service builds from a choice; two choices with the same value build the same tunnel. */
data class TunnelChoice(val mode: AppMode, val packages: Set<String>, val viaVpn: Set<String>, val direct: Set<String>)

/**
 * What the VPN service does with a choice: "only the chosen" with nothing chosen is "all apps", and "all" has no list.
 * The "always through VPN" and "always direct" lists count in every mode.
 */
fun AppSelection.canonical(): TunnelChoice =
    TunnelChoice(effectiveMode, if (effectiveMode == AppMode.All) emptySet() else packages, viaVpn, direct)

/**
 * Reads and saves the per-app choice for the whole app. Everything goes through one lock, so a read never interleaves
 * with a write, writes are serial, and the newest choice wins: [submit] records the choice on the CALLER's thread, so the
 * order of taps is the order of the calls however the coroutines are scheduled. The work runs on the app's scope, not
 * the screen's, so leaving the picker right after a tap does not cancel it. Every step is bounded by [timeoutMs]: a
 * service that never answers cannot hold the lock for ever.
 *
 * [onSaved] gets the choice that was stored before and the one stored now (the first is null when it could not be read).
 */
class AppSelectionSaver(
    private val read: suspend () -> AppSelection?,
    private val write: suspend (AppSelection) -> Boolean,
    private val onSaved: (before: AppSelection?, after: AppSelection) -> Unit,
    private val timeoutMs: Long = DEFAULT_TIMEOUT_MS,
) {
    sealed interface Result {
        data class Saved(val selection: AppSelection) : Result

        /** A newer choice was submitted meanwhile; the call that handles it reports the outcome. */
        data object Superseded : Result

        /** The write failed; [stored] is what the service has now (null when that could not be read either). */
        data class Failed(val stored: AppSelection?) : Result
    }

    private val lock = Mutex()
    private var latest: AppSelection? = null

    @OptIn(DelicateCoroutinesApi::class)
    fun submit(selection: AppSelection, scope: CoroutineScope): Deferred<Result> {
        synchronized(this) { latest = selection }
        // ATOMIC: a job cancelled before it started must still take the choice that was recorded for it.
        return scope.async(start = CoroutineStart.ATOMIC) { drain() }
    }

    private suspend fun drain(): Result = lock.withLock {
        val next = synchronized(this) { latest.also { latest = null } } ?: return@withLock Result.Superseded
        val before = withTimeoutOrNull(timeoutMs) { read() }
        val ok = withTimeoutOrNull(timeoutMs) { write(next) } == true
        if (ok) {
            onSaved(before, next)
            Result.Saved(next)
        } else {
            Result.Failed(withTimeoutOrNull(timeoutMs) { read() })
        }
    }

    /** The stored choice, read under the same lock as the writes; null when it cannot be read. */
    suspend fun current(): AppSelection? = lock.withLock { withTimeoutOrNull(timeoutMs) { read() } }

    companion object {
        const val DEFAULT_TIMEOUT_MS = 10_000L
    }
}
