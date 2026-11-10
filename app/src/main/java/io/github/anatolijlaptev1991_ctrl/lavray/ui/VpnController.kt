// SPDX-License-Identifier: GPL-3.0-only
package io.github.anatolijlaptev1991_ctrl.lavray.ui

import android.content.ComponentName
import android.content.Context
import android.content.Intent
import android.content.ServiceConnection
import android.os.IBinder
import io.github.anatolijlaptev1991_ctrl.lavray.api.CoreEndpoint
import io.github.anatolijlaptev1991_ctrl.lavray.ipc.IVpnObserver
import io.github.anatolijlaptev1991_ctrl.lavray.ipc.IVpnService
import io.github.anatolijlaptev1991_ctrl.lavray.ipc.VpnStatus
import io.github.anatolijlaptev1991_ctrl.lavray.ipc.VpnStatusWire
import io.github.anatolijlaptev1991_ctrl.lavray.vpn.AppSelection
import io.github.anatolijlaptev1991_ctrl.lavray.vpn.StartIntent
import io.github.anatolijlaptev1991_ctrl.lavray.vpn.VpnBinder
import kotlinx.coroutines.CompletableDeferred
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.SupervisorJob
import kotlinx.coroutines.cancel
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.launch
import kotlinx.coroutines.withContext

/**
 * UI-process side of the :vpn service. [bind] returns at once; the system starts
 * the :vpn process asynchronously, and every binder transaction runs on a
 * background dispatcher, never on the main thread.
 */
class VpnController(context: Context) {
    private val app = context.applicationContext
    private val scope = CoroutineScope(SupervisorJob() + Dispatchers.IO)
    private val mutableStatus = MutableStateFlow(VpnStatus.IDLE)
    // Replaced on the main thread at unbind and disconnect, read from IO coroutines: volatile, and always completed
    // through the deferred captured when its connection arrived, never through whatever the field holds later.
    @Volatile private var ready = CompletableDeferred<IVpnService>()
    private var bound = false
    @Volatile private var service: IVpnService? = null

    val status: StateFlow<VpnStatus> = mutableStatus

    private val observer = object : IVpnObserver.Stub() {
        override fun onStatus(state: Int, error: String?, detail: String?) {
            mutableStatus.value = VpnStatusWire.decode(state, error, detail)
        }
    }

    private val connection = object : ServiceConnection {
        override fun onServiceConnected(name: ComponentName, service: IBinder) {
            val api = IVpnService.Stub.asInterface(service)
            this@VpnController.service = api
            val pending = ready
            scope.launch {
                runCatching { api.registerObserver(observer) }
                pending.complete(api)
            }
        }

        override fun onServiceDisconnected(name: ComponentName) {
            service = null
            ready = CompletableDeferred()
            mutableStatus.value = VpnStatus.IDLE
        }
    }

    fun bind() {
        if (bound) return
        bound = app.bindService(Intent().setClassName(app, StartIntent.SERVICE_CLASS), connection, Context.BIND_AUTO_CREATE)
    }

    fun unbind() {
        if (!bound) return
        val api = service
        service = null
        scope.launch { runCatching { api?.unregisterObserver(observer) } }
        app.unbindService(connection)
        bound = false
        ready = CompletableDeferred()
    }

    fun close() {
        unbind()
        scope.cancel()
    }

    /** Where the core listens and the token that opens it; starts the core if needed. */
    suspend fun endpoint(): CoreEndpoint = withContext(Dispatchers.IO) {
        val bundle = ready.await().endpoint()
        CoreEndpoint(
            checkNotNull(bundle.getString(VpnBinder.KEY_PATH)),
            checkNotNull(bundle.getString(VpnBinder.KEY_TOKEN)),
        )
    }

    /** profileId <= 0 connects the last server. */
    fun connect(profileId: Long = 0) {
        scope.launch { runCatching { ready.await().connect(profileId) } }
    }

    /**
     * Rebuilds the tunnel (server switch, changed settings) keeping the interface blocked, not open, until the new one
     * replaces it (at most 30 s). profileId <= 0 reconnects the last server. Prefer it to disconnect() then connect().
     */
    fun reconnect(profileId: Long = 0) {
        scope.launch { runCatching { ready.await().reconnect(profileId) } }
    }

    fun disconnect() {
        scope.launch { runCatching { ready.await().disconnect() } }
    }

    suspend fun isAutoConnectEnabled(): Boolean = withContext(Dispatchers.IO) { ready.await().isAutoConnectEnabled() }

    /** The per-app choice kept by the :vpn process. */
    suspend fun appSelection(): AppSelection = withContext(Dispatchers.IO) { AppSelection.parse(ready.await().getAppSelection()) }

    /** Saves the choice; it applies the next time the tunnel is built. */
    suspend fun setAppSelection(selection: AppSelection) = withContext(Dispatchers.IO) { ready.await().setAppSelection(selection.toJson()) }

    /**
     * Writes the "connect at phone start" setting and the boot receiver. True only when the value is really in force
     * afterwards; false (or an exception from the binder) means the stored value is the old one, so the switch must
     * show the old value.
     */
    suspend fun setAutoConnect(enabled: Boolean): Boolean = withContext(Dispatchers.IO) { ready.await().setAutoConnectEnabled(enabled) }

    /** Fire and forget: kept for callers that cannot wait. A failed write is lost here; prefer [setAutoConnect]. */
    fun setAutoConnectEnabled(enabled: Boolean) {
        scope.launch { runCatching { setAutoConnect(enabled) } }
    }

    /**
     * Keeps the :vpn process alive while a long operation that runs in the core (a backup restore) is under way, even if
     * the UI unbinds. [token] names the hold, [timeoutMs] is a hard cap (the service clamps it to 15 minutes).
     */
    suspend fun beginBusy(token: String, timeoutMs: Long) = withContext(Dispatchers.IO) { ready.await().beginBusy(token, timeoutMs) }

    suspend fun endBusy(token: String) = withContext(Dispatchers.IO) { ready.await().endBusy(token) }
}
