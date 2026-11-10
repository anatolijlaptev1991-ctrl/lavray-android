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
    val autoConnect: Boolean

    /** Writes the setting and aligns the boot receiver; false when it is not in force afterwards. */
    fun setAutoConnect(enabled: Boolean): Boolean
    var appSelection: AppSelection
    fun beginBusy(token: String, timeoutMs: Long)
    fun endBusy(token: String)
}

internal class VpnBinder(private val host: VpnBinderHost) : IVpnService.Stub() {
    private val observers = RemoteCallbackList<IVpnObserver>()

    /** The newest status ever broadcast; a new observer gets it under the same lock, so it can never overtake a newer one. */
    private var latest: VpnStatus = VpnStatus.IDLE

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

    @Synchronized
    override fun registerObserver(observer: IVpnObserver) {
        observers.register(observer)
        runCatching { deliver(observer, latest) }
    }

    override fun unregisterObserver(observer: IVpnObserver) {
        observers.unregister(observer)
    }

    override fun isAutoConnectEnabled(): Boolean = host.autoConnect

    override fun setAutoConnectEnabled(enabled: Boolean): Boolean = host.setAutoConnect(enabled)

    override fun getAppSelection(): String = host.appSelection.toJson()

    override fun setAppSelection(json: String) {
        host.appSelection = AppSelection.parse(json)
    }

    override fun beginBusy(token: String, timeoutMs: Long) = host.beginBusy(token, timeoutMs)

    override fun endBusy(token: String) = host.endBusy(token)

    /**
     * Synchronized: the event loop and binder threads both publish, and RemoteCallbackList allows one broadcast at a time
     * (a second beginBroadcast throws).
     */
    @Synchronized
    fun broadcast(status: VpnStatus) {
        latest = status
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
