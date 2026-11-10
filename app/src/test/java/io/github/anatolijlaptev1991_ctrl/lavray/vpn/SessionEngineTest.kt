// SPDX-License-Identifier: GPL-3.0-only
package io.github.anatolijlaptev1991_ctrl.lavray.vpn

import io.github.anatolijlaptev1991_ctrl.lavray.api.gen.CoreState
import io.github.anatolijlaptev1991_ctrl.lavray.api.gen.CoreStatus
import io.github.anatolijlaptev1991_ctrl.lavray.ipc.FailureKind
import io.github.anatolijlaptev1991_ctrl.lavray.ipc.VpnState
import io.github.anatolijlaptev1991_ctrl.lavray.ipc.VpnStatus
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test

internal class FakeEnv : SessionEnv {
    var consent = true
    var network = true
    var ui = false
    var held = false
    var busy = false
    var mono = 1_000L
    var wall = 1_700_000_000_000L
    var seq = 0L
    override fun consentGranted() = consent
    override fun networkUp() = network
    override fun uiBound() = ui
    override fun tunHeld() = held
    override fun busy() = busy
    override fun nowMono() = mono
    override fun nowWall() = wall
    override fun latestUserSeq() = seq
}

private fun core(state: CoreState, error: String? = null, profile: Long? = null) =
    CoreStatus(state = state, lastError = error, activeProfileId = profile)

private fun ok(vararg warnings: String) =
    AttemptOutcome.Success(ConnectResult(core(CoreState.CORE_STATE_CONNECTED, profile = 5), warnings.toList()))

private fun fail(permanent: Boolean, kind: String = FailureKind.CONNECT_FAILED, reason: String? = null, message: String? = "boom") =
    AttemptOutcome.Failure(AttemptFailure(permanent, message, kind, reason))

internal class SessionEngineTest {
    private val env = FakeEnv()
    private val store = MemoryTunnelStore()
    private val wish = TunnelIntent(store)
    private val engine = SessionEngine(env, wish)

    // The person's tap as the binder records it: a number and the wish, before the command reaches the loop.
    private fun userConnects(): Long {
        env.seq++
        wish.onConnectRequested()
        return env.seq
    }

    private fun userDisconnects(): Long {
        env.seq++
        wish.onUserDisconnect()
        return env.seq
    }

    private fun List<SessionEffect>.started() = filterIsInstance<SessionEffect.StartAttempt>()
    private fun List<SessionEffect>.status(): VpnStatus = filterIsInstance<SessionEffect.Publish>().last().status
    private fun List<SessionEffect>.stops() = any { it == SessionEffect.StopSelf }

    // ---- H2: a deliberate Disconnect is never undone by event order

    @Test
    fun resultQueuedBeforeTheDisconnectDoesNotUndoIt() {
        val seq1 = userConnects()
        val start = engine.onConnect(StartSource.USER, 5, seq1).started().single()
        // The notification's Disconnect: written on the main thread at once, queued behind the finished attempt.
        val seq2 = userDisconnects()
        engine.onAttemptDone(start.generation, ok())
        assertFalse("a result must not set the wish", store.value.wantConnected)
        val effects = engine.onDisconnect(seq2)
        assertTrue(effects.contains(SessionEffect.DisconnectCore))
        engine.onDisconnectDone()
        assertFalse(store.value.wantConnected)
        assertNull("no spurious failure after the person's own disconnect", store.value.failureKind)
        assertFalse(store.value.unexpectedStop)
    }

    @Test
    fun connectQueuedBehindADisconnectIsDropped() {
        val seq1 = userConnects()
        val seq2 = userDisconnects()
        val effects = engine.onConnect(StartSource.USER, 5, seq1)
        assertTrue("a superseded connect starts nothing", effects.started().isEmpty())
        assertFalse(store.value.wantConnected)
        assertTrue(engine.onDisconnect(seq2).contains(SessionEffect.DisconnectCore))
        assertFalse(store.value.wantConnected)
    }

    @Test
    fun theNewestCommandWinsWhenTheyPileUp() {
        val seq1 = userConnects()
        val seq2 = userDisconnects()
        val seq3 = userConnects()
        assertTrue(engine.onConnect(StartSource.USER, 5, seq1).started().isEmpty())
        assertFalse("a stale disconnect stops nothing", engine.onDisconnect(seq2).contains(SessionEffect.DisconnectCore))
        assertTrue(store.value.wantConnected)
        assertEquals(1, engine.onConnect(StartSource.USER, 5, seq3).started().size)
        assertTrue(store.value.wantConnected)
    }

    @Test
    fun disconnectReappliesTheDisconnectWhenAnotherEntryPointSetTheWish() {
        val seq = userDisconnects()
        wish.onConnectRequested() // for example a boot start that ran in between
        engine.onDisconnect(seq)
        assertFalse(store.value.wantConnected)
    }

    @Test
    fun userStartExecutesARequestWithoutWritingTheWish() {
        env.seq = 1
        engine.onConnect(StartSource.USER, 5, 1)
        assertFalse(store.value.wantConnected)
    }

    @Test
    fun systemStartCarriesTheIntentItself() {
        engine.onConnect(StartSource.SYSTEM, null, 0)
        assertTrue(store.value.wantConnected)
    }

    // ---- Resume (START_STICKY, null intent)

    @Test
    fun resumeReconnectsAWantedTunnel() {
        store.value = PersistedTunnel(wantConnected = true)
        val effects = engine.onResume()
        assertEquals(1, effects.started().size)
        assertEquals(1, store.value.resumeAttempts)
        assertFalse(effects.stops())
    }

    @Test
    fun resumeOfAnUnwantedTunnelStopsAtOnce() {
        val effects = engine.onResume()
        assertTrue(effects.started().isEmpty())
        assertTrue(effects.stops())
        assertTrue(effects.any { it == SessionEffect.SetForeground(false) })
    }

    @Test
    fun resumeWithoutConsentSaysSo() {
        store.value = PersistedTunnel(wantConnected = true)
        env.consent = false
        val effects = engine.onResume()
        assertTrue(effects.contains(SessionEffect.Alert(AlertKind.PermissionNeeded)))
        assertTrue(store.value.needsPermission)
        assertFalse(store.value.wantConnected)
        assertTrue(effects.started().isEmpty())
        assertTrue(effects.stops())
    }

    @Test
    fun aCrashLoopBeforeTheFirstConnectedStopsRestarting() {
        store.value = PersistedTunnel(wantConnected = true)
        repeat(ResumePolicy.MAX_ATTEMPTS) { assertEquals(1, SessionEngine(env, wish).onResume().started().size) }
        val effects = SessionEngine(env, wish).onResume()
        assertTrue(effects.started().isEmpty())
        assertTrue(effects.contains(SessionEffect.Alert(AlertKind.Failure(FailureKind.REPEATED_CRASH, null))))
        assertFalse(store.value.wantConnected)
        assertEquals(FailureKind.REPEATED_CRASH, store.value.failureKind)
        assertTrue(effects.stops())
    }

    @Test
    fun aConnectedTunnelResetsTheCrashCounter() {
        store.value = PersistedTunnel(wantConnected = true, resumeAttempts = 2)
        val start = engine.onResume().started().single()
        engine.onAttemptDone(start.generation, ok())
        assertEquals(0, store.value.resumeAttempts)
    }

    // ---- a start the system refused

    @Test
    fun restrictedStartRecordsShowsAndStopsWithoutConnecting() {
        wish.onConnectRequested()
        val effects = engine.onRestrictedStart("not allowed")
        assertTrue(effects.started().isEmpty())
        assertFalse(store.value.wantConnected)
        assertEquals(FailureKind.RESTRICTED_START, store.value.failureKind)
        assertTrue(effects.contains(SessionEffect.Alert(AlertKind.Failure(FailureKind.RESTRICTED_START, "not allowed"))))
        assertTrue(effects.stops())
    }

    // ---- core drops, the grace period and the blackhole

    private fun established(): Int {
        val start = engine.onConnect(StartSource.SYSTEM, 5, 0).started().single()
        env.held = true
        engine.onTunEstablished(1, emptyList())
        engine.onAttemptDone(start.generation, ok())
        return start.generation
    }

    @Test
    fun coreRestartIsHeldAndNeverReleasesBeforeTheNewInterface() {
        established()
        var effects = engine.onCore(core(CoreState.CORE_STATE_DISCONNECTING))
        effects = engine.onCore(core(CoreState.CORE_STATE_IDLE))
        assertTrue(effects.any { it is SessionEffect.StartGrace })
        assertTrue(effects.any { it is SessionEffect.ScheduleHold })
        assertTrue(effects.none { it is SessionEffect.ReleaseTun })
        assertTrue("foreground through the grace", effects.contains(SessionEffect.SetForeground(true)))
        assertEquals(VpnState.CONNECTING, effects.status().state)
        assertTrue(effects.status().trafficBlocked)
        // The core is CONNECTING again but has not called OpenTun: the held interface is still unread, the hold counts on.
        effects = engine.onCore(core(CoreState.CORE_STATE_CONNECTING))
        assertTrue(effects.none { it is SessionEffect.ReleaseTun })
        assertTrue(effects.status().trafficBlocked)
        effects = engine.onTunEstablished(2, emptyList())
        assertTrue(effects.contains(SessionEffect.CancelHold))
        assertFalse(effects.status().trafficBlocked)
    }

    @Test
    fun holdIsACapEvenWhileTheCoreSitsInConnecting() {
        established()
        engine.onCore(core(CoreState.CORE_STATE_IDLE))
        engine.onCore(core(CoreState.CORE_STATE_CONNECTING))
        env.mono += BlackholePolicy.DEFAULT_CAP_MS
        val effects = engine.onPoke()
        assertTrue("released against the generation this engine knows", effects.contains(SessionEffect.ReleaseTun(1)))
        assertTrue(effects.contains(SessionEffect.Alert(AlertKind.ProtectionOff)))
        assertEquals(FailureKind.PROTECTION_OFF, store.value.failureKind)
        assertFalse(effects.status().trafficBlocked)
    }

    @Test
    fun graceThatEndsWithTheCoreDownRetriesForSystemStarts() {
        established()
        val grace = engine.onCore(core(CoreState.CORE_STATE_ERROR, "reset")).filterIsInstance<SessionEffect.StartGrace>().single()
        val effects = engine.onGraceElapsed(grace.token)
        assertEquals(1, effects.started().size)
        assertTrue("the first rebuild after a loss is still held", effects.none { it is SessionEffect.ReleaseTun })
    }

    @Test
    fun lostUserTunnelIsAnUnexpectedStopAndTheWishIsCleared() {
        env.seq = 1
        wish.onConnectRequested()
        val start = engine.onConnect(StartSource.USER, 5, 1).started().single()
        env.held = true
        engine.onTunEstablished(1, emptyList())
        engine.onAttemptDone(start.generation, ok())
        val grace = engine.onCore(core(CoreState.CORE_STATE_ERROR, "reset")).filterIsInstance<SessionEffect.StartGrace>().single()
        val effects = engine.onGraceElapsed(grace.token)
        assertTrue(effects.contains(SessionEffect.Alert(AlertKind.Stopped)))
        assertTrue(store.value.unexpectedStop)
        assertFalse(store.value.wantConnected)
        assertTrue(effects.status().unexpectedStop)
    }

    @Test
    fun nothingIsReleasedForAnInterfaceThatANewerGenerationReplaced() {
        // The release a late ERROR asks for is always bound to the generation the engine knows, never "whatever is held".
        env.held = true
        engine.onTunEstablished(2, emptyList())
        val effects = engine.onCore(core(CoreState.CORE_STATE_ERROR, "late"))
        assertEquals(listOf(SessionEffect.ReleaseTun(2)), effects.filterIsInstance<SessionEffect.ReleaseTun>())
    }

    // ---- attempts

    @Test
    fun sameProfileInFlightIsJoinedAndAReconnectDoesNotLeaveItsFlagBehind() {
        env.seq = 1
        engine.onConnect(StartSource.USER, 5, 1).started().single()
        env.seq = 2
        assertTrue("joined, not restarted", engine.onReconnect(5, 2).started().isEmpty())
        val start = engine.onAttemptDone(1, fail(permanent = false)).let { engine.onAuto(AutoEvent.TimerFired) }
        assertTrue(start.started().isEmpty()) // USER source: no retry, nothing scheduled
        env.seq = 3
        val next = engine.onConnect(StartSource.USER, 5, 3).started().single()
        assertFalse("L9: the skipped reconnect's flag was cleared", next.disconnectFirst)
    }

    @Test
    fun reconnectAlwaysAsksTheCoreToStopFirst() {
        env.seq = 1
        val first = engine.onConnect(StartSource.USER, 5, 1).started().single()
        engine.onAttemptDone(first.generation, ok())
        env.seq = 2
        val again = engine.onReconnect(6, 2).started().single()
        assertTrue(again.disconnectFirst)
        assertEquals(6L, again.profile)
    }

    @Test
    fun staleAttemptResultsAreIgnored() {
        env.seq = 1
        val first = engine.onConnect(StartSource.USER, 5, 1).started().single()
        env.seq = 2
        engine.onDisconnect(2)
        engine.onDisconnectDone()
        val effects = engine.onAttemptDone(first.generation, ok())
        assertEquals(VpnState.IDLE, effects.status().state)
    }

    // ---- what the UI is told

    @Test
    fun failureCarriesTheCoresReason() {
        env.seq = 1
        val start = engine.onConnect(StartSource.USER, 5, 1).started().single()
        val effects = engine.onAttemptDone(start.generation, fail(permanent = true, kind = FailureKind.NO_SERVER, reason = "no_server"))
        val status = effects.status()
        assertEquals(VpnState.ERROR, status.state)
        assertEquals(FailureKind.NO_SERVER, status.failureKind)
        assertEquals("no_server", status.failureReason)
        assertEquals("boom", status.error)
    }

    @Test
    fun warningsOfTheConnectReachTheStatus() {
        env.seq = 1
        val start = engine.onConnect(StartSource.USER, 5, 1).started().single()
        val status = engine.onAttemptDone(start.generation, ok("w1", "w2")).status()
        assertEquals(VpnState.CONNECTED, status.state)
        assertEquals(listOf("w1", "w2"), status.warnings)
        assertTrue(engine.onDisconnect(2).status().warnings.isEmpty())
    }

    @Test
    fun cutLooseCoreBumpsTheEpochAndNotesTheTimeout() {
        established()
        val effects = engine.onCoreCutLoose(timedOut = true)
        assertEquals(1, effects.status().coreEpoch)
        assertEquals(FailureKind.CORE_STOP_TIMEOUT, store.value.failureKind)
    }

    // ---- foreground and stopping

    // A skipped stale command must not stop the service (and clear the wish) while the newer command is still queued.
    @Test
    fun aQueuedCommandOfThePersonKeepsTheServiceAliveAndForeground() {
        val seq1 = userConnects()
        val seq2 = userDisconnects()
        val seq3 = userConnects()
        val effects = engine.onConnect(StartSource.USER, 5, seq1)
        assertFalse(effects.stops())
        assertTrue(store.value.wantConnected)
        assertTrue(effects.contains(SessionEffect.SetForeground(true)))
        engine.onDisconnect(seq2)
        assertEquals(1, engine.onConnect(StartSource.USER, 5, seq3).started().size)
    }

    @Test
    fun anAbandonedCommandNoLongerKeepsTheServiceAlive() {
        val seq = userConnects()
        assertFalse(engine.onPoke().stops())
        assertTrue(engine.onCommandAbandoned(seq).stops())
    }

    @Test
    fun aRefusedStartCountsAsHandled() {
        val seq = userConnects()
        assertTrue(engine.onRestrictedStart("no", seq).stops())
    }

    @Test
    fun idleUnboundServiceStopsAndABoundOneStays() {
        assertTrue(engine.onPoke().stops())
        env.ui = true
        assertFalse(engine.onPoke().stops())
    }

    @Test
    fun aBusyOperationKeepsTheProcessAndForegroundUntilItEnds() {
        env.busy = true
        val held = engine.onPoke()
        assertFalse(held.stops())
        assertTrue(held.contains(SessionEffect.SetForeground(true)))
        env.busy = false
        val done = engine.onPoke()
        assertTrue(done.stops())
        assertTrue(done.contains(SessionEffect.SetForeground(false)))
    }

    @Test
    fun stoppingOnItsOwnClearsAStaleWishWithoutBlamingAnyone() {
        wish.onConnectRequested()
        val effects = engine.onPoke()
        assertTrue(effects.stops())
        assertFalse(store.value.wantConnected)
        assertNull(store.value.failureKind)
    }

    @Test
    fun missingConsentOnAUserStartSaysSo() {
        env.consent = false
        env.seq = 1
        wish.onConnectRequested()
        val effects = engine.onConnect(StartSource.USER, 5, 1)
        assertTrue(effects.contains(SessionEffect.Alert(AlertKind.PermissionNeeded)))
        assertTrue(store.value.needsPermission)
        assertFalse(store.value.wantConnected)
        assertEquals(VpnState.NEEDS_PERMISSION, effects.status().state)
    }

    @Test
    fun revokedConsentEndsTheSessionAndAsks() {
        established()
        val effects = engine.onRevoked()
        assertTrue(effects.contains(SessionEffect.DisconnectCore))
        assertTrue(store.value.needsPermission)
        assertFalse(store.value.wantConnected)
        assertFalse(store.value.unexpectedStop)
    }

    @Test
    fun aProcessThatDiedUnderAWishIsReportedOnceItIsCreatedAgain() {
        store.value = PersistedTunnel(wantConnected = true)
        engine.onCreated { "low_memory" }
        assertTrue(store.value.unexpectedStop)
        assertEquals("low_memory", store.value.stopReason)
        assertEquals(VpnState.IDLE, engine.onPoke().status().state)
        assertTrue(engine.onPoke().status().unexpectedStop)
    }
}
