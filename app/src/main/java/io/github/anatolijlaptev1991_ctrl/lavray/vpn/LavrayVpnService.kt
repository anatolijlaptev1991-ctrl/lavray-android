// SPDX-License-Identifier: GPL-3.0-only
package io.github.anatolijlaptev1991_ctrl.lavray.vpn

import android.app.NotificationManager
import android.app.PendingIntent
import android.content.BroadcastReceiver
import android.content.Context
import android.content.Intent
import android.content.IntentFilter
import android.content.pm.ServiceInfo
import android.net.ConnectivityManager
import android.net.VpnService
import android.os.Build
import android.os.IBinder
import android.os.PowerManager
import io.github.anatolijlaptev1991_ctrl.lavray.R
import io.github.anatolijlaptev1991_ctrl.lavray.api.ApiException
import io.github.anatolijlaptev1991_ctrl.lavray.api.ConnectCode
import io.github.anatolijlaptev1991_ctrl.lavray.api.CoreEndpoint
import io.github.anatolijlaptev1991_ctrl.lavray.api.LavrayClient
import io.github.anatolijlaptev1991_ctrl.lavray.api.gen.CoreState
import io.github.anatolijlaptev1991_ctrl.lavray.api.gen.CoreStatus
import io.github.anatolijlaptev1991_ctrl.lavray.api.gen.DisconnectRequest
import io.github.anatolijlaptev1991_ctrl.lavray.api.gen.coreDisconnect
import io.github.anatolijlaptev1991_ctrl.lavray.api.gen.coreSubscribeEvents
import io.github.anatolijlaptev1991_ctrl.lavray.ipc.VpnState
import io.github.anatolijlaptev1991_ctrl.lavray.ipc.VpnStatus
import io.github.anatolijlaptev1991_ctrl.lavray.ui.MainActivity
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.Job
import kotlinx.coroutines.SupervisorJob
import kotlinx.coroutines.cancel
import kotlinx.coroutines.channels.Channel
import kotlinx.coroutines.delay
import kotlinx.coroutines.launch
import kotlinx.coroutines.runBlocking
import kotlinx.coroutines.sync.Mutex
import kotlinx.coroutines.sync.withLock
import kotlinx.coroutines.withContext
import kotlinx.coroutines.withTimeoutOrNull
import java.util.concurrent.atomic.AtomicInteger

/**
 * The tunnel, in its own process (:vpn). One coroutine consumes every event in order,
 * so the auto-connect machine and the status fields have a single writer. Nothing here
 * polls, schedules alarms or holds a wake lock; the only timers are the backoff delays.
 */
class LavrayVpnService : VpnService(), VpnBinderHost {
    private sealed interface Event {
        data class Command(val command: StartCommand) : Event
        data class Auto(val event: AutoEvent) : Event
        data class Core(val status: CoreStatus) : Event
        data class UiBound(val bound: Boolean) : Event
        data object Revoked : Event
    }

    private val events = Channel<Event>(Channel.UNLIMITED)
    private val scope = CoroutineScope(SupervisorJob() + Dispatchers.Default)
    private val machine = AutoConnectMachine()
    private val gate = NotificationGate()
    private val binder = VpnBinder(this)
    private val coreMutex = Mutex()

    private lateinit var core: CoreHost
    private lateinit var platform: VpnPlatform
    private lateinit var watcher: NetworkWatcher
    private lateinit var prefs: VpnPrefs

    @Volatile private var client: LavrayClient? = null
    @Volatile private var endpoint: CoreEndpoint? = null
    @Volatile private var status: VpnStatus = VpnStatus.IDLE

    // The fields below are touched only by the event loop.
    private var coreStatus: CoreStatus? = null
    private var requestedProfile: Long? = null
    private var lastFailure: String? = null
    private var needsPermission = false
    private var uiBound = false
    private var foreground = false
    private var attemptJob: Job? = null
    private var retryJob: Job? = null
    private var eventsJob: Job? = null

    private val screenOn = object : BroadcastReceiver() {
        override fun onReceive(context: Context, intent: Intent) {
            gate.onScreenOn()?.let { notify(it) }
        }
    }

    override fun onCreate() {
        super.onCreate()
        live.incrementAndGet()
        VpnNotifications.ensureChannels(this)
        prefs = VpnPrefs(this)
        val cm = getSystemService(ConnectivityManager::class.java)
        val openApp = PendingIntent.getActivity(this, 1, Intent(this, MainActivity::class.java), PendingIntent.FLAG_IMMUTABLE)
        platform = VpnPlatform(this, cm, TunEstablisher(this, getString(R.string.app_name), openApp) { prefs.appSelection })
        core = CoreHost(this, platform)
        watcher = NetworkWatcher(
            cm,
            onAvailable = { post(Event.Auto(AutoEvent.NetworkAvailable)) },
            onLost = { post(Event.Auto(AutoEvent.NetworkLost)) },
        )
        registerScreenReceiver()
        scope.launch { for (event in events) handle(event) }
    }

    override fun onStartCommand(intent: Intent?, flags: Int, startId: Int): Int {
        val command = StartIntent.parse(
            intent?.action,
            intent?.getStringExtra(StartIntent.EXTRA_SOURCE),
            intent?.getLongExtra(StartIntent.EXTRA_PROFILE_ID, 0L) ?: 0L,
        )
        // A start that was promised to become foreground must do so within seconds, whatever follows.
        if (command is StartCommand.Connect) enterForeground()
        post(Event.Command(command))
        return START_NOT_STICKY
    }

    override fun onBind(intent: Intent?): IBinder? {
        if (intent?.action == SERVICE_INTERFACE) return super.onBind(intent)
        post(Event.UiBound(true))
        scope.launch(Dispatchers.IO) { runCatching { ensureCore() } }
        return binder
    }

    override fun onUnbind(intent: Intent?): Boolean {
        if (intent?.action != SERVICE_INTERFACE) post(Event.UiBound(false))
        return super.onUnbind(intent)
    }

    override fun onRevoke() {
        post(Event.Revoked)
    }

    override fun onDestroy() {
        unregisterReceiver(screenOn)
        watcher.stop()
        platform.close()
        binder.kill()
        scope.cancel()
        live.decrementAndGet()
        val host = core
        // Stop the core and leave: this process exists only for the tunnel. A new
        // service instance created meanwhile keeps the process alive.
        Thread({
            host.stop()
            if (live.get() == 0) android.os.Process.killProcess(android.os.Process.myPid())
        }, "lavray-exit").start()
        super.onDestroy()
    }

    // ---- VpnBinderHost

    override fun endpoint(): CoreEndpoint {
        runBlocking { ensureCore() }
        return checkNotNull(endpoint)
    }

    override fun connect(profileId: Long) {
        val intent = Intent(this, LavrayVpnService::class.java)
            .putExtra(StartIntent.EXTRA_SOURCE, StartSource.USER.wire)
            .putExtra(StartIntent.EXTRA_PROFILE_ID, profileId)
        try {
            startForegroundService(intent)
        } catch (e: RuntimeException) {
            publishNow(VpnStatus(VpnState.ERROR, e.message))
        }
    }

    override fun disconnect() = post(Event.Command(StartCommand.Disconnect))

    override fun currentStatus(): VpnStatus = status

    override var autoConnect: Boolean
        get() = prefs.autoConnect
        set(value) {
            prefs.autoConnect = value
        }

    override var appSelection: AppSelection
        get() = prefs.appSelection
        set(value) {
            prefs.appSelection = value
        }

    // ---- event loop

    private fun post(event: Event) {
        events.trySend(event)
    }

    private suspend fun handle(event: Event) {
        when (event) {
            is Event.Command -> when (val c = event.command) {
                is StartCommand.Connect -> startConnect(c)
                StartCommand.Disconnect -> stopConnection(AutoEvent.ManualDisconnect)
            }
            is Event.Auto -> apply(machine.handle(event.event))
            is Event.Core -> onCoreStatus(event.status)
            is Event.UiBound -> uiBound = event.bound
            Event.Revoked -> stopConnection(AutoEvent.ConsentRevoked)
        }
        publish()
    }

    private fun startConnect(command: StartCommand.Connect) {
        needsPermission = false
        lastFailure = null
        VpnNotifications.cancelAlert(this)
        requestedProfile = command.profileId
        val consent = VpnService.prepare(this) == null
        apply(machine.handle(AutoEvent.Start(command.source, consent, watcher.isUp())))
    }

    private suspend fun stopConnection(reason: AutoEvent) {
        apply(machine.handle(reason))
        attemptJob?.cancel()
        client?.let { c ->
            try {
                c.coreDisconnect(DisconnectRequest(collectStats = false))
            } catch (e: CancellationException) {
                throw e
            } catch (_: Exception) {
                // Not running, or the core is already gone: the tunnel is down either way.
            }
        }
        coreStatus = coreStatus?.copy(state = CoreState.CORE_STATE_IDLE)
        platform.releaseTun()
    }

    private fun onCoreStatus(next: CoreStatus) {
        val wasActive = isCoreActive()
        val wasConnected = coreStatus?.state == CoreState.CORE_STATE_CONNECTED
        coreStatus = next
        // The core has let go of the tunnel: close our descriptor or the interface outlives it.
        if (wasActive && !isCoreActive()) platform.releaseTun()
        val dropped = next.state == CoreState.CORE_STATE_ERROR || next.state == CoreState.CORE_STATE_IDLE
        if (wasConnected && dropped) apply(machine.handle(AutoEvent.ConnectionLost))
    }

    private fun apply(effects: List<AutoEffect>) {
        for (effect in effects) {
            when (effect) {
                AutoEffect.StartAttempt -> launchAttempt()
                is AutoEffect.ScheduleRetry -> {
                    retryJob?.cancel()
                    retryJob = scope.launch {
                        delay(effect.delayMs)
                        post(Event.Auto(AutoEvent.TimerFired))
                    }
                }
                AutoEffect.CancelRetry -> retryJob?.cancel()
                is AutoEffect.WatchNetwork -> watchNetwork(effect.enabled)
                AutoEffect.NotifyNeedsPermission -> {
                    needsPermission = true
                    VpnNotifications.postPermissionNeeded(this)
                }
                is AutoEffect.Fail -> {
                    lastFailure = effect.message
                    if (!uiBound) VpnNotifications.postFailure(this, effect.message)
                }
            }
        }
    }

    private fun watchNetwork(enabled: Boolean) {
        if (!enabled) {
            watcher.stop()
            return
        }
        watcher.start()
        // Registering may have missed a change that happened since the machine last looked.
        val up = watcher.isUp()
        if (up != machine.state.networkAvailable) {
            post(Event.Auto(if (up) AutoEvent.NetworkAvailable else AutoEvent.NetworkLost))
        }
    }

    private fun launchAttempt() {
        attemptJob?.cancel()
        val profile = requestedProfile
        attemptJob = scope.launch(Dispatchers.IO) {
            try {
                val c = ensureCore()
                if (eventsJob?.isActive != true) startEventStream(c)
                val result = withTimeoutOrNull(ATTEMPT_TIMEOUT_MS) { ConnectAttempt.run(c, profile) }
                    ?: throw ApiException(ConnectCode.DEADLINE_EXCEEDED, "connect timed out")
                post(Event.Core(result))
                post(Event.Auto(AutoEvent.AttemptSucceeded))
            } catch (e: CancellationException) {
                throw e
            } catch (e: Exception) {
                val failure = ConnectAttempt.classify(e)
                platform.releaseTun()
                post(Event.Auto(AutoEvent.AttemptFailed(failure.permanent, failure.message)))
            }
        }
    }

    // ---- core

    private suspend fun ensureCore(): LavrayClient = coreMutex.withLock {
        client ?: withContext(Dispatchers.IO) {
            val ep = core.ensureStarted()
            endpoint = ep
            LavrayClient(ep).also {
                client = it
                startEventStream(it)
            }
        }
    }

    private fun startEventStream(c: LavrayClient) {
        eventsJob?.cancel()
        eventsJob = scope.launch(Dispatchers.IO) {
            try {
                c.coreSubscribeEvents().collect { event -> event.stateChanged?.let { post(Event.Core(it)) } }
            } catch (e: CancellationException) {
                throw e
            } catch (_: Exception) {
                // The stream ends when the core stops; the next attempt reopens it.
            }
        }
    }

    // ---- status, notification, lifecycle

    private fun publish() {
        publishNow(computeStatus())
        val active = isCoreActive()
        val pending = machine.state.pending
        if (foreground && ShutdownPolicy.shouldLeaveForeground(active, pending)) {
            stopForeground(STOP_FOREGROUND_REMOVE)
            foreground = false
        }
        if (ShutdownPolicy.shouldStop(active, uiBound, pending)) stopSelf()
    }

    private fun publishNow(next: VpnStatus) {
        status = next
        binder.broadcast(next)
        if (foreground) offerNotification(NotificationModel(next.state, next.error))
    }

    private fun computeStatus(): VpnStatus {
        val fromCore = coreStatus?.let { CoreStatusMapper.fromCore(it.state, it.lastError) } ?: VpnStatus.IDLE
        return when {
            needsPermission -> VpnStatus(VpnState.NEEDS_PERMISSION)
            machine.state.phase == AutoPhase.WAITING_NETWORK || machine.state.phase == AutoPhase.BACKING_OFF ->
                VpnStatus(VpnState.WAITING_NETWORK)
            machine.state.phase == AutoPhase.FAILED -> VpnStatus(VpnState.ERROR, lastFailure)
            machine.state.phase == AutoPhase.ATTEMPTING && fromCore.state == VpnState.IDLE -> VpnStatus(VpnState.CONNECTING)
            else -> fromCore
        }
    }

    private fun isCoreActive(): Boolean = when (coreStatus?.state) {
        CoreState.CORE_STATE_CONNECTING,
        CoreState.CORE_STATE_CONNECTED,
        CoreState.CORE_STATE_DISCONNECTING,
        CoreState.CORE_STATE_RECONNECTING -> true
        else -> false
    }

    private fun enterForeground() {
        if (foreground) return
        val model = gate.forcePost(NotificationModel(VpnState.CONNECTING))
        val notification = VpnNotifications.status(this, model)
        try {
            if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.UPSIDE_DOWN_CAKE) {
                startForeground(VpnNotifications.ID_STATUS, notification, ServiceInfo.FOREGROUND_SERVICE_TYPE_SPECIAL_USE)
            } else {
                startForeground(VpnNotifications.ID_STATUS, notification)
            }
            foreground = true
        } catch (e: RuntimeException) {
            // Not allowed to run in the foreground (restricted start): report and stand down.
            VpnNotifications.postFailure(this, e.message)
            stopSelf()
        }
    }

    // While the screen is off the update is held back and released by the screen-on receiver.
    private fun offerNotification(model: NotificationModel) {
        val interactive = getSystemService(PowerManager::class.java).isInteractive
        gate.offer(model, interactive)?.let { notify(it) }
    }

    private fun notify(model: NotificationModel) {
        if (!foreground) return
        getSystemService(NotificationManager::class.java)
            .notify(VpnNotifications.ID_STATUS, VpnNotifications.status(this, model))
    }

    private fun registerScreenReceiver() {
        val filter = IntentFilter(Intent.ACTION_SCREEN_ON)
        if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.TIRAMISU) {
            registerReceiver(screenOn, filter, Context.RECEIVER_NOT_EXPORTED)
        } else {
            registerReceiver(screenOn, filter)
        }
    }

    private companion object {
        val live = AtomicInteger(0)
        const val ATTEMPT_TIMEOUT_MS = 90_000L
    }
}
