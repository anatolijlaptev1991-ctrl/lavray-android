// SPDX-License-Identifier: GPL-3.0-only
package io.github.anatolijlaptev1991_ctrl.lavray.vpn

import android.app.Notification
import android.app.NotificationChannel
import android.app.NotificationManager
import android.app.PendingIntent
import android.content.Context
import android.content.Intent
import io.github.anatolijlaptev1991_ctrl.lavray.R
import io.github.anatolijlaptev1991_ctrl.lavray.ipc.VpnState
import io.github.anatolijlaptev1991_ctrl.lavray.ui.MainActivity

internal object VpnNotifications {
    const val ID_STATUS = 1
    private const val ID_ALERT = 2
    private const val CHANNEL_STATUS = "vpn_status"
    private const val CHANNEL_ALERTS = "vpn_alerts"

    private fun manager(context: Context) = context.getSystemService(NotificationManager::class.java)

    fun ensureChannels(context: Context) {
        val nm = manager(context)
        val status = NotificationChannel(CHANNEL_STATUS, context.getString(R.string.channel_status), NotificationManager.IMPORTANCE_LOW)
            .apply { setShowBadge(false) }
        val alerts = NotificationChannel(CHANNEL_ALERTS, context.getString(R.string.channel_alerts), NotificationManager.IMPORTANCE_DEFAULT)
        nm.createNotificationChannels(listOf(status, alerts))
    }

    /** The ongoing foreground notification: one text line and a Disconnect action. */
    fun status(context: Context, model: NotificationModel): Notification {
        val disconnect = Intent(context, LavrayVpnService::class.java).setAction(StartIntent.ACTION_DISCONNECT)
        val disconnectIntent = PendingIntent.getService(context, 0, disconnect, PendingIntent.FLAG_IMMUTABLE or PendingIntent.FLAG_UPDATE_CURRENT)
        return Notification.Builder(context, CHANNEL_STATUS)
            .setSmallIcon(R.drawable.ic_stat_vpn)
            .setContentTitle(context.getString(R.string.app_name))
            .setContentText(model.detail ?: context.getString(textFor(model.state)))
            .setContentIntent(openApp(context))
            .setOngoing(true)
            .setOnlyAlertOnce(true)
            .setShowWhen(false)
            .setCategory(Notification.CATEGORY_SERVICE)
            .addAction(
                Notification.Action.Builder(null, context.getString(R.string.notif_action_disconnect), disconnectIntent).build(),
            )
            .build()
    }

    fun postPermissionNeeded(context: Context) {
        ensureChannels(context)
        val n = Notification.Builder(context, CHANNEL_ALERTS)
            .setSmallIcon(R.drawable.ic_stat_vpn)
            .setContentTitle(context.getString(R.string.notif_permission_title))
            .setContentText(context.getString(R.string.notif_permission_text))
            .setContentIntent(openApp(context))
            .setAutoCancel(true)
            .setCategory(Notification.CATEGORY_ERROR)
            .build()
        manager(context).notify(ID_ALERT, n)
    }

    fun postFailure(context: Context, message: String?) {
        ensureChannels(context)
        val n = Notification.Builder(context, CHANNEL_ALERTS)
            .setSmallIcon(R.drawable.ic_stat_vpn)
            .setContentTitle(context.getString(R.string.notif_failure_title))
            .apply { if (!message.isNullOrBlank()) setContentText(message) }
            .setContentIntent(openApp(context))
            .setAutoCancel(true)
            .setCategory(Notification.CATEGORY_ERROR)
            .build()
        manager(context).notify(ID_ALERT, n)
    }

    fun cancelAlert(context: Context) = manager(context).cancel(ID_ALERT)

    private fun openApp(context: Context): PendingIntent =
        PendingIntent.getActivity(context, 1, Intent(context, MainActivity::class.java), PendingIntent.FLAG_IMMUTABLE)

    private fun textFor(state: VpnState): Int = when (state) {
        VpnState.IDLE -> R.string.notif_idle
        VpnState.CONNECTING -> R.string.notif_connecting
        VpnState.CONNECTED -> R.string.notif_connected
        VpnState.DISCONNECTING -> R.string.notif_disconnecting
        VpnState.WAITING_NETWORK -> R.string.notif_waiting_network
        VpnState.NEEDS_PERMISSION -> R.string.notif_permission_title
        VpnState.ERROR -> R.string.notif_error
    }
}
