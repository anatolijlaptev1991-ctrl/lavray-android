// SPDX-License-Identifier: GPL-3.0-only
package io.github.anatolijlaptev1991_ctrl.lavray.vpn

import android.os.Bundle
import android.os.RemoteCallbackList
import io.github.anatolijlaptev1991_ctrl.lavray.api.CoreEndpoint
import io.github.anatolijlaptev1991_ctrl.lavray.ipc.IVpnObserver
import io.github.anatolijlaptev1991_ctrl.lavray.ipc.IVpnService
import io.github.anatolijlaptev1991_ctrl.lavray.ipc.VpnStatus
import io.github.anatolijlaptev1991_ctrl.lavray.ipc.VpnStatusWire

/** What the binder needs from the service; keeps the AIDL stub free of service internals. */
internal interface VpnBinderHost {
    fun endpoint(): CoreEndpoint
    fun connect(profileId: Long)
    fun reconnect(profileId: Long)
    fun disconnect()
    fun currentStatus(): VpnStatus
    var autoConnect: Boolean
    var appSelection: AppSelection
}

internal class VpnBinder(private val host: VpnBinderHost) : IVpnService.Stub() {
    private val observers = RemoteCallbackList<IVpnObserver>()

    override fun endpoint(): Bundle {
        val endpoint = try {
            host.endpoint()
        } catch (e: CoreStartException) {
            throw IllegalStateException(e.message)
        }
        return Bundle(2).apply {
            putString(KEY_PATH, endpoint.socketPath)
            putString(KEY_TOKEN, endpoint.token)
        }
    }

    override fun connect(profileId: Long) = host.connect(profileId)

    override fun reconnect(profileId: Long) = host.reconnect(profileId)

    override fun disconnect() = host.disconnect()

    override fun registerObserver(observer: IVpnObserver) {
        observers.register(observer)
        val status = host.currentStatus()
        runCatching { deliver(observer, status) }
    }

    override fun unregisterObserver(observer: IVpnObserver) {
        observers.unregister(observer)
    }

    override fun isAutoConnectEnabled(): Boolean = host.autoConnect

    override fun setAutoConnectEnabled(enabled: Boolean) {
        host.autoConnect = enabled
    }

    override fun getAppSelection(): String = host.appSelection.toJson()

    override fun setAppSelection(json: String) {
        host.appSelection = AppSelection.parse(json)
    }

    /**
     * Synchronized: the event loop and binder threads both publish, and RemoteCallbackList allows one broadcast at a time
     * (a second beginBroadcast throws).
     */
    @Synchronized
    fun broadcast(status: VpnStatus) {
        val n = observers.beginBroadcast()
        try {
            for (i in 0 until n) {
                runCatching { deliver(observers.getBroadcastItem(i), status) }
            }
        } finally {
            observers.finishBroadcast()
        }
    }

    private fun deliver(observer: IVpnObserver, status: VpnStatus) =
        observer.onStatus(status.state.code, status.error, VpnStatusWire.detailOf(status))

    fun kill() = observers.kill()

    companion object {
        const val KEY_PATH = "path"
        const val KEY_TOKEN = "token"
    }
}
