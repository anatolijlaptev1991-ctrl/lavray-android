// SPDX-License-Identifier: GPL-3.0-only
package io.github.anatolijlaptev1991_ctrl.lavray.vpn

/**
 * The :vpn process only exists to run the tunnel and, while the UI is open, to
 * serve it. When none of those holds the core stops and the process exits.
 * [transition] is true while an established tunnel is only being rebuilt (grace period after a core
 * drop, or the interface held as a blackhole): the service must stay in the foreground through it.
 */
object ShutdownPolicy {
    fun shouldStop(vpnActive: Boolean, uiBound: Boolean, autoConnectPending: Boolean, transition: Boolean = false): Boolean =
        !vpnActive && !uiBound && !autoConnectPending && !transition

    /** Foreground state is dropped as soon as nothing is active, pending or in transition, UI or not. */
    fun shouldLeaveForeground(vpnActive: Boolean, autoConnectPending: Boolean, transition: Boolean = false): Boolean =
        !vpnActive && !autoConnectPending && !transition

    /** The service is foreground whenever the tunnel is active, pending or in transition. */
    fun shouldBeForeground(vpnActive: Boolean, autoConnectPending: Boolean, transition: Boolean = false): Boolean =
        !shouldLeaveForeground(vpnActive, autoConnectPending, transition)
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
