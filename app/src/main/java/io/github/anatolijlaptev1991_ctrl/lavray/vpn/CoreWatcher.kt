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
 * so it is reopened and the state is re-seeded with a status call each time. A stream that delivers nothing before it
 * ends again is retried with a growing pause ([retryMs] doubling up to [maxRetryMs]), so a core that closes it at once
 * does not make the service re-seed and publish every couple of seconds. The pause is reset only by a stream that stayed
 * open for [healthyMs]: one event and a close, over and over, is the same churn.
 */
internal object CoreWatcher {
    suspend fun keep(
        api: CoreApi,
        onStatus: (CoreStatus) -> Unit,
        isCurrent: () -> Boolean,
        retryMs: Long = RETRY_MS,
        maxRetryMs: Long = MAX_RETRY_MS,
        sleep: suspend (Long) -> Unit = { delay(it) },
        healthyMs: Long = HEALTHY_MS,
        now: () -> Long = { System.nanoTime() / 1_000_000 },
    ) {
        var pause = retryMs
        while (coroutineContext.isActive && isCurrent()) {
            val delivered = AtomicBoolean(false)
            val openedAt = now()
            try {
                coroutineScope {
                    // Subscribe first, then seed: a change between the two must not be lost.
                    val stream = launch(start = CoroutineStart.UNDISPATCHED) {
                        api.events().collect {
                            delivered.set(true)
                            onStatus(it)
                        }
                    }
                    // A seed that arrives after a newer event would overwrite it with an older state: only an unopposed one counts.
                    runCatching { api.status() }.onSuccess { if (!delivered.get()) onStatus(it) }
                    stream.join()
                }
            } catch (e: CancellationException) {
                throw e
            } catch (_: Exception) {
                // The core is gone or the socket broke: try again below.
            }
            if (!isCurrent()) return
            val healthy = now() - openedAt >= healthyMs
            val wait = if (healthy) retryMs else pause
            pause = if (healthy) retryMs else minOf(pause * 2, maxRetryMs)
            sleep(wait)
        }
    }

    const val RETRY_MS = 2_000L
    const val MAX_RETRY_MS = 30_000L
    const val HEALTHY_MS = 30_000L
}
