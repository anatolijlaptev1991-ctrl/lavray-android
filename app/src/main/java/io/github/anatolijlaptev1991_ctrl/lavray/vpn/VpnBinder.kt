// SPDX-License-Identifier: GPL-3.0-only
package io.github.anatolijlaptev1991_ctrl.lavray.vpn

import android.os.Bundle
import android.os.RemoteCallbackList
import io.github.anatolijlaptev1991_ctrl.lavray.api.CoreEndpoint
import io.github.anatolijlaptev1991_ctrl.lavray.ipc.IVpnObserver
import io.github.anatolijlaptev1991_ctrl.lavray.ipc.IVpnService
import io.github.anatolijlaptev1991_ctrl.lavray.ipc.VpnStatus

/** What the binder needs from the service; keeps the AIDL stub free of service internals. */
internal interface VpnBinderHost {
    fun endpoint(): CoreEndpoint
    fun connect(profileId: Long)
    fun disconnect()
    fun currentStatus(): VpnStatus
    var autoConnect: Boolean
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

    override fun disconnect() = host.disconnect()

    override fun registerObserver(observer: IVpnObserver) {
        observers.register(observer)
        val status = host.currentStatus()
        runCatching { observer.onStatus(status.state.code, status.error) }
    }

    override fun unregisterObserver(observer: IVpnObserver) {
        observers.unregister(observer)
    }

    override fun isAutoConnectEnabled(): Boolean = host.autoConnect

    override fun setAutoConnectEnabled(enabled: Boolean) {
        host.autoConnect = enabled
    }

    fun broadcast(status: VpnStatus) {
        val n = observers.beginBroadcast()
        try {
            for (i in 0 until n) {
                runCatching { observers.getBroadcastItem(i).onStatus(status.state.code, status.error) }
            }
        } finally {
            observers.finishBroadcast()
        }
    }

    fun kill() = observers.kill()

    companion object {
        const val KEY_PATH = "path"
        const val KEY_TOKEN = "token"
    }
}
