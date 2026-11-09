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
import io.github.anatolijlaptev1991_ctrl.lavray.ipc.VpnState
import io.github.anatolijlaptev1991_ctrl.lavray.ipc.VpnStatus
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
    private var ready = CompletableDeferred<IVpnService>()
    private var bound = false
    @Volatile private var service: IVpnService? = null

    val status: StateFlow<VpnStatus> = mutableStatus

    private val observer = object : IVpnObserver.Stub() {
        override fun onStatus(state: Int, error: String?) {
            mutableStatus.value = VpnStatus(VpnState.fromCode(state), error)
        }
    }

    private val connection = object : ServiceConnection {
        override fun onServiceConnected(name: ComponentName, service: IBinder) {
            val api = IVpnService.Stub.asInterface(service)
            this@VpnController.service = api
            scope.launch {
                runCatching { api.registerObserver(observer) }
                ready.complete(api)
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

    fun disconnect() {
        scope.launch { runCatching { ready.await().disconnect() } }
    }

    suspend fun isAutoConnectEnabled(): Boolean = withContext(Dispatchers.IO) { ready.await().isAutoConnectEnabled() }

    /** The per-app choice kept by the :vpn process. */
    suspend fun appSelection(): AppSelection = withContext(Dispatchers.IO) { AppSelection.parse(ready.await().getAppSelection()) }

    /** Saves the choice; it applies the next time the tunnel is built. */
    suspend fun setAppSelection(selection: AppSelection) = withContext(Dispatchers.IO) { ready.await().setAppSelection(selection.toJson()) }

    fun setAutoConnectEnabled(enabled: Boolean) {
        scope.launch { runCatching { ready.await().setAutoConnectEnabled(enabled) } }
    }
}
