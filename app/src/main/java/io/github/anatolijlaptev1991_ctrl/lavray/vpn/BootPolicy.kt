// SPDX-License-Identifier: GPL-3.0-only
package io.github.anatolijlaptev1991_ctrl.lavray.vpn

enum class BootAction { IGNORE, NOTIFY_PERMISSION, START_SERVICE }

/** What woke the receiver. */
enum class BootTrigger {
    /** BOOT_COMPLETED and the vendors' fast-boot equivalents (QUICKBOOT_POWERON). */
    BOOT,

    /** MY_PACKAGE_REPLACED: the app was updated, which killed the running tunnel. */
    UPDATE,
    ;

    companion object {
        const val ACTION_QUICKBOOT = "android.intent.action.QUICKBOOT_POWERON"
        const val ACTION_HTC_QUICKBOOT = "com.htc.intent.action.QUICKBOOT_POWERON"

        fun fromAction(action: String?): BootTrigger? = when (action) {
            "android.intent.action.BOOT_COMPLETED", ACTION_QUICKBOOT, ACTION_HTC_QUICKBOOT -> BOOT
            "android.intent.action.MY_PACKAGE_REPLACED" -> UPDATE
            else -> null
        }
    }
}

object BootPolicy {
    /** Boot completed: connect only if the user asked for it; lost VPN consent becomes a notification. */
    fun decide(autoConnectEnabled: Boolean, consentGranted: Boolean): BootAction = when {
        !autoConnectEnabled -> BootAction.IGNORE
        !consentGranted -> BootAction.NOTIFY_PERMISSION
        else -> BootAction.START_SERVICE
    }

    /**
     * Boot: the "connect at phone start" setting decides. Update: the tunnel comes back only if it was
     * wanted before (an update kills the process, it is not a reboot), whatever the boot setting says.
     */
    fun decide(trigger: BootTrigger, autoConnectEnabled: Boolean, wantConnected: Boolean, consentGranted: Boolean): BootAction {
        val wanted = when (trigger) {
            BootTrigger.BOOT -> autoConnectEnabled
            BootTrigger.UPDATE -> wantConnected
        }
        return decide(wanted, consentGranted)
    }
}
