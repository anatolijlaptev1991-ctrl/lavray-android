// SPDX-License-Identifier: GPL-3.0-only
package io.github.anatolijlaptev1991_ctrl.lavray.vpn

import android.app.NotificationManager
import android.app.Service
import android.content.BroadcastReceiver
import android.content.Context
import android.content.Intent
import android.content.IntentFilter
import android.content.pm.ServiceInfo
import android.os.Build
import android.os.PowerManager
import android.util.Log
import io.github.anatolijlaptev1991_ctrl.lavray.ipc.VpnState

/**
 * The foreground state of the tunnel service and its ongoing notification. Touched only by the event loop, except
 * [offer], which is safe from any thread through the gate.
 */
internal class ServiceForeground(private val service: Service) {
    private val gate = NotificationGate()

    var isForeground = false
        private set

    private val screenOn = object : BroadcastReceiver() {
        override fun onReceive(context: Context, intent: Intent) {
            gate.onScreenOn()?.let { post(it) }
        }
    }

    fun registerScreenReceiver() {
        val filter = IntentFilter(Intent.ACTION_SCREEN_ON)
        if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.TIRAMISU) {
            service.registerReceiver(screenOn, filter, Context.RECEIVER_NOT_EXPORTED)
        } else {
            service.registerReceiver(screenOn, filter)
        }
    }

    fun unregisterScreenReceiver() {
        runCatching { service.unregisterReceiver(screenOn) }
    }

    /**
     * Promotes the service. A false result means the system refused (restricted background start); the caller decides
     * whether that is fatal. A start that was promised to become foreground must do so within seconds.
     */
    fun enter(state: VpnState = VpnState.CONNECTING): Boolean {
        if (isForeground) return true
        val model = gate.forcePost(NotificationModel(state))
        val notification = VpnNotifications.status(service, model)
        return try {
            if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.UPSIDE_DOWN_CAKE) {
                service.startForeground(VpnNotifications.ID_STATUS, notification, ServiceInfo.FOREGROUND_SERVICE_TYPE_SPECIAL_USE)
            } else {
                service.startForeground(VpnNotifications.ID_STATUS, notification)
            }
            isForeground = true
            true
        } catch (e: RuntimeException) {
            Log.w(TAG, "startForeground refused: ${e.javaClass.simpleName}")
            false
        }
    }

    fun leave() {
        if (!isForeground) return
        service.stopForeground(Service.STOP_FOREGROUND_REMOVE)
        isForeground = false
    }

    // While the screen is off the update is held back and released by the screen-on receiver.
    fun offer(model: NotificationModel) {
        if (!isForeground) return
        val interactive = service.getSystemService(PowerManager::class.java).isInteractive
        gate.offer(model, interactive)?.let { post(it) }
    }

    private fun post(model: NotificationModel) {
        if (!isForeground) return
        service.getSystemService(NotificationManager::class.java)
            .notify(VpnNotifications.ID_STATUS, VpnNotifications.status(service, model))
    }

    private companion object {
        const val TAG = "LavrayVpn"
    }
}
