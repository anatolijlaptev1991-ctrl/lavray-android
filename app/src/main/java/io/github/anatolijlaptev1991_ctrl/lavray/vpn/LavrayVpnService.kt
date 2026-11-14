// SPDX-License-Identifier: GPL-3.0-only
package io.github.anatolijlaptev1991_ctrl.lavray.vpn

import android.app.PendingIntent
import android.content.Intent
import android.net.ConnectivityManager
import android.net.VpnService
import android.os.IBinder
import android.os.SystemClock
import android.util.Log
import io.github.anatolijlaptev1991_ctrl.lavray.R
import io.github.anatolijlaptev1991_ctrl.lavray.api.ApiException
import io.github.anatolijlaptev1991_ctrl.lavray.api.ConnectCode
import io.github.anatolijlaptev1991_ctrl.lavray.api.CoreEndpoint
import io.github.anatolijlaptev1991_ctrl.lavray.api.LavrayClient
import io.github.anatolijlaptev1991_ctrl.lavray.api.gen.CoreStatus
import io.github.anatolijlaptev1991_ctrl.lavray.api.gen.DisconnectRequest
import io.github.anatolijlaptev1991_ctrl.lavray.api.gen.coreDisconnect
import io.github.anatolijlaptev1991_ctrl.lavray.ipc.FailureKind
import io.github.anatolijlaptev1991_ctrl.lavray.ipc.VpnState
import io.github.anatolijlaptev1991_ctrl.lavray.ipc.VpnStatus
import io.github.anatolijlaptev1991_ctrl.lavray.ui.MainActivity
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.NonCancellable
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.Job
import kotlinx.coroutines.SupervisorJob
import kotlinx.coroutines.async
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
import java.util.concurrent.atomic.AtomicLong
import java.util.concurrent.atomic.AtomicReference

/**
 * The tunnel, in its own process (:vpn): the interpreter of [SessionEngine]. One coroutine consumes every event in
 * order and runs the effects the engine returns, so the engine and the status fields have a single writer. Nothing
 * here polls, schedules alarms or holds a wake lock; the only timers are the backoff delays, the grace period
 * after a core drop, the blackhole cap and the busy-hold deadline.
 *
 * The decisions (what a command means, who writes the persisted wish, when the interface is held, when the
 * service is foreground or stops) are in [SessionEngine] and the policies it uses, all tested on the JVM.
 */
class LavrayVpnService : VpnService(), VpnBinderHost, SessionEnv {
    private sealed interface Event {
        data class Command(val command: StartCommand, val seq: Long) : Event
        data class Auto(val event: AutoEvent) : Event
        data class Core(val status: CoreStatus) : Event
        data class AttemptDone(val generation: Int, val outcome: AttemptOutcome) : Event
        data class GraceElapsed(val token: Int) : Event
        data class TunEstablished(val generation: Int, val skipped: List<String>) : Event
        data class RestrictedStart(val message: String?, val seq: Long) : Event
        data object KeepAliveRefused : Event
        data class Abandoned(val seq: Long) : Event
        data object Poke : Event
        data object Revoked : Event
    }

    private val events = Channel<Event>(Channel.UNLIMITED)
    private val scope = CoroutineScope(SupervisorJob() + Dispatchers.Default)
    private val ui = UiBindTracker()
    private val busyHold = BusyHold()
    private val binder = VpnBinder(this)
    private val coreMutex = Mutex()

    /** The newest command from the person; commands older than it that are still queued are stale. */
    private val userSeq = AtomicLong(0)

    /** A job slot written from several threads (Binder, IO, the loop): swapping it cancels the previous one. */
    private class JobSlot {
        private val ref = AtomicReference<Job?>()
        fun set(job: Job) { ref.getAndSet(job)?.cancel() }
        fun cancel() { ref.getAndSet(null)?.cancel() }
        val isActive: Boolean get() = ref.get()?.isActive == true
    }

    /** The startId of the newest start: stopSelf(startId) does nothing when a newer start has arrived since. */
    @Volatile private var lastStartId = 0

    private lateinit var core: CoreHost
    private lateinit var platform: VpnPlatform
    private lateinit var watcher: NetworkWatcher
    private lateinit var prefs: VpnPrefs
    private lateinit var wish: TunnelIntent
    private lateinit var fg: ServiceForeground
    private lateinit var engine: SessionEngine
    private lateinit var bootSetting: AutoConnectSetting
    private lateinit var userCommands: UserCommands
    private val foregroundConfirm = ForegroundConfirm()
    private val selfBinding = SelfBinding(this)
    private val busyLock = Any()

    @Volatile private var client: LavrayClient? = null
    @Volatile private var endpoint: CoreEndpoint? = null

    /** Set from Go threads while the interface is built; read by the attempt that asked for it. */
    @Volatile private var tunFailure: TunFailure? = null

    // These two are written from Binder and IO threads as well as the loop.
    private val eventsJob = JobSlot()
    private val busyWake = JobSlot()

    // The fields below are touched only by the event loop.
    private var attemptJob: Job? = null
    private var retryJob: Job? = null
    private var graceJob: Job? = null
    private var holdJob: Job? = null
    private var stableJob: Job? = null

    private val platformEvents = object : PlatformListener {
        override fun onTunEstablished(generation: Int, skippedPackages: List<String>) {
            post(Event.TunEstablished(generation, skippedPackages))
        }

        override fun onTunFailed(failure: TunFailure) {
            tunFailure = failure
        }
    }

    override fun onCreate() {
        super.onCreate()
        live.incrementAndGet()
        // Without this the system does not restart the service after its process dies with a tunnel up (see SelfBinding).
        selfBinding.bind()
        VpnNotifications.ensureChannels(this)
        prefs = VpnPrefs(this)
        wish = TunnelIntent(prefs, { BootCount.read(this) }, { BootId.current }, { SystemClock.elapsedRealtime() })
        bootSetting = BootComponents.setting(this, prefs)
        fg = ServiceForeground(this)
        val cm = getSystemService(ConnectivityManager::class.java)
        val openApp = PendingIntent.getActivity(this, 1, Intent(this, MainActivity::class.java), PendingIntent.FLAG_IMMUTABLE)
        platform = VpnPlatform(
            this,
            cm,
            TunEstablisher(this, getString(R.string.app_name), openApp, appSelection = { prefs.appSelection }),
            platformEvents,
        )
        core = CoreHost(this, platform)
        watcher = NetworkWatcher(
            cm,
            onAvailable = { post(Event.Auto(AutoEvent.NetworkAvailable)) },
            onLost = { post(Event.Auto(AutoEvent.NetworkLost)) },
        )
        engine = SessionEngine(this, wish)
        userCommands = UserCommands(
            userSeq, wish,
            { action, profileId, seq ->
                val start = Intent(this, LavrayVpnService::class.java)
                    .putExtra(StartIntent.EXTRA_SOURCE, StartSource.USER.wire)
                    .putExtra(StartIntent.EXTRA_PROFILE_ID, profileId)
                    .putExtra(StartIntent.EXTRA_SEQ, seq)
                if (action != null) start.setAction(action)
                startForegroundService(start)
            },
            { command, seq -> post(Event.Command(command, seq)) },
            { message, seq ->
                publishNow(VpnStatus(VpnState.ERROR, message, lastFailure = message, failureKind = FailureKind.RESTRICTED_START))
                post(Event.Abandoned(seq))
            },
        )
        // Before any command can arrive: a wish left by a dead process of this boot is an unexpected stop. The exit
        // reason (a Binder call) is read only then.
        engine.onCreated { ExitReasons.lastVpnExit(this) }
        fg.registerScreenReceiver()
        scope.launch(Dispatchers.IO) { reconcileBootReceiver() }
        scope.launch { for (event in events) handle(event) }
    }

    override fun onStartCommand(intent: Intent?, flags: Int, startId: Int): Int {
        lastStartId = startId
        val command = StartIntent.parse(
            intent?.action,
            intent?.getStringExtra(StartIntent.EXTRA_SOURCE),
            intent?.getLongExtra(StartIntent.EXTRA_PROFILE_ID, 0L) ?: 0L,
            hasIntent = intent != null,
        )
        val route = StartRouter.route(
            command,
            intent?.getLongExtra(StartIntent.EXTRA_SEQ, 0L) ?: 0L,
            { fg.enter(firstModel()) },
            { userCommands.takeDisconnect() },
            { busyHold.clear() },
        )
        if (command is StartCommand.KeepAlive) foregroundConfirm.report(route is StartRoute.Deliver)
        return when (route) {
            is StartRoute.Deliver -> {
                post(Event.Command(route.command, route.seq))
                // Sticky: after the process dies the system restarts the service with a null intent, and
                // [StartCommand.Resume] reconnects only if the tunnel was wanted, consent is there, and the process has
                // not died repeatedly.
                START_STICKY
            }
            // The foreground promise could not be kept: nothing is started, the failure is recorded and shown, and the
            // started state ends (whether or not the UI is bound).
            // Sticky all the same: a tunnel may be running in a service that was demoted, and the wish (cleared by the
            // failure) decides whether a system restart resumes anything.
            is StartRoute.RefusedForeground -> {
                post(Event.RestrictedStart(null, route.seq))
                START_STICKY
            }
            StartRoute.KeepAliveRefused -> {
                post(Event.KeepAliveRefused)
                START_STICKY
            }
        }
    }

    override fun onBind(intent: Intent?): IBinder? {
        if (intent?.action == SERVICE_INTERFACE) return super.onBind(intent)
        ui.onBind()
        post(Event.Poke)
        scope.launch(Dispatchers.IO) { runCatching { ensureCore() } }
        return binder
    }

    override fun onRebind(intent: Intent?) {
        ui.onRebind()
        post(Event.Poke)
    }

    override fun onUnbind(intent: Intent?): Boolean {
        if (intent?.action == SERVICE_INTERFACE) return super.onUnbind(intent)
        ui.onUnbind()
        post(Event.Poke)
        // True: when the UI comes back to a live service the system calls onRebind instead of staying silent.
        return true
    }

    override fun onRevoke() {
        post(Event.Revoked)
    }

    override fun onDestroy() {
        selfBinding.unbind()
        fg.unregisterScreenReceiver()
        watcher.stop()
        platform.close()
        binder.kill()
        scope.cancel()
        live.decrementAndGet()
        val host = core
        // Stop the core and leave: this process exists only for the tunnel. A new service instance created meanwhile
        // keeps the process alive and owns its own core, unless the stop hangs: a Go runtime that cannot be stopped
        // would block that core too, so the process goes after a bounded wait either way.
        Thread({
            val finished = BoundedStop.run({ host.stop() }, STOP_TIMEOUT_MS)
            if (BoundedStop.shouldKillProcess(finished, otherServicesAlive = live.get() > 0)) {
                android.os.Process.killProcess(android.os.Process.myPid())
            }
        }, "lavray-exit").start()
        super.onDestroy()
    }

    // ---- SessionEnv

    override fun consentGranted(): Boolean = VpnService.prepare(this) == null
    override fun networkUp(): Boolean = watcher.isUp()
    override fun uiBound(): Boolean = ui.bound
    override fun tunHeld(): Boolean = platform.isTunHeld()
    override fun busy(): Boolean = busyHold.isBusy(nowMono())
    override fun nowMono(): Long = SystemClock.elapsedRealtime()
    override fun nowWall(): Long = System.currentTimeMillis()
    override fun latestUserSeq(): Long = userSeq.get()

    // ---- VpnBinderHost

    override fun endpoint(): CoreEndpoint {
        // A core whose stop hangs holds its lock until the process leaves: the caller fails now (the UI shows "core
        // unavailable") instead of blocking, with every screen's core call behind it, for as long as a busy hold lasts.
        check(!engine.coreUnusable) { "the core is being restarted" }
        runBlocking { ensureCore() }
        return checkNotNull(endpoint)
    }

    override fun connect(profileId: Long) = userCommands.connect(profileId)

    override fun reconnect(profileId: Long) = userCommands.reconnect(profileId)

    override fun disconnect() = userCommands.disconnect()

    override val autoConnect: Boolean get() = prefs.autoConnect

    // The preference and the boot receiver change together under one lock; a failure to switch the receiver on rolls the
    // preference back, so the caller never sees "on" for something that will not fire.
    override fun setAutoConnect(enabled: Boolean): Boolean {
        val result = bootSetting.apply(enabled)
        // The stored value says ON while the receiver may be off (the rollback could not be written): keep trying to switch it on.
        if (result == WriteResult.ROLLBACK_FAILED) scope.launch(Dispatchers.IO) { reconcileBootReceiver() }
        return result == WriteResult.SAVED
    }

    override var appSelection: AppSelection
        get() = prefs.appSelection
        set(value) {
            prefs.appSelection = value
        }

    override fun beginBusy(token: String, timeoutMs: Long): Boolean = synchronized(busyLock) {
        busyHold.begin(nowMono(), token, timeoutMs)
        // A process kept alive for a long operation is a started, foreground service: the keep-alive start does that, and
        // its answer (did the system let it enter the foreground?) is awaited here, so true means the hold is really in force.
        foregroundConfirm.arm()
        try {
            startForegroundService(Intent(this, LavrayVpnService::class.java).setAction(StartIntent.ACTION_KEEP_ALIVE))
        } catch (e: RuntimeException) {
            // Nothing keeps the process alive: the hold is not in force, and the caller is told.
            Log.w(TAG, "keep-alive start refused: " + e.javaClass.simpleName)
            busyHold.end(token)
            return@synchronized false
        }
        if (!foregroundConfirm.await(FOREGROUND_CONFIRM_MS)) {
            busyHold.end(token)
            return@synchronized false
        }
        rearmBusyWake()
        true
    }

    override fun endBusy(token: String) {
        busyHold.end(token)
        rearmBusyWake()
        post(Event.Poke)
    }

    /** One wake-up job for the earliest deadline of any hold; it re-arms itself for the next until none is left. */
    @Synchronized
    private fun rearmBusyWake() {
        busyWake.cancel()
        if (busyHold.delayUntilNext(nowMono()) == null) return
        busyWake.set(
            scope.launch {
                while (true) {
                    val wait = busyHold.delayUntilNext(nowMono()) ?: break
                    delay(wait)
                    post(Event.Poke)
                }
            },
        )
    }

    // ---- event loop

    private fun post(event: Event) {
        events.trySend(event)
    }

    private suspend fun handle(event: Event) {
        // One bad event must not end the loop: the tunnel outlives any single handler's failure.
        try {
            run(dispatch(event))
        } catch (e: CancellationException) {
            throw e
        } catch (e: Throwable) {
            Log.e(TAG, "event failed: ${e.javaClass.simpleName}")
            runCatching { publishNow(VpnStatus(VpnState.ERROR, e.message ?: e.javaClass.simpleName)) }
        }
    }

    private fun dispatch(event: Event): List<SessionEffect> = when (event) {
        is Event.Command -> when (val c = event.command) {
            is StartCommand.Connect -> engine.onConnect(c.source, c.profileId, event.seq)
            is StartCommand.Reconnect -> engine.onReconnect(c.profileId, event.seq)
            StartCommand.Disconnect -> engine.onDisconnect(event.seq)
            StartCommand.Resume -> engine.onResume()
            StartCommand.KeepAlive -> engine.onPoke()
        }
        is Event.Auto -> engine.onAuto(event.event)
        is Event.Core -> engine.onCore(event.status)
        is Event.AttemptDone -> engine.onAttemptDone(event.generation, event.outcome)
        is Event.GraceElapsed -> engine.onGraceElapsed(event.token)
        is Event.TunEstablished -> engine.onTunEstablished(event.generation, event.skipped)
        is Event.RestrictedStart -> engine.onRestrictedStart(event.message, event.seq)
        Event.KeepAliveRefused -> engine.onKeepAliveRefused()
        is Event.Abandoned -> engine.onCommandAbandoned(event.seq)
        Event.Poke -> engine.onPoke()
        Event.Revoked -> engine.onRevoked()
    }

    // Every effect runs even when an earlier one failed: a failing Publish must not skip the DisconnectCore behind it.
    private suspend fun run(effects: List<SessionEffect>) {
        for (effect in effects) {
            try {
                exec(effect)
            } catch (e: CancellationException) {
                throw e
            } catch (e: Throwable) {
                Log.e(TAG, "effect " + effect.javaClass.simpleName + " failed: " + e.javaClass.simpleName)
            }
        }
    }

    private suspend fun exec(effect: SessionEffect) {
        run {
            when (effect) {
                is SessionEffect.StartAttempt -> launchAttempt(effect)
                SessionEffect.CancelAttempt -> attemptJob?.cancel()
                is SessionEffect.ScheduleRetry -> {
                    retryJob?.cancel()
                    retryJob = scope.launch {
                        delay(effect.delayMs)
                        post(Event.Auto(AutoEvent.TimerFired))
                    }
                }
                SessionEffect.CancelRetry -> retryJob?.cancel()
                is SessionEffect.WatchNetwork -> watchNetwork(effect.enabled)
                is SessionEffect.StartGrace -> {
                    graceJob?.cancel()
                    graceJob = scope.launch {
                        delay(effect.delayMs)
                        post(Event.GraceElapsed(effect.token))
                    }
                }
                SessionEffect.CancelGrace -> graceJob?.cancel()
                is SessionEffect.ScheduleHold -> {
                    holdJob?.cancel()
                    holdJob = scope.launch {
                        delay(effect.delayMs)
                        post(Event.Poke)
                    }
                }
                SessionEffect.CancelHold -> holdJob?.cancel()
                is SessionEffect.ScheduleStable -> {
                    stableJob?.cancel()
                    stableJob = scope.launch {
                        delay(effect.delayMs + 1)
                        post(Event.Poke)
                    }
                }
                SessionEffect.KillProcess -> {
                    // The Go core cannot be stopped: a fresh process is the only way out. The failure is already recorded.
                    Log.e(TAG, "core stop hung; ending the process")
                    android.os.Process.killProcess(android.os.Process.myPid())
                }
                is SessionEffect.ReleaseTun ->
                    if (effect.onlyGeneration == null) platform.releaseTun() else platform.releaseTunIfCurrent(effect.onlyGeneration)
                SessionEffect.DisconnectCore -> {
                    var cut: CutLoose? = null
                    try {
                        cut = disconnectCore()
                    } finally {
                        // Whatever happened to the stop, the engine must leave "disconnecting" and the interface must go.
                        withContext(NonCancellable) {
                            if (cut != null) run(engine.onCoreCutLoose(timedOut = cut.timedOut))
                            run(engine.onDisconnectDone())
                        }
                    }
                }
                is SessionEffect.Alert -> alert(effect.alert)
                SessionEffect.CancelAlert -> VpnNotifications.cancelAlert(this)
                is SessionEffect.SetForeground -> setForeground(effect.wanted)
                SessionEffect.StopSelf -> if (lastStartId > 0) stopSelf(lastStartId) else stopSelf()
                is SessionEffect.Publish -> publishNow(effect.status)
            }
        }
    }

    private fun alert(kind: AlertKind) {
        when (kind) {
            AlertKind.PermissionNeeded -> VpnNotifications.postPermissionNeeded(this)
            is AlertKind.Failure -> VpnNotifications.postFailure(this, kind.kind, kind.message)
            AlertKind.Stopped -> VpnNotifications.postStopped(this)
            AlertKind.ProtectionOff -> VpnNotifications.postProtectionOff(this)
        }
    }

    private fun setForeground(wanted: Boolean) {
        // The core can be up while the service was demoted (a restricted start); take the state back when allowed.
        if (wanted && !fg.isForeground) fg.enter(firstModel()) else if (!wanted && fg.isForeground) fg.leave()
    }

    private fun firstModel(): NotificationModel =
        if (busy()) NotificationModel(VpnState.IDLE, busy = true) else NotificationModel(VpnState.CONNECTING)

    private fun publishNow(next: VpnStatus) {
        binder.broadcast(next)
        fg.offer(NotificationModel(next.state, next.error, busy = busy()))
        checkLockdown(next.state)
    }

    /** The interface whose app list was checked against the lockdown state, and when a rebuild was last asked for. */
    private var lockdownCheckedFor = -1
    private var lockdownRebuildAtMs = Long.MIN_VALUE / 2
    private var lockdownRebuilds = 0

    /**
     * When an interface is up: if it was built for another lockdown state than the real one ([VpnPlatform.lockdownStale],
     * typically the first interface of an Always-on start under "Block connections without VPN"), rebuild it once, as
     * "Reconnect" does. Under lockdown the rebuild cannot let traffic out past the tunnel. At most one per [LOCKDOWN_REBUILD_GAP_MS] and [LOCKDOWN_REBUILDS_MAX] for the life of the service: a state that keeps
     * disagreeing is not chased for ever.
     */
    @Synchronized
    private fun checkLockdown(state: VpnState) {
        if (state != VpnState.CONNECTED) return
        val generation = platform.tunGeneration()
        if (generation == lockdownCheckedFor) return
        if (!platform.lockdownStale()) {
            lockdownCheckedFor = generation
            return
        }
        // Not marked as checked: a rebuild that another command overtook is asked for again, not before the gap is over.
        val now = SystemClock.elapsedRealtime()
        if (now - lockdownRebuildAtMs < LOCKDOWN_REBUILD_GAP_MS || lockdownRebuilds >= LOCKDOWN_REBUILDS_MAX) return
        lockdownRebuildAtMs = now
        lockdownRebuilds++
        Log.i(TAG, "the interface was built for another lockdown state; rebuilding it once")
        userCommands.reconnect(0)
    }

    // ---- attempts and the core

    private fun launchAttempt(start: SessionEffect.StartAttempt) {
        attemptJob?.cancel()
        tunFailure = null
        attemptJob = scope.launch(Dispatchers.IO) {
            val outcome = try {
                val c = ensureCore()
                if (start.disconnectFirst) {
                    runCatching { withTimeoutOrNull(DISCONNECT_TIMEOUT_MS) { c.coreDisconnect(DisconnectRequest(collectStats = false)) } }
                }
                val result = withTimeoutOrNull(ATTEMPT_TIMEOUT_MS) { ConnectAttempt.run(ClientCoreApi(c), start.profile) }
                    ?: throw ApiException(ConnectCode.DEADLINE_EXCEEDED, "connect timed out")
                AttemptOutcome.Success(result)
            } catch (e: CancellationException) {
                throw e
            } catch (e: Exception) {
                AttemptOutcome.Failure(ConnectAttempt.classify(e, tunFailure))
            } catch (e: LinkageError) {
                AttemptOutcome.Failure(ConnectAttempt.classify(CoreStartException("core failed to start: ${e.message}", e)))
            }
            post(Event.AttemptDone(start.generation, outcome))
        }
    }

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

    /** One subscription per core, kept open and re-seeded for as long as this client is the one in use. */
    private fun startEventStream(c: LavrayClient) {
        eventsJob.set(
            scope.launch(Dispatchers.IO) {
                CoreWatcher.keep(ClientCoreApi(c), { post(Event.Core(it)) }, { client === c })
            },
        )
    }

    private class CutLoose(val timedOut: Boolean)

    /** Asks the core to stop within a bound; a core that does not answer is cut loose. Null when it answered. */
    private suspend fun disconnectCore(): CutLoose? {
        val c = client ?: return null
        val answered = try {
            withTimeoutOrNull(DISCONNECT_TIMEOUT_MS) { c.coreDisconnect(DisconnectRequest(collectStats = false)) }
        } catch (e: CancellationException) {
            throw e
        } catch (_: Exception) {
            // Not running, or the core is already gone: the tunnel is down either way.
            Unit
        }
        return if (answered == null) forceStopCore() else null
    }

    /**
     * Stops the Go core from underneath, with a bound: a hung Mobile.stop() runs on a thread of its own and must never
     * freeze the event loop. The client is dropped at once (the UI's token is void), the next connect starts a new core.
     */
    private suspend fun forceStopCore(): CutLoose {
        Log.w(TAG, "core did not answer Disconnect in time; stopping it")
        eventsJob.cancel()
        coreMutex.withLock {
            client = null
            endpoint = null
        }
        val stopper = scope.async(Dispatchers.IO) { core.stop() }
        val stopped = withTimeoutOrNull(STOP_TIMEOUT_MS) { stopper.await() } != null
        return CutLoose(timedOut = !stopped)
    }

    private fun watchNetwork(enabled: Boolean) {
        if (!enabled) {
            watcher.stop()
            return
        }
        watcher.start()
        // Registering may have missed a change that happened since the machine last looked.
        val up = watcher.isUp()
        if (up != engine.believesNetworkUp) {
            post(Event.Auto(if (up) AutoEvent.NetworkAvailable else AutoEvent.NetworkLost))
        }
    }

    /** Aligns the boot receiver with the preference; a refused enable is retried a few times, never abandoned silently. */
    private suspend fun reconcileBootReceiver() {
        var attempt = 0
        while (true) {
            val outcome = bootSetting.reconcile()
            val wait = SwitchRetry.delayMs(outcome, attempt++) ?: return
            delay(wait)
        }
    }

    private companion object {
        const val TAG = "LavrayVpn"

        /** Two automatic rebuilds for the lockdown state never come closer than this. */
        const val LOCKDOWN_REBUILD_GAP_MS = 30_000L
        const val LOCKDOWN_REBUILDS_MAX = 3
        val live = AtomicInteger(0)
        const val ATTEMPT_TIMEOUT_MS = 90_000L
        const val DISCONNECT_TIMEOUT_MS = 15_000L
        const val STOP_TIMEOUT_MS = 15_000L
        const val FOREGROUND_CONFIRM_MS = 3_000L
    }
}
