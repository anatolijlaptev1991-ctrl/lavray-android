// SPDX-License-Identifier: GPL-3.0-only
package io.github.anatolijlaptev1991_ctrl.lavray.vpn

import android.content.BroadcastReceiver
import android.content.Context
import android.content.Intent
import android.net.VpnService

/**
 * BOOT_COMPLETED, in the :vpn process. Does one preference read and either starts the
 * service, posts the "permission needed" notice, or does nothing. The service re-checks
 * consent itself.
 */
class BootReceiver : BroadcastReceiver() {
    override fun onReceive(context: Context, intent: Intent) {
        if (intent.action != Intent.ACTION_BOOT_COMPLETED) return
        val enabled = VpnPrefs(context).autoConnect
        // Calling prepare() from a receiver is not described by the docs; treated as "no consent" on failure.
        val consent = enabled && runCatching { VpnService.prepare(context) == null }.getOrDefault(false)
        when (BootPolicy.decide(enabled, consent)) {
            BootAction.IGNORE -> Unit
            BootAction.NOTIFY_PERMISSION -> VpnNotifications.postPermissionNeeded(context)
            BootAction.START_SERVICE -> context.startForegroundService(
                Intent(context, LavrayVpnService::class.java).putExtra(StartIntent.EXTRA_SOURCE, StartSource.BOOT.wire),
            )
        }
    }
}
