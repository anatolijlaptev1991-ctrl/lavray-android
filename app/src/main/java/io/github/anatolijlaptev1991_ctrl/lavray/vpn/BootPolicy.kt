// SPDX-License-Identifier: GPL-3.0-only
package io.github.anatolijlaptev1991_ctrl.lavray.vpn

enum class BootAction { IGNORE, NOTIFY_PERMISSION, START_SERVICE }

object BootPolicy {
    /** Boot completed: connect only if the user asked for it; lost VPN consent becomes a notification. */
    fun decide(autoConnectEnabled: Boolean, consentGranted: Boolean): BootAction = when {
        !autoConnectEnabled -> BootAction.IGNORE
        !consentGranted -> BootAction.NOTIFY_PERMISSION
        else -> BootAction.START_SERVICE
    }
}
