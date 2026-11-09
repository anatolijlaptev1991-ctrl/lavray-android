// SPDX-License-Identifier: GPL-3.0-only
package io.github.anatolijlaptev1991_ctrl.lavray.vpn

/**
 * The :vpn process only exists to run the tunnel and, while the UI is open, to
 * serve it. When none of those holds the core stops and the process exits.
 */
object ShutdownPolicy {
    fun shouldStop(vpnActive: Boolean, uiBound: Boolean, autoConnectPending: Boolean): Boolean =
        !vpnActive && !uiBound && !autoConnectPending

    /** Foreground state is dropped as soon as nothing is active or pending, UI or not. */
    fun shouldLeaveForeground(vpnActive: Boolean, autoConnectPending: Boolean): Boolean =
        !vpnActive && !autoConnectPending
}
