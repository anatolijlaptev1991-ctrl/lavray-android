// SPDX-License-Identifier: GPL-3.0-only
package io.github.anatolijlaptev1991_ctrl.lavray.ui.screens.diag

import io.github.anatolijlaptev1991_ctrl.lavray.api.gen.SubscribeLogsRequest
import io.github.anatolijlaptev1991_ctrl.lavray.api.gen.logSubscribe
import io.github.anatolijlaptev1991_ctrl.lavray.ui.core.CoreSession
import io.github.anatolijlaptev1991_ctrl.lavray.ui.core.ErrorKind
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.Job
import kotlinx.coroutines.coroutineScope
import kotlinx.coroutines.delay
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.launch
import kotlinx.coroutines.withContext

/**
 * The core log: one subscription that sends the last lines and then the live ones (desktop useLogBuffer.ts). Lines
 * go into a capped ring; the screen gets an immutable snapshot at most every [FLUSH_MS], so a burst of lines is one
 * recomposition. The stream exists only while [stream] runs, which the screen ties to its own visibility.
 */
class LogModel(private val session: CoreSession) {
    private val lock = Any()
    private val buffer = LogBuffer()
    private val mutableLines = MutableStateFlow<List<LogEntry>>(emptyList())
    private val mutableError = MutableStateFlow<ErrorKind?>(null)

    val lines: StateFlow<List<LogEntry>> = mutableLines.asStateFlow()
    val streamError: StateFlow<ErrorKind?> = mutableError.asStateFlow()

    suspend fun stream() = withContext(Dispatchers.Default) {
        coroutineScope {
            var timer: Job? = null
            var lastFlush = 0L
            var first = true
            fun flush() {
                lastFlush = System.nanoTime()
                val snap = synchronized(lock) { buffer.snapshot() }
                mutableLines.value = snap
            }
            StreamBackoff.run(
                // After a break the core would replay its tail and duplicate lines: only the first open asks for it.
                open = {
                    val tail = if (first) TAIL_LINES else null
                    first = false
                    session.stream { logSubscribe(SubscribeLogsRequest(tailLines = tail)) }
                },
                onError = { mutableError.value = it },
                onFrame = { line ->
                    mutableError.value = null
                    synchronized(lock) { buffer.push(line) }
                    val sinceMs = (System.nanoTime() - lastFlush) / NANOS_PER_MS
                    if (sinceMs >= FLUSH_MS) {
                        flush()
                    } else if (timer?.isActive != true) {
                        timer = launch {
                            delay(FLUSH_MS - sinceMs)
                            flush()
                        }
                    }
                },
            )
        }
    }

    /** Empties the window (the core keeps its own history). */
    fun clear() {
        synchronized(lock) { buffer.clear() }
        mutableLines.value = emptyList()
    }

    companion object {
        const val TAIL_LINES = 200
        const val FLUSH_MS = 100L
        private const val NANOS_PER_MS = 1_000_000L
    }
}
