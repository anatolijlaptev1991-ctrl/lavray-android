// SPDX-License-Identifier: GPL-3.0-only
package io.github.anatolijlaptev1991_ctrl.lavray.ui

import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.channels.Channel
import kotlinx.coroutines.launch

/**
 * Calls on the service's Binder that must reach it in the order they were made (connect, reconnect, disconnect): each
 * call waits for [target] (the bound service), then runs, one after the other. Launching each call on its own coroutine
 * would let a quick tap and a Disconnect arrive in reverse order, and the last tap would lose.
 */
class SerialCalls<T>(scope: CoroutineScope, private val target: suspend () -> T) {
    private val queue = Channel<(T) -> Unit>(Channel.UNLIMITED)

    init {
        scope.launch {
            for (call in queue) {
                try {
                    call(target())
                } catch (e: CancellationException) {
                    throw e
                } catch (_: Exception) {
                    // A dead or unbound service: the call is lost, the next one is tried (the UI rebinds on its own).
                }
            }
        }
    }

    /** No more calls: the worker coroutine ends once the queued ones have run. */
    fun close() {
        queue.close()
    }

    /** Queues [call]; returns at once. */
    fun submit(call: (T) -> Unit) {
        queue.trySend(call)
    }
}
