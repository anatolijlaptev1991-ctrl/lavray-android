// SPDX-License-Identifier: GPL-3.0-only
package io.github.anatolijlaptev1991_ctrl.lavray.ui

import kotlinx.coroutines.CompletableDeferred

/**
 * Hands out "the bound service" to callers that may have to wait for it. When the binding goes away (unbind, the
 * service's process died, the binding itself died) the gate is [replace]d: everyone still waiting on the old binding is
 * failed at once with an ordinary exception, never left hanging and never cancelled (a cancellation would end the
 * coroutine that runs the queued commands). A command queued for a dead binding is thereby dropped, not replayed hours
 * later on a binding it was never meant for.
 */
class ServiceGate<T> {
    @Volatile private var ready = CompletableDeferred<T>()

    /** The service of the current binding; waits until it is connected, or fails when the binding is replaced first. */
    suspend fun await(): T = ready.await()

    /** The deferred of the current binding: a connection that arrives completes this one, not whatever is current later. */
    fun capture(): CompletableDeferred<T> = ready

    /** A new binding begins: waiters on the old one are failed. */
    fun replace() {
        val old = ready
        ready = CompletableDeferred()
        old.completeExceptionally(IllegalStateException("the service binding was replaced"))
    }
}
