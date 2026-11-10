// SPDX-License-Identifier: GPL-3.0-only
package io.github.anatolijlaptev1991_ctrl.lavray.vpn

/**
 * The :vpn process only exists to run the tunnel and, while the UI is open, to
 * serve it. When none of those holds the core stops and the process exits.
 * [transition] is true while an established tunnel is only being rebuilt (grace period after a core
 * drop, or the interface held as a blackhole): the service must stay in the foreground through it.
 * [busy] is true while the UI has asked to keep the process for a long operation that runs in the core (a backup
 * restore): leaving the app must not kill it.
 */
object ShutdownPolicy {
    fun shouldStop(
        vpnActive: Boolean,
        uiBound: Boolean,
        autoConnectPending: Boolean,
        transition: Boolean = false,
        busy: Boolean = false,
    ): Boolean = !vpnActive && !uiBound && !autoConnectPending && !transition && !busy

    /** Foreground state is dropped as soon as nothing is active, pending, in transition or busy, UI or not. */
    fun shouldLeaveForeground(vpnActive: Boolean, autoConnectPending: Boolean, transition: Boolean = false, busy: Boolean = false): Boolean =
        !vpnActive && !autoConnectPending && !transition && !busy

    /** The service is foreground whenever the tunnel is active, pending, in transition, or a long operation runs. */
    fun shouldBeForeground(vpnActive: Boolean, autoConnectPending: Boolean, transition: Boolean = false, busy: Boolean = false): Boolean =
        !shouldLeaveForeground(vpnActive, autoConnectPending, transition, busy)
}

/**
 * Whether a UI is bound, counted from the binder callbacks. onUnbind returns true for the UI binding, so the
 * system calls onRebind when the UI comes back to a live service; without it the system would hand out the
 * cached binder and the service would believe nobody is listening.
 */
class UiBindTracker {
    private var count = 0

    val bound: Boolean @Synchronized get() = count > 0

    @Synchronized fun onBind() { count++ }

    @Synchronized fun onRebind() { count++ }

    @Synchronized fun onUnbind() { if (count > 0) count-- }
}

/**
 * "Busy operation" holds set by the UI over AIDL (begin/end, by token). Every hold has a deadline, hard-capped at
 * [MAX_HOLD_MS], so a UI that dies without ending its hold cannot keep the process alive for ever.
 */
class BusyHold {
    private val deadlines = HashMap<String, Long>()

    /** Starts (or extends) the hold of [token] for [timeoutMs], clamped to 1 ms..[MAX_HOLD_MS]. Returns the deadline. */
    @Synchronized
    fun begin(nowMs: Long, token: String, timeoutMs: Long): Long {
        val deadline = nowMs + timeoutMs.coerceIn(1, MAX_HOLD_MS)
        deadlines[token] = deadline
        return deadline
    }

    /** Drops every hold (the keep-alive start was refused). */
    @Synchronized
    fun clear() {
        deadlines.clear()
    }

    @Synchronized
    fun end(token: String) {
        deadlines.remove(token)
    }

    /** True while any hold has a deadline after [nowMs]; expired holds are dropped. */
    @Synchronized
    fun isBusy(nowMs: Long): Boolean {
        deadlines.values.removeAll { it <= nowMs }
        return deadlines.isNotEmpty()
    }

    /** The earliest deadline still in force, or null: the service wakes up then to re-evaluate. */
    @Synchronized
    fun nextDeadline(nowMs: Long): Long? = deadlines.values.filter { it > nowMs }.minOrNull()

    companion object {
        const val MAX_HOLD_MS = 15 * 60_000L
    }
}
