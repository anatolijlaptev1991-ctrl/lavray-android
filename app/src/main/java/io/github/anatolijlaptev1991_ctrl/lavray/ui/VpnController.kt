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
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.SupervisorJob
import kotlinx.coroutines.cancel
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.launch
import kotlinx.coroutines.withContext
import kotlinx.coroutines.withTimeoutOrNull
import java.io.IOException

/**
 * UI-process side of the :vpn service. [bind] returns at once; the system starts
 * the :vpn process asynchronously, and every binder transaction runs on a
 * background dispatcher, never on the main thread.
 */
class VpnController(context: Context) {
    private val app = context.applicationContext
    private val scope = CoroutineScope(SupervisorJob() + Dispatchers.IO)
    private val mutableStatus = MutableStateFlow(VpnStatus.IDLE)
    // Replaced on the main thread at unbind, disconnect and rebind, awaited from IO coroutines: a connection completes the
    // deferred captured when it arrived, and replacing the gate fails everyone still waiting on the old binding.
    private val ready = ServiceGate<IVpnService>()
    private var bound = false
    @Volatile private var connected: IVpnService? = null

    val status: StateFlow<VpnStatus> = mutableStatus

    // connect, reconnect and disconnect reach the service in the order they were called.
    private val commands = SerialCalls<IVpnService>(scope) { service() }
    private val holds = HoldRegistry<IVpnService>()

    private val observer = object : IVpnObserver.Stub() {
        override fun onStatus(state: Int, error: String?, detail: String?) {
            mutableStatus.value = VpnStatusWire.decode(state, error, detail)
        }
    }

    private val connection = object : ServiceConnection {
        override fun onServiceConnected(name: ComponentName, service: IBinder) {
            val api = IVpnService.Stub.asInterface(service)
            this@VpnController.connected = api
            val pending = ready.capture()
            scope.launch {
                runCatching { api.registerObserver(observer) }
                pending.complete(api)
            }
        }

        override fun onServiceDisconnected(name: ComponentName) {
            connected = null
            ready.replace()
            mutableStatus.value = VpnStatus.IDLE
        }

        // The binding itself is dead (the service's package was replaced, or its process was killed in a way the
        // system does not reconnect): onServiceConnected will not come again for it, so bind anew.
        override fun onBindingDied(name: ComponentName) {
            onServiceDisconnected(name)
            rebind()
        }
    }

    private fun rebind() {
        if (!bound) return
        runCatching { app.unbindService(connection) }
        bound = false
        bind()
    }

    fun bind() {
        if (bound) return
        bound = app.bindService(Intent().setClassName(app, StartIntent.SERVICE_CLASS), connection, Context.BIND_AUTO_CREATE)
    }

    fun unbind() {
        if (!bound) return
        val api = connected
        connected = null
        scope.launch { runCatching { api?.unregisterObserver(observer) } }
        app.unbindService(connection)
        bound = false
        ready.replace()
    }

    fun close() {
        unbind()
        scope.cancel()
    }

    /**
     * The bound service, waiting for the binding at most [BIND_WAIT_MS]. A service that never binds fails the caller with
     * [ServiceNotBoundException] instead of suspending it for ever (a settings screen would otherwise hold its mutex
     * indefinitely); a binding replaced meanwhile fails at once (see [ServiceGate]).
     */
    private suspend fun service(): IVpnService = withTimeoutOrNull(BIND_WAIT_MS) { ready.await() } ?: throw ServiceNotBoundException()

    /** Where the core listens and the token that opens it; starts the core if needed. */
    suspend fun endpoint(): CoreEndpoint = withContext(Dispatchers.IO) {
        val bundle = service().endpoint()
        CoreEndpoint(
            checkNotNull(bundle.getString(VpnBinder.KEY_PATH)),
            checkNotNull(bundle.getString(VpnBinder.KEY_TOKEN)),
        )
    }

    /** profileId <= 0 connects the last server. */
    fun connect(profileId: Long = 0) {
        commands.submit { it.connect(profileId) }
    }

    /**
     * Rebuilds the tunnel (server switch, changed settings) keeping the interface blocked, not open, until the new one
     * replaces it (at most 30 s). profileId <= 0 reconnects the last server. Prefer it to disconnect() then connect().
     */
    fun reconnect(profileId: Long = 0) {
        commands.submit { it.reconnect(profileId) }
    }

    fun disconnect() {
        commands.submit { it.disconnect() }
    }

    suspend fun isAutoConnectEnabled(): Boolean = withContext(Dispatchers.IO) { service().isAutoConnectEnabled() }

    /** The per-app choice kept by the :vpn process. */
    suspend fun appSelection(): AppSelection = withContext(Dispatchers.IO) { AppSelection.parse(service().getAppSelection()) }

    /** Saves the choice; it applies the next time the tunnel is built. */
    suspend fun setAppSelection(selection: AppSelection) = withContext(Dispatchers.IO) { service().setAppSelection(selection.toJson()) }

    /**
     * Writes the "connect at phone start" setting and the boot receiver. True only when the value is really in force
     * afterwards; false (or an exception from the binder) means the stored value is the old one, so the switch must
     * show the old value.
     */
    suspend fun setAutoConnect(enabled: Boolean): Boolean = withContext(Dispatchers.IO) { service().setAutoConnectEnabled(enabled) }

    /** Fire and forget: kept for callers that cannot wait. A failed write is lost here; prefer [setAutoConnect]. */
    fun setAutoConnectEnabled(enabled: Boolean) {
        scope.launch { runCatching { setAutoConnect(enabled) } }
    }

    /**
     * Keeps the :vpn process alive while a long operation that runs in the core (a backup restore) is under way, even if
     * the UI unbinds. [token] names the hold, [timeoutMs] is a hard cap (the service clamps it to 15 minutes).
     */
    suspend fun beginBusy(token: String, timeoutMs: Long): Boolean {
        val bound = service()
        return holds.begin(token, bound) { it.beginBusy(token, timeoutMs) }
    }

    /**
     * Ends the hold on the service that accepted it, bounded, whether or not the UI is bound now. Never waits for a bind,
     * never throws; a hold that was not accepted is nothing to end.
     */
    suspend fun endBusy(token: String) = holds.end(token) { it.endBusy(token) }

    private companion object {
        /** A cold :vpn process is up in a couple of seconds; a service that is not bound after this is not coming. */
        const val BIND_WAIT_MS = 10_000L
    }
}

/** The service did not bind in time (or the binding was replaced): a call that needs it fails with this, never hangs. */
class ServiceNotBoundException : IOException("the VPN service is not bound")
