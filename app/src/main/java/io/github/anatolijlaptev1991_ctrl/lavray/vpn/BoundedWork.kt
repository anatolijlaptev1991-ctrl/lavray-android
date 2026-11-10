// SPDX-License-Identifier: GPL-3.0-only
package io.github.anatolijlaptev1991_ctrl.lavray.vpn

import java.util.concurrent.CountDownLatch
import java.util.concurrent.TimeUnit

/** Runs work that may hang (the Go core's stop) on a thread of its own, so the caller can give up on it. */
internal object BoundedStop {
    /** True when [stop] returned within [timeoutMs]; false when it is still running (the thread is left behind, daemon). */
    fun run(stop: () -> Unit, timeoutMs: Long): Boolean {
        val thread = Thread(stop, "lavray-stop").apply { isDaemon = true }
        thread.start()
        thread.join(timeoutMs)
        return !thread.isAlive
    }

    /**
     * What the process does after the service's stop: it leaves when nothing else lives in it, and always leaves when the
     * stop hung (a Go runtime that cannot be stopped would also block the next service instance's core).
     */
    fun shouldKillProcess(stopFinished: Boolean, otherServicesAlive: Boolean): Boolean = !stopFinished || !otherServicesAlive
}

/**
 * The answer of the service's own onStartCommand to a keep-alive start, handed to the Binder thread that asked for the
 * hold: whether the system really let the service enter the foreground. Without it `beginBusy` would report "in force"
 * before the fact is known.
 */
internal class ForegroundConfirm {
    @Volatile private var latch: CountDownLatch? = null

    @Volatile private var result = true

    /** A keep-alive start is about to be issued. */
    fun arm() {
        result = true
        latch = CountDownLatch(1)
    }

    /** The start was handled: [entered] is whether the foreground state was entered. */
    fun report(entered: Boolean) {
        result = entered
        latch?.countDown()
    }

    /**
     * Waits for [report]. A start that is never answered within [timeoutMs] (a busy main thread) counts as in force: the
     * system refuses synchronously or in onStartCommand, so silence is the rare case, and the hold ends by its own
     * deadline whatever happens.
     */
    fun await(timeoutMs: Long): Boolean {
        val l = latch ?: return true
        val answered = l.await(timeoutMs, TimeUnit.MILLISECONDS)
        return if (answered) result else true
    }
}
