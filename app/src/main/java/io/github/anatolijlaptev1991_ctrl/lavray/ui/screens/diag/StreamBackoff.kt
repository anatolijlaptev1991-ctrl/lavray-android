// SPDX-License-Identifier: GPL-3.0-only
package io.github.anatolijlaptev1991_ctrl.lavray.ui.screens.diag

import io.github.anatolijlaptev1991_ctrl.lavray.ui.core.ErrorKind
import io.github.anatolijlaptev1991_ctrl.lavray.ui.core.StreamFailure
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.delay
import kotlinx.coroutines.flow.Flow

/**
 * Reads a core stream and reopens it after a break, with a growing pause (desktop lib/streamWatch.ts):
 * 1 s, 2 s, 4 s ... at most 30 s. A received frame resets the pause. This is reconnection, not polling: while
 * the stream is healthy nothing wakes up between frames. It returns only when the caller's coroutine is cancelled,
 * which is how the screen stops the stream when it is no longer visible.
 */
object StreamBackoff {
    const val START_MS = 1_000L
    const val CAP_MS = 30_000L

    fun next(ms: Long): Long = (ms * 2).coerceAtMost(CAP_MS)

    suspend fun <T> run(
        open: () -> Flow<T>,
        onError: (ErrorKind) -> Unit,
        onFrame: suspend (T) -> Unit,
    ) {
        var pause = START_MS
        while (true) {
            try {
                open().collect { frame ->
                    pause = START_MS
                    onFrame(frame)
                }
                // The core holds a subscription open while it runs: a clean end is a lost connection.
                onError(ErrorKind.CoreUnavailable)
            } catch (e: CancellationException) {
                throw e
            } catch (e: StreamFailure) {
                onError(e.kind)
            }
            delay(pause)
            pause = next(pause)
        }
    }
}
