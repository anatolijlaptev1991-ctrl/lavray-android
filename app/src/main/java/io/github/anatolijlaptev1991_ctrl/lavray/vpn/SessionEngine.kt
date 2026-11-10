// SPDX-License-Identifier: GPL-3.0-only
package io.github.anatolijlaptev1991_ctrl.lavray.vpn

import io.github.anatolijlaptev1991_ctrl.lavray.api.gen.CoreState
import io.github.anatolijlaptev1991_ctrl.lavray.api.gen.CoreStatus
import io.github.anatolijlaptev1991_ctrl.lavray.ipc.FailureKind
import io.github.anatolijlaptev1991_ctrl.lavray.ipc.VpnStatus

/**
 * The decisions of the tunnel service, free of Android types: one input in, effects out, in order. The service
 * (single event-loop coroutine) owns the clock, the threads, the Binder and the notifications and only interprets the
 * effects. Not thread-safe: the service feeds it from that one coroutine.
 *
 * What is decided here, and tested on the JVM: which commands are stale (superseded by a newer one from the person),
 * who may write the persisted wish ([TunnelIntent]), when a restart by the system resumes, what a core drop means,
 * when the interface is held or released, when the service is foreground and when it stops.
 */
internal class SessionEngine(
    private val env: SessionEnv,
    private val wish: TunnelIntent,
    private val machine: AutoConnectMachine = AutoConnectMachine(),
    private val watch: LinkWatch = LinkWatch(),
    private val blackhole: BlackholePolicy = BlackholePolicy(),
) {
    private var coreStatus: CoreStatus? = null
    private var requestedProfile: Long? = null
    private var attemptRunning = false
    private var attemptProfile: Long? = null
    private var attemptGeneration = 0
    private var disconnectFirst = false
    /** A deliberate disconnect waits for the core (the interface is still held until [onDisconnectDone]). */
    var disconnecting = false
        private set
    private var connectedSince: Long? = null
    private var knownTunGeneration = 0
    /**
     * The interface generation of the last run that reached CONNECTED. A hold ends when the core is active on a generation
     * above it. The reference is taken at CONNECTED and not when the hold begins: an interface that is reused is reported
     * microseconds into the new run, so its TunEstablished can be handled before the old run's IDLE has arrived, and a
     * hold that began after it would wait for an interface that is already there, to the cap and a false "protection off".
     */
    private var settledTunGeneration = 0
    private var holdUntilMono = 0L
    private var blackholeUntilWall = 0L
    private var skippedPackages: List<String> = emptyList()
    private var warnings: List<String> = emptyList()
    private var coreEpoch = 0
    private var tunnelSeq = 0
    @Volatile private var killPending = false
    /** A Connect arrived while the kill was deferred: the next process carries it out, so this one must not stop itself and clear the wish. */
    private var resumeInNextProcess = false
    /** The newest command number of the person this engine has looked at; a larger [SessionEnv.latestUserSeq] means one is still queued. */
    private var handledSeq = 0L
    private var lastFailureReasonFromAttempt: String? = null

    val phase: AutoPhase get() = machine.state.phase

    /** What the machine last believed about the network, for the service to reconcile with the real one. */
    val believesNetworkUp: Boolean get() = machine.state.networkAvailable
    val established: Boolean get() = watch.established

    /**
     * The Go core of this process cannot be used again: its stop never returned and holds the core lock, and the process
     * leaves as soon as the busy hold ends. Read by Binder threads, so a call that needs the core fails at once.
     */
    val coreUnusable: Boolean get() = killPending

    /** The process starts: a wish left over from a dead process of this boot becomes an unexpected stop. */
    fun onCreated(exitReason: () -> String?) {
        wish.onProcessStarted(exitReason)
        if (wish.snapshot.needsPermission && env.consentGranted()) wish.onPermissionGranted()
    }

    // ---- commands

    /**
     * Connect. [seq] is the number of the person's command; a USER connect that a newer command from the person
     * has superseded is dropped (its Disconnect, or the next Connect, follows in the queue).
     */
    fun onConnect(source: StartSource, profile: Long?, seq: Long): List<SessionEffect> {
        handledSeq = maxOf(handledSeq, seq)
        if (source == StartSource.USER && seq < env.latestUserSeq()) return finish(Out())
        // The newest command settles the wish: its binder write and its number are taken by two threads without a lock,
        // so the write may have been overtaken by an older command's; a stale command never reaches this line.
        return connect(source, profile, reconnecting = false, reassertWish = source == StartSource.USER)
    }

    fun onReconnect(profile: Long?, seq: Long): List<SessionEffect> {
        handledSeq = maxOf(handledSeq, seq)
        if (seq < env.latestUserSeq()) return finish(Out())
        val source = if (machine.state.retries) StartSource.SYSTEM else StartSource.USER
        return connect(source, profile, reconnecting = true, reassertWish = true)
    }

    private fun connect(source: StartSource, profile: Long?, reconnecting: Boolean, reassertWish: Boolean = false): List<SessionEffect> {
        val out = Out()
        val consent = env.consentGranted()
        // The core of this process is gone for good and the process only waits for a busy hold to end: an attempt would hang
        // on the core lock and show "connecting" for as long as the hold lasts. The wish is kept (the next process resumes
        // it, which is what the person asked for) and the reason stays on the screen.
        if (killPending) {
            if (consent && (source != StartSource.USER || reassertWish)) wish.onConnectRequested()
            wish.onFailureNoted(FailureKind.CORE_STOP_TIMEOUT, STOP_TIMEOUT_MESSAGE)
            // The service stays started, so the system restarts it after the kill and the fresh process resumes the wish.
            resumeInNextProcess = true
            return finish(out)
        }
        // Only entry points that carry intent write the wish: the person's tap did (binder), boot did (receiver).
        // The system's Always-on carries it here, and so does a boot or update start: its receiver wrote the wish in this
        // same fresh process BEFORE the service was created, so the process start read that wish as a death nobody
        // decided; asserting it again here takes the false "stopped unexpectedly" back before anything is published.
        // A USER start executes a request that was already written, and re-asserts it only when it is the newest command
        // of the person (see [reassertWish]).
        if (consent && (source != StartSource.USER || reassertWish)) wish.onConnectRequested()
        if (profile != null) requestedProfile = profile
        // A rebuild asks the core to stop first; harmless when it is idle, and it never skips the switch.
        disconnectFirst = reconnecting && consent
        if (!reconnecting) {
            watch.reset().forEach { out.link(it) }
            blackhole.reset()
            endHold(out)
        }
        warnings = emptyList()
        out += SessionEffect.CancelAlert
        apply(machine.handle(AutoEvent.Start(source, consent, env.networkUp())), out)
        return finish(out)
    }

    /** A deliberate Disconnect; a stale one (a newer command from the person exists) is dropped. */
    fun onDisconnect(seq: Long): List<SessionEffect> {
        handledSeq = maxOf(handledSeq, seq)
        if (seq < env.latestUserSeq()) return finish(Out())
        // Re-applied here: a result that was queued before this command must not leave the wish set.
        wish.onUserDisconnect()
        return stop(AutoEvent.ManualDisconnect)
    }

    fun onRevoked(): List<SessionEffect> {
        wish.onRevoked()
        return stop(AutoEvent.ConsentRevoked)
    }

    private fun stop(reason: AutoEvent): List<SessionEffect> {
        val out = Out()
        resumeInNextProcess = false
        apply(machine.handle(reason), out)
        attemptGeneration++
        cancelAttempt(out)
        watch.reset().forEach { out.link(it) }
        blackhole.reset()
        endHold(out)
        disconnecting = true
        out += SessionEffect.Publish(computeStatus())
        out += SessionEffect.DisconnectCore
        return out.toList()
    }

    /** The core answered Disconnect (or did not, and was cut loose): the interface goes now. */
    fun onDisconnectDone(): List<SessionEffect> {
        disconnecting = false
        coreStatus = coreStatus?.copy(state = CoreState.CORE_STATE_IDLE, lastError = null)
        skippedPackages = emptyList()
        warnings = emptyList()
        val out = Out()
        out += SessionEffect.ReleaseTun(null)
        return finish(out)
    }

    /** The system restarted the service after the process died (null intent). */
    fun onResume(): List<SessionEffect> {
        val out = Out()
        when (ResumePolicy.decide(wish.snapshot, env.consentGranted())) {
            ResumeAction.CONNECT -> {
                wish.onResumeAttempt()
                return connect(StartSource.SYSTEM, null, reconnecting = false)
            }
            ResumeAction.STOP_QUIET -> Unit
            ResumeAction.NEEDS_PERMISSION -> {
                wish.onPermissionMissing()
                out += SessionEffect.Alert(AlertKind.PermissionNeeded)
            }
            ResumeAction.GIVE_UP -> {
                // The process died again and again before the tunnel came up: stop the restart loop.
                wish.onGaveUp(FailureKind.REPEATED_CRASH, null, wasEstablished = false)
                out += SessionEffect.Alert(AlertKind.Failure(FailureKind.REPEATED_CRASH, null))
            }
        }
        return finish(out)
    }

    /** The foreground promise of a start could not be kept: nothing may be started, the failure is recorded and shown. */
    fun onRestrictedStart(message: String?, seq: Long = 0): List<SessionEffect> {
        handledSeq = maxOf(handledSeq, seq)
        val out = Out()
        wish.onGaveUp(FailureKind.RESTRICTED_START, message, wasEstablished = false)
        out += SessionEffect.Alert(AlertKind.Failure(FailureKind.RESTRICTED_START, message))
        return endStartedState(finish(out))
    }

    /**
     * A keep-alive start was refused foreground: the promise of a foreground service is unmet, so the started state
     * must end (a bound UI keeps the binding, which is not what the promise is about).
     */
    fun onKeepAliveRefused(): List<SessionEffect> = endStartedState(finish(Out()))

    /** Ends the started state unless a tunnel runs: stopping then would take the tunnel down with it. */
    private fun endStartedState(effects: List<SessionEffect>): List<SessionEffect> {
        if (isCoreActive() || machine.state.pending || SessionEffect.StopSelf in effects) return effects
        return effects + SessionEffect.StopSelf
    }

    // ---- events

    fun onCore(status: CoreStatus): List<SessionEffect> {
        val out = Out()
        applyCore(status, out)
        return finish(out)
    }

    private fun applyCore(status: CoreStatus, out: Out) {
        val before = coreStatus?.state
        coreStatus = status
        // Every transition to CONNECTED is a new tunnel (a rebuild, a core-driven restart, a reconnect after a drop).
        if (status.state == CoreState.CORE_STATE_CONNECTED && before != CoreState.CORE_STATE_CONNECTED) {
            tunnelSeq++
            // A run's TunEstablished is always handled before its CONNECTED (it is posted from OpenTun, on the way there).
            settledTunGeneration = knownTunGeneration
        }
        // The warnings belong to the connect that made the tunnel; the core does not report them in its status, so a
        // restart the core did on its own (RestartProfile from the UI) leaves them unknown, and unknown is shown as none.
        if (before == CoreState.CORE_STATE_CONNECTED && status.state != CoreState.CORE_STATE_CONNECTED) warnings = emptyList()
        // A core that finished connecting just before it saw our Disconnect delivers CONNECTED, DISCONNECTING, IDLE
        // late: for a session the person ended they are not news (no grace period, no "tunnel lost").
        if (disconnecting || machine.state.phase == AutoPhase.STOPPED) return
        watch.onCore(status.state, attemptRunning).forEach { out.link(it) }
    }

    fun onAttemptDone(generation: Int, outcome: AttemptOutcome): List<SessionEffect> {
        // A cancelled or replaced attempt must not overwrite anything.
        if (generation != attemptGeneration) return finish(Out())
        attemptRunning = false
        val out = Out()
        when (outcome) {
            is AttemptOutcome.Success -> {
                applyCore(outcome.result.status, out)
                // Also when the status stream skipped the frames in between and the engine saw no transition.
                settledTunGeneration = knownTunGeneration
                warnings = outcome.result.warnings
                wish.onConnected()
                apply(machine.handle(AutoEvent.AttemptSucceeded), out)
            }
            is AttemptOutcome.Failure -> {
                val f = outcome.failure
                lastFailureReasonFromAttempt = f.reason
                apply(machine.handle(AutoEvent.AttemptFailed(f.permanent, f.message, f.kind)), out)
            }
        }
        return finish(out)
    }

    fun onGraceElapsed(token: Int): List<SessionEffect> {
        val out = Out()
        watch.onGraceElapsed(token).forEach { out.link(it) }
        return finish(out)
    }

    fun onTunEstablished(generation: Int, skipped: List<String>): List<SessionEffect> {
        knownTunGeneration = generation
        skippedPackages = skipped
        return finish(Out())
    }

    fun onAuto(event: AutoEvent): List<SessionEffect> {
        val out = Out()
        apply(machine.handle(event), out)
        return finish(out)
    }

    /** The Go core was cut loose from underneath (it did not stop in time): the UI must fetch the endpoint again. */
    fun onCoreCutLoose(timedOut: Boolean): List<SessionEffect> {
        coreEpoch++
        coreStatus = coreStatus?.copy(state = CoreState.CORE_STATE_IDLE, lastError = null)
        val out = Out()
        if (timedOut) {
            wish.onFailureNoted(FailureKind.CORE_STOP_TIMEOUT, STOP_TIMEOUT_MESSAGE)
            out += SessionEffect.Alert(AlertKind.Failure(FailureKind.CORE_STOP_TIMEOUT, STOP_TIMEOUT_MESSAGE))
        }
        // A Go runtime whose stop never returns holds the core lock for good: no later start in this process can
        // succeed. The failure is recorded and published; a fresh process is the only way out. The kill waits while a busy
        // hold is in force (a restore under way is not sacrificed on a guess); a Connect written meanwhile is resumed by
        // the new process, which is what the person asked for.
        if (timedOut) killPending = true
        return finish(out)
    }

    /** A command of the person that will never reach the loop (its start was refused): it no longer counts as queued. */
    fun onCommandAbandoned(seq: Long): List<SessionEffect> {
        handledSeq = maxOf(handledSeq, seq)
        return finish(Out())
    }

    /** A clock tick, a Binder bind or unbind, a busy-hold change: nothing to decide, everything to re-evaluate. */
    fun onPoke(): List<SessionEffect> = finish(Out())

    // ---- decisions

    private fun apply(effects: List<AutoEffect>, out: Out) {
        for (effect in effects) {
            when (effect) {
                AutoEffect.StartAttempt -> launchAttempt(out)
                is AutoEffect.ScheduleRetry -> out += SessionEffect.ScheduleRetry(effect.delayMs)
                AutoEffect.CancelRetry -> out += SessionEffect.CancelRetry
                is AutoEffect.WatchNetwork -> out += SessionEffect.WatchNetwork(effect.enabled)
                AutoEffect.NotifyNeedsPermission -> {
                    wish.onPermissionMissing()
                    attemptGeneration++
                    cancelAttempt(out)
                    out += SessionEffect.Alert(AlertKind.PermissionNeeded)
                }
                is AutoEffect.Fail -> giveUp(effect.kind ?: FailureKind.CONNECT_FAILED, effect.message, wasEstablished = false, out)
            }
        }
    }

    private fun launchAttempt(out: Out) {
        val profile = requestedProfile
        // The same profile already in flight is joined, not cancelled: two starts at boot (the receiver and the
        // system's Always-on) must not abort the first connect. What the second start asked for is already under way.
        if (AttemptGate.decide(attemptRunning, attemptProfile, profile) == AttemptDecision.KEEP) {
            disconnectFirst = false
            return
        }
        cancelAttempt(out)
        attemptRunning = true
        attemptProfile = profile
        lastFailureReasonFromAttempt = null
        val generation = ++attemptGeneration
        val first = disconnectFirst
        disconnectFirst = false
        out += SessionEffect.StartAttempt(generation, profile, first)
    }

    private fun cancelAttempt(out: Out) {
        if (!attemptRunning) return
        attemptRunning = false
        out += SessionEffect.CancelAttempt
    }

    private fun Out.link(effect: LinkEffect) {
        when (effect) {
            is LinkEffect.StartGrace -> this += SessionEffect.StartGrace(effect.token, effect.delayMs)
            LinkEffect.CancelGrace -> this += SessionEffect.CancelGrace
            LinkEffect.Lost -> onTunnelLost(this)
        }
    }

    private fun onTunnelLost(out: Out) {
        apply(machine.handle(AutoEvent.ConnectionLost), out)
        // No retry follows (a user start, or a state that cannot retry): the tunnel ended on its own.
        if (!machine.state.pending) giveUp(FailureKind.TUNNEL_LOST, coreStatus?.lastError, wasEstablished = true, out)
    }

    /** The service stops trying: persist why, and say so when nobody is looking at the app. */
    private fun giveUp(kind: String, message: String?, wasEstablished: Boolean, out: Out) {
        wish.onGaveUp(kind, message, wasEstablished, reason = lastFailureReasonFromAttempt.takeUnless { wasEstablished })
        if (env.uiBound()) return
        out += SessionEffect.Alert(if (wasEstablished) AlertKind.Stopped else AlertKind.Failure(kind, message))
    }

    /** After every input: hold or release the interface, foreground, status, and stop when nothing needs the process. */
    private fun finish(out: Out): List<SessionEffect> {
        reconcileHold(out)
        val active = isCoreActive()
        val pending = machine.state.pending
        // A command of the person that is still queued will start (or stop) things: this is not the moment to leave.
        val commandQueued = env.latestUserSeq() > handledSeq
        val transition = watch.inGrace || holdUntilMono != 0L || commandQueued
        val busy = env.busy()
        trackStability(out)
        out += SessionEffect.SetForeground(ShutdownPolicy.shouldBeForeground(active, pending, transition, busy))
        out += SessionEffect.Publish(computeStatus())
        // A Connect handed to the next process is work still to do: stopping here would clear the wish it relies on.
        val stop = !resumeInNextProcess && ShutdownPolicy.shouldStop(active, env.uiBound(), pending, transition, busy)
        if (stop) {
            // Whenever the service stops on its own the wish is cleared, so a fresh process never reads a stale one.
            // Nothing went wrong, so no failure is recorded.
            if (wish.snapshot.wantConnected) wish.onServiceStopped()
            out += SessionEffect.StopSelf
        }
        if (killPending && !busy) {
            killPending = false
            out += SessionEffect.KillProcess
        }
        return out.toList()
    }

    /**
     * The crash counter of [ResumePolicy] resets only once the tunnel has stayed CONNECTED for [STABLE_MS]: a native
     * crash a few seconds after CONNECTED must still count.
     */
    private fun trackStability(out: Out) {
        val connected = coreStatus?.state == CoreState.CORE_STATE_CONNECTED && !disconnecting && machine.state.phase != AutoPhase.STOPPED
        if (!connected) {
            connectedSince = null
            return
        }
        val now = env.nowMono()
        val since = connectedSince
        if (since == null) {
            connectedSince = now
            out += SessionEffect.ScheduleStable(STABLE_MS)
        } else if (now - since >= STABLE_MS && wish.snapshot.resumeAttempts > 0) {
            wish.onStable()
        }
    }

    /** The rule is [BlackholePolicy]: keep the interface blocked only while the app itself rebuilds an established tunnel. */
    private fun reconcileHold(out: Out) {
        val now = env.nowMono()
        val input = HoldInput(
            tunHeld = env.tunHeld(),
            coreActive = isCoreActive(),
            established = watch.established,
            inGrace = watch.inGrace,
            phase = machine.state.phase,
            newInterface = knownTunGeneration > settledTunGeneration,
        )
        when (val decision = blackhole.evaluate(now, input)) {
            HoldDecision.None -> endHold(out)
            is HoldDecision.Hold -> beginHold(now, decision.untilMs, out)
            is HoldDecision.Release -> {
                endHold(out)
                // Against the generation this engine knows: a newer interface, built while this input was on its way, stays.
                out += SessionEffect.ReleaseTun(knownTunGeneration)
                if (decision.capExpired) {
                    wish.onFailureNoted(FailureKind.PROTECTION_OFF, null)
                    out += SessionEffect.Alert(AlertKind.ProtectionOff)
                }
            }
        }
    }

    private fun beginHold(now: Long, untilMono: Long, out: Out) {
        if (holdUntilMono == untilMono) return
        holdUntilMono = untilMono
        blackholeUntilWall = env.nowWall() + (untilMono - now)
        out += SessionEffect.ScheduleHold(untilMono - now)
    }

    private fun endHold(out: Out) {
        if (holdUntilMono == 0L) return
        holdUntilMono = 0
        blackholeUntilWall = 0
        out += SessionEffect.CancelHold
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
                failureReason = saved.failureReason,
                warnings = warnings,
                coreEpoch = coreEpoch,
                tunnelSeq = tunnelSeq,
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

    /** The effects collected by one input, in order. */
    companion object {
        /** How long a tunnel must stay CONNECTED before the crash counter of [ResumePolicy] is cleared. */
        const val STABLE_MS = 3 * 60_000L
        const val STOP_TIMEOUT_MESSAGE = "the core did not stop in time"
    }

    private class Out {
        private val list = ArrayList<SessionEffect>()

        operator fun plusAssign(effect: SessionEffect) {
            list += effect
        }

        fun toList(): List<SessionEffect> = list.toList()
    }
}
