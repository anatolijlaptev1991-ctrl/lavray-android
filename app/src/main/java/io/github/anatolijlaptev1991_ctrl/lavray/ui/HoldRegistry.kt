// SPDX-License-Identifier: GPL-3.0-only
package io.github.anatolijlaptev1991_ctrl.lavray.ui

import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.NonCancellable
import kotlinx.coroutines.SupervisorJob
import kotlinx.coroutines.async
import kotlinx.coroutines.withContext
import kotlinx.coroutines.withTimeoutOrNull
import java.util.concurrent.ConcurrentHashMap

/**
 * The busy holds the UI has asked the service for, each remembered together with the service object (the Binder) that
 * accepted it. A hold is ended on THAT Binder, bounded in time and independent of what the controller is bound to now:
 * after the UI left (unbind), the next bind may be minutes away, and waiting for it would keep the hold, the foreground
 * notification and the "busy" state of the screen until the hold's cap.
 */
class HoldRegistry<T : Any>(
    private val endTimeoutMs: Long = END_TIMEOUT_MS,
) {
    private val services = ConcurrentHashMap<String, T>()

    // Calls into a Binder block and cannot be interrupted: they run on a scope of their own and are abandoned on timeout.
    private val io = CoroutineScope(SupervisorJob() + Dispatchers.IO)

    /**
     * Asks [service] (through [call]) for the hold. Not cancellable once started, so a cancelled caller cannot leave a
     * hold in force that nobody knows about: it is recorded, and the caller's `finally` ends it by token.
     */
    suspend fun begin(token: String, service: T, call: (T) -> Boolean): Boolean = withContext(NonCancellable + Dispatchers.IO) {
        val held = call(service)
        if (held) services[token] = service
        held
    }

    /** Ends the hold of [token] on the service that accepted it; a hold that was never accepted is a no-op. Never throws. */
    suspend fun end(token: String, call: (T) -> Unit) {
        val service = services.remove(token) ?: return
        withContext(NonCancellable) {
            val ended = io.async { runCatching { call(service) } }
            // A dead or hung service takes its holds with it (or they end at their own deadline): do not wait for it.
            withTimeoutOrNull(endTimeoutMs) { ended.await() }
        }
    }

    fun isHeld(token: String): Boolean = services.containsKey(token)

    companion object {
        const val END_TIMEOUT_MS = 3_000L
    }
}
