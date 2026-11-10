// SPDX-License-Identifier: GPL-3.0-only
package io.github.anatolijlaptev1991_ctrl.lavray.vpn

import android.content.ComponentName
import android.content.Context
import android.content.Intent
import android.content.ServiceConnection
import android.net.VpnService
import android.os.IBinder
import android.util.Log

/**
 * A binding of the VPN service to itself, under exactly the intent the system binds it with. It exists for one reason:
 * without it the system does NOT restart the service after its process dies while a tunnel is up, START_STICKY or not.
 *
 * What happens without it (seen on Android 16, read in ActiveServices.java): the dying process closes the tun device, the
 * system's VPN module hears "interface removed" and unbinds its own binding to the service BEFORE the activity manager has
 * handled the death. That unbind is the last client of the binding going away, so the activity manager calls into the
 * process to deliver onUnbind, the call fails (the process is dead), and the error path detaches the service record from
 * the process. When the death is handled a moment later the process has no services left: no restart is scheduled, the
 * tunnel is gone, and nothing tells the person.
 *
 * With a second client on the same binding (this one), the system's unbind is not the last client: nothing is called in
 * the dead process, the record stays attached, and the death is handled the ordinary way: a sticky restart after a
 * second, which [StartCommand.Resume] turns into a reconnect when the tunnel was wanted.
 *
 * The flags are 0 on purpose: without BIND_AUTO_CREATE the binding neither creates the service nor keeps it alive, so an
 * idle service still stops and its process still leaves. It relies on behaviour of the activity manager that no document
 * promises; where it does not help, nothing is worse than without it.
 */
internal class SelfBinding(private val service: VpnService) {
    private var bound = false

    private val connection = object : ServiceConnection {
        override fun onServiceConnected(name: ComponentName, binder: IBinder) = Unit

        override fun onServiceDisconnected(name: ComponentName) = Unit
    }

    /** Called from onCreate. A refusal is logged and otherwise ignored: the service works without it. */
    fun bind() {
        if (bound) return
        val intent = intent(service)
        bound = try {
            service.bindService(intent, connection, 0)
        } catch (e: RuntimeException) {
            Log.w(TAG, "self binding refused: " + e.javaClass.simpleName)
            false
        }
    }

    /** Called from onDestroy. */
    fun unbind() {
        if (!bound) return
        bound = false
        runCatching { service.unbindService(connection) }
    }

    companion object {
        private const val TAG = "LavrayVpn"

        /** Action and component only, as the system builds it: the two must compare equal to share one binding. */
        fun intent(context: Context): Intent =
            Intent(VpnService.SERVICE_INTERFACE).setClassName(context.packageName, LavrayVpnService::class.java.name)
    }
}
