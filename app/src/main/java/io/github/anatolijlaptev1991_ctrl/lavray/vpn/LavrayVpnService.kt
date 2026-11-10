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
import io.github.anatolijlaptev1991_ctrl.lavray.api.gen.CoreState
import io.github.anatolijlaptev1991_ctrl.lavray.api.gen.CoreStatus
import io.github.anatolijlaptev1991_ctrl.lavray.api.gen.DisconnectRequest
import io.github.anatolijlaptev1991_ctrl.lavray.api.gen.coreDisconnect
import io.github.anatolijlaptev1991_ctrl.lavray.api.gen.coreSubscribeEvents
import io.github.anatolijlaptev1991_ctrl.lavray.ipc.FailureKind
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
 * The tunnel, in its own process (:vpn). One coroutine consumes every event in order, so the auto-connect
 * machine and the status fields have a single writer. Nothing here polls, schedules alarms or holds a wake
 * lock; the only timers are the backoff delays, the grace period after a core drop and the blackhole cap.
 *
 * The decisions live in small testable classes: [AutoConnectMachine] (retry), [LinkWatch] (is a core drop final),
 * [BlackholePolicy] (keep the interface blocked during a transition), [TunnelIntent] (what survives a process
 * death), [StatusModel] (what the UI is told), [ShutdownPolicy] (when the process may leave).
 */
class LavrayVpnService : VpnService(), VpnBinderHost {
    private sealed interface Outcome {
        data class Success(val status: CoreStatus) : Outcome
        data class Failure(val failure: AttemptFailure) : Outcome
    }

    private sealed interface Event {
        data class Command(val command: StartCommand) : Event
        data class Auto(val event: AutoEvent) : Event
        data class Core(val status: CoreStatus) : Event
        data class AttemptDone(val generation: Int, val outcome: Outcome) : Event
        data class GraceElapsed(val token: Int) : Event
        data class TunEstablished(val generation: Int, val skipped: List<String>) : Event
        data object Poke : Event
        data object Revoked : Event
    }

    private val events = Channel<Event>(Channel.UNLIMITED)
    private val scope = CoroutineScope(SupervisorJob() + Dispatchers.Default)
    private val machine = AutoConnectMachine()
    private val watch = LinkWatch()
    private val blackhole = BlackholePolicy()
    private val ui = UiBindTracker()
    private val binder = VpnBinder(this)
    private val coreMutex = Mutex()

    private lateinit var core: CoreHost
    private lateinit var platform: VpnPlatform
    private lateinit var watcher: NetworkWatcher
    private lateinit var prefs: VpnPrefs
    private lateinit var wish: TunnelIntent
    private lateinit var fg: ServiceForeground

    @Volatile private var client: LavrayClient? = null
    @Volatile private var endpoint: CoreEndpoint? = null
    @Volatile private var status: VpnStatus = VpnStatus.IDLE

    /** Set from Go threads while the interface is built; read by the attempt that asked for it. */
    @Volatile private var tunFailure: TunFailure? = null

    // The fields below are touched only by the event loop.
    private var coreStatus: CoreStatus? = null
    private var requestedProfile: Long? = null
    private var attemptJob: Job? = null
    private var attemptProfile: Long? = null
    private var attemptGeneration = 0
    private var disconnectFirst = false
    private var retryJob: Job? = null
    private var graceJob: Job? = null
    private var holdJob: Job? = null
    private var eventsJob: Job? = null
    private var disconnecting = false
    private var knownTunGeneration = 0
    private var skippedPackages: List<String> = emptyList()
    private var holdUntilMono = 0L
    private var blackholeUntilWall = 0L

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
        VpnNotifications.ensureChannels(this)
        prefs = VpnPrefs(this)
        wish = TunnelIntent(prefs)
        fg = ServiceForeground(this)
        // A stale "wanted" flag in a fresh process means the last process died without anyone deciding it.
        wish.onProcessStarted(ExitReasons.lastVpnExit(this))
        if (wish.snapshot.needsPermission && VpnService.prepare(this) == null) wish.onPermissionGranted()
        val cm = getSystemService(ConnectivityManager::class.java)
        val openApp = PendingIntent.getActivity(this, 1, Intent(this, MainActivity::class.java), PendingIntent.FLAG_IMMUTABLE)
        platform = VpnPlatform(
            this,
            cm,
            TunEstablisher(this, getString(R.string.app_name), openApp) { prefs.appSelection },
            platformEvents,
        )
        core = CoreHost(this, platform)
        watcher = NetworkWatcher(
            cm,
            onAvailable = { post(Event.Auto(AutoEvent.NetworkAvailable)) },
            onLost = { post(Event.Auto(AutoEvent.NetworkLost)) },
        )
        fg.registerScreenReceiver()
        scope.launch(Dispatchers.IO) { BootComponents.reconcile(this@LavrayVpnService, prefs.autoConnect) }
        scope.launch { for (event in events) handle(event) }
    }

    override fun onStartCommand(intent: Intent?, flags: Int, startId: Int): Int {
        val command = StartIntent.parse(
            intent?.action,
            intent?.getStringExtra(StartIntent.EXTRA_SOURCE),
            intent?.getLongExtra(StartIntent.EXTRA_PROFILE_ID, 0L) ?: 0L,
            hasIntent = intent != null,
        )
        when (command) {
            // A start that was promised to become foreground must do so within seconds, whatever follows.
            is StartCommand.Connect, is StartCommand.Reconnect -> {
                if (!fg.enter()) failRestrictedStart()
            }
            StartCommand.Disconnect -> this.wish.onUserDisconnect()
            StartCommand.Resume -> fg.enter()
        }
        post(Event.Command(command))
        // Sticky: after the process dies the system restarts the service with a null intent, and [StartCommand.Resume]
        // reconnects only if the tunnel was wanted and consent is still there; otherwise it stops at once.
        return START_STICKY
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
        fg.unregisterScreenReceiver()
        watcher.stop()
        platform.close()
        binder.kill()
        scope.cancel()
        live.decrementAndGet()
        val host = core
        // Stop the core and leave: this process exists only for the tunnel. A new
        // service instance created meanwhile keeps the process alive and owns its own core.
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

    override fun connect(profileId: Long) = startViaIntent(null, profileId)

    override fun reconnect(profileId: Long) = startViaIntent(StartIntent.ACTION_RECONNECT, profileId)

    private fun startViaIntent(action: String?, profileId: Long) {
        // Written before the start, with commit(): a process that dies a moment later must still be restored.
        wish.onConnectRequested()
        val start = Intent(this, LavrayVpnService::class.java)
            .putExtra(StartIntent.EXTRA_SOURCE, StartSource.USER.wire)
            .putExtra(StartIntent.EXTRA_PROFILE_ID, profileId)
        if (action != null) start.setAction(action)
        try {
            startForegroundService(start)
        } catch (e: RuntimeException) {
            wish.onGaveUp(FailureKind.RESTRICTED_START, e.message, wasEstablished = false)
            publishNow(VpnStatus(VpnState.ERROR, e.message, lastFailure = e.message, failureKind = FailureKind.RESTRICTED_START))
        }
    }

    override fun disconnect() {
        wish.onUserDisconnect()
        post(Event.Command(StartCommand.Disconnect))
    }

    override fun currentStatus(): VpnStatus = status

    override var autoConnect: Boolean
        get() = prefs.autoConnect
        set(value) {
            prefs.autoConnect = value
            // The boot receiver exists only while the setting is on; a failed switch leaves it on, never silently off.
            BootComponents.reconcile(this, value)
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
        guarded("event") { dispatch(event) }
        guarded("reconcile") { reconcile() }
        guarded("publish") { publish() }
    }

    // One bad event must not end the loop: the tunnel outlives any single handler's failure.
    private inline fun guarded(what: String, block: () -> Unit) {
        try {
            block()
        } catch (e: CancellationException) {
            throw e
        } catch (e: Throwable) {
            Log.e(TAG, "$what failed: ${e.javaClass.simpleName}")
            runCatching { publishNow(VpnStatus(VpnState.ERROR, e.message ?: e.javaClass.simpleName)) }
        }
    }

    private suspend fun dispatch(event: Event) {
        when (event) {
            is Event.Command -> when (val c = event.command) {
                is StartCommand.Connect -> startConnect(c.source, c.profileId, reconnecting = false)
                is StartCommand.Reconnect -> startConnect(StartSource.USER, c.profileId, reconnecting = true)
                StartCommand.Disconnect -> stopConnection(AutoEvent.ManualDisconnect)
                StartCommand.Resume -> resume()
            }
            is Event.Auto -> apply(machine.handle(event.event))
            is Event.Core -> onCoreStatus(event.status)
            is Event.AttemptDone -> onAttemptDone(event)
            is Event.GraceElapsed -> applyLink(watch.onGraceElapsed(event.token))
            is Event.TunEstablished -> {
                knownTunGeneration = event.generation
                skippedPackages = event.skipped
            }
            Event.Poke -> Unit
            Event.Revoked -> {
                wish.onRevoked()
                stopConnection(AutoEvent.ConsentRevoked)
            }
        }
    }

    private fun resume() {
        val wanted = wish.snapshot.wantConnected
        if (!wanted || VpnService.prepare(this) != null) {
            // Nobody asked for the tunnel (or consent is gone): do not hold the process for nothing.
            if (wanted) wish.onPermissionMissing()
            return
        }
        startConnect(StartSource.SYSTEM, null, reconnecting = false)
    }

    private fun startConnect(source: StartSource, profileId: Long?, reconnecting: Boolean) {
        val consent = VpnService.prepare(this) == null
        if (consent) wish.onConnectRequested()
        if (profileId != null) requestedProfile = profileId
        // A reconnect keeps the interface blocked; the rebuild first asks the core to stop.
        disconnectFirst = reconnecting && consent && watch.established
        if (!reconnecting) {
            watch.reset().forEach { applyLinkEffect(it) }
            blackhole.reset()
        }
        VpnNotifications.cancelAlert(this)
        apply(machine.handle(AutoEvent.Start(source, consent, watcher.isUp())))
    }

    private suspend fun stopConnection(reason: AutoEvent) {
        apply(machine.handle(reason))
        attemptGeneration++
        attemptJob?.cancel()
        watch.reset().forEach { applyLinkEffect(it) }
        blackhole.reset()
        holdJob?.cancel()
        blackholeUntilWall = 0
        // Show "disconnecting" while the core is asked; the answer is bounded.
        disconnecting = true
        publishNow(computeStatus())
        try {
            disconnectCore()
        } finally {
            disconnecting = false
        }
        coreStatus = coreStatus?.copy(state = CoreState.CORE_STATE_IDLE, lastError = null)
        skippedPackages = emptyList()
        platform.releaseTun()
    }

    /** Asks the core to stop; a core that does not answer in time is stopped from underneath. */
    private suspend fun disconnectCore() {
        val c = client ?: return
        val answered = try {
            withTimeoutOrNull(DISCONNECT_TIMEOUT_MS) { c.coreDisconnect(DisconnectRequest(collectStats = false)) }
        } catch (e: CancellationException) {
            throw e
        } catch (_: Exception) {
            // Not running, or the core is already gone: the tunnel is down either way.
            Unit
        }
        if (answered == null) forceStopCore()
    }

    private suspend fun forceStopCore() {
        Log.w(TAG, "core did not answer Disconnect in time; stopping it")
        eventsJob?.cancel()
        coreMutex.withLock {
            client = null
            endpoint = null
            withContext(Dispatchers.IO) { core.stop() }
        }
    }

    private fun onCoreStatus(next: CoreStatus) {
        coreStatus = next
        applyLink(watch.onCore(next.state, attemptRunning = attemptJob?.isActive == true))
    }

    private fun applyLink(effects: List<LinkEffect>) = effects.forEach { applyLinkEffect(it) }

    private fun applyLinkEffect(effect: LinkEffect) {
        when (effect) {
            is LinkEffect.StartGrace -> {
                graceJob?.cancel()
                graceJob = scope.launch {
                    delay(effect.delayMs)
                    post(Event.GraceElapsed(effect.token))
                }
            }
            LinkEffect.CancelGrace -> graceJob?.cancel()
            LinkEffect.Lost -> onTunnelLost()
        }
    }

    private fun onTunnelLost() {
        apply(machine.handle(AutoEvent.ConnectionLost))
        // No retry follows (a user start, or a state that cannot retry): the tunnel ended on its own.
        if (!machine.state.pending) giveUp(FailureKind.TUNNEL_LOST, coreStatus?.lastError, wasEstablished = true)
    }

    private fun onAttemptDone(done: Event.AttemptDone) {
        if (done.generation != attemptGeneration) return // a cancelled or replaced attempt must not overwrite anything
        when (val outcome = done.outcome) {
            is Outcome.Success -> {
                onCoreStatus(outcome.status)
                wish.onConnected()
                apply(machine.handle(AutoEvent.AttemptSucceeded))
            }
            is Outcome.Failure -> {
                val f = outcome.failure
                apply(machine.handle(AutoEvent.AttemptFailed(f.permanent, f.message, f.kind)))
            }
        }
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
                    wish.onPermissionMissing()
                    attemptGeneration++
                    attemptJob?.cancel()
                    VpnNotifications.postPermissionNeeded(this)
                }
                is AutoEffect.Fail -> giveUp(effect.kind ?: FailureKind.CONNECT_FAILED, effect.message, wasEstablished = false)
            }
        }
    }

    /** The service stops trying: persist why, and say so when nobody is looking at the app. */
    private fun giveUp(kind: String, message: String?, wasEstablished: Boolean) {
        wish.onGaveUp(kind, message, wasEstablished)
        if (ui.bound) return
        if (wasEstablished) VpnNotifications.postStopped(this) else VpnNotifications.postFailure(this, kind, message)
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
        val profile = requestedProfile
        // The same profile already in flight is joined, not cancelled: two starts at boot (the receiver and the
        // system's Always-on) must not abort the first connect.
        if (AttemptGate.decide(attemptJob?.isActive == true, attemptProfile, profile) == AttemptDecision.KEEP) return
        attemptJob?.cancel()
        val generation = ++attemptGeneration
        attemptProfile = profile
        tunFailure = null
        val reconnecting = disconnectFirst
        disconnectFirst = false
        attemptJob = scope.launch(Dispatchers.IO) {
            val outcome = try {
                val c = ensureCore()
                if (eventsJob?.isActive != true) startEventStream(c)
                if (reconnecting) runCatching { withTimeoutOrNull(DISCONNECT_TIMEOUT_MS) { c.coreDisconnect(DisconnectRequest(collectStats = false)) } }
                val status = withTimeoutOrNull(ATTEMPT_TIMEOUT_MS) { ConnectAttempt.run(ClientCoreApi(c), profile) }
                    ?: throw ApiException(ConnectCode.DEADLINE_EXCEEDED, "connect timed out")
                Outcome.Success(status)
            } catch (e: CancellationException) {
                throw e
            } catch (e: Exception) {
                Outcome.Failure(ConnectAttempt.classify(e, tunFailure))
            } catch (e: LinkageError) {
                Outcome.Failure(ConnectAttempt.classify(CoreStartException("core failed to start: ${e.message}", e)))
            }
            post(Event.AttemptDone(generation, outcome))
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

    // ---- interface hold, status, lifecycle

    /** The rule is [BlackholePolicy]: keep the interface blocked only while the app itself rebuilds an established tunnel. */
    private fun reconcile() {
        val now = SystemClock.elapsedRealtime()
        val input = HoldInput(platform.isTunHeld(), isCoreActive(), watch.established, watch.inGrace, machine.state.phase)
        when (val decision = blackhole.evaluate(now, input)) {
            HoldDecision.None -> endHold()
            is HoldDecision.Hold -> beginHold(now, decision.untilMs)
            is HoldDecision.Release -> {
                endHold()
                // Against the generation this loop knows: a newer interface (built while this event was on its way) stays.
                platform.releaseTunIfCurrent(knownTunGeneration)
                if (decision.capExpired) onBlackholeCapExpired()
            }
        }
    }

    private fun beginHold(now: Long, untilMono: Long) {
        if (holdUntilMono == untilMono) return
        holdUntilMono = untilMono
        blackholeUntilWall = System.currentTimeMillis() + (untilMono - now)
        holdJob?.cancel()
        holdJob = scope.launch {
            delay(untilMono - now)
            post(Event.Poke)
        }
    }

    private fun endHold() {
        if (holdUntilMono == 0L) return
        holdUntilMono = 0
        blackholeUntilWall = 0
        holdJob?.cancel()
    }

    private fun onBlackholeCapExpired() {
        wish.onFailureNoted(FailureKind.PROTECTION_OFF, null)
        VpnNotifications.postProtectionOff(this)
    }

    private fun publish() {
        val active = isCoreActive()
        val pending = machine.state.pending
        val transition = watch.inGrace || holdUntilMono != 0L
        if (ShutdownPolicy.shouldBeForeground(active, pending, transition)) {
            // The core can be up while the service was demoted (a restricted start); take the state back when allowed.
            if (!fg.isForeground) fg.enter(computeStatus().state)
        } else if (fg.isForeground) {
            fg.leave()
        }
        publishNow(computeStatus())
        if (ShutdownPolicy.shouldStop(active, ui.bound, pending, transition)) {
            // Whenever the service stops on its own the wish is cleared, so a fresh process never reads a stale one.
            if (wish.snapshot.wantConnected) wish.onGaveUp(FailureKind.TUNNEL_LOST, null, wasEstablished = false)
            stopSelf()
        }
    }

    private fun publishNow(next: VpnStatus) {
        status = next
        binder.broadcast(next)
        fg.offer(NotificationModel(next.state, next.error))
    }

    private fun computeStatus(): VpnStatus {
        val saved = wish.snapshot
        return StatusModel.compute(
            StatusInputs(
                coreState = coreStatus?.state,
                coreError = coreStatus?.lastError,
                healthError = coreStatus?.healthError,
                phase = machine.state.phase,
                needsPermission = saved.needsPermission,
                lastFailure = saved.lastFailure,
                failureKind = saved.failureKind,
                inGrace = watch.inGrace,
                disconnecting = disconnecting,
                unexpectedStop = saved.unexpectedStop,
                stopReason = saved.stopReason,
                skippedPackages = skippedPackages,
                blackholeUntilMs = blackholeUntilWall,
            ),
        )
    }

    private fun isCoreActive(): Boolean = when (coreStatus?.state) {
        CoreState.CORE_STATE_CONNECTING,
        CoreState.CORE_STATE_CONNECTED,
        CoreState.CORE_STATE_DISCONNECTING,
        CoreState.CORE_STATE_RECONNECTING -> true
        else -> false
    }

    /** A start that must be foreground could not be: report it, record it, and stand down. */
    private fun failRestrictedStart() {
        wish.onGaveUp(FailureKind.RESTRICTED_START, null, wasEstablished = false)
        VpnNotifications.postFailure(this, FailureKind.RESTRICTED_START, null)
        stopSelf()
    }

    private companion object {
        const val TAG = "LavrayVpn"
        val live = AtomicInteger(0)
        const val ATTEMPT_TIMEOUT_MS = 90_000L
        const val DISCONNECT_TIMEOUT_MS = 15_000L
    }
}
