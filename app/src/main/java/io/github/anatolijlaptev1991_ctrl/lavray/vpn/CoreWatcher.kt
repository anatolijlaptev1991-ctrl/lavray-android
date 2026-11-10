// SPDX-License-Identifier: GPL-3.0-only
package io.github.anatolijlaptev1991_ctrl.lavray.vpn

import io.github.anatolijlaptev1991_ctrl.lavray.api.gen.CoreStatus
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.CoroutineStart
import kotlinx.coroutines.coroutineScope
import kotlinx.coroutines.delay
import kotlinx.coroutines.isActive
import kotlinx.coroutines.launch
import java.util.concurrent.atomic.AtomicBoolean
import kotlin.coroutines.coroutineContext

/**
 * Keeps the core's state stream open. A stream that ends or fails while the core is still the one in use would leave
 * the service believing the last state it saw (CONNECTED on a dead tunnel, never an IDLE to start the grace period),
 * so it is reopened after [retryMs] and the state is re-seeded with a status call each time.
 */
internal object CoreWatcher {
    suspend fun keep(
        api: CoreApi,
        onStatus: (CoreStatus) -> Unit,
        isCurrent: () -> Boolean,
        retryMs: Long = RETRY_MS,
    ) {
        while (coroutineContext.isActive && isCurrent()) {
            try {
                coroutineScope {
                    val gotEvent = AtomicBoolean(false)
                    // Subscribe first, then seed: a change between the two must not be lost.
                    val stream = launch(start = CoroutineStart.UNDISPATCHED) {
                        api.events().collect {
                            gotEvent.set(true)
                            onStatus(it)
                        }
                    }
                    // A seed that arrives after a newer event would overwrite it with an older state: only an unopposed one counts.
                    runCatching { api.status() }.onSuccess { if (!gotEvent.get()) onStatus(it) }
                    stream.join()
                }
            } catch (e: CancellationException) {
                throw e
            } catch (_: Exception) {
                // The core is gone or the socket broke: try again below.
            }
            if (!isCurrent()) return
            delay(retryMs)
        }
    }

    const val RETRY_MS = 2_000L
}
