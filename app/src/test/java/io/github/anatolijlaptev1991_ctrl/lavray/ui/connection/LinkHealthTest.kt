// SPDX-License-Identifier: GPL-3.0-only
package io.github.anatolijlaptev1991_ctrl.lavray.ui.connection

import io.github.anatolijlaptev1991_ctrl.lavray.R
import io.github.anatolijlaptev1991_ctrl.lavray.api.CoreEndpoint
import io.github.anatolijlaptev1991_ctrl.lavray.api.gen.CoreState
import io.github.anatolijlaptev1991_ctrl.lavray.api.gen.CoreStatus
import io.github.anatolijlaptev1991_ctrl.lavray.ui.core.CoreSession
import io.github.anatolijlaptev1991_ctrl.lavray.ui.design.MarkState
import io.github.anatolijlaptev1991_ctrl.lavray.ui.screens.connection.ConnectionModel
import io.github.anatolijlaptev1991_ctrl.lavray.ui.screens.connection.ErrorDetail
import io.github.anatolijlaptev1991_ctrl.lavray.ui.screens.connection.FailureInfo
import io.github.anatolijlaptev1991_ctrl.lavray.ui.screens.connection.HealthBasis
import io.github.anatolijlaptev1991_ctrl.lavray.ui.screens.connection.HealthRules
import io.github.anatolijlaptev1991_ctrl.lavray.ui.screens.connection.HealthSignal
import io.github.anatolijlaptev1991_ctrl.lavray.ui.screens.connection.HealthSource
import io.github.anatolijlaptev1991_ctrl.lavray.ui.screens.connection.LinkHealth
import io.github.anatolijlaptev1991_ctrl.lavray.ui.screens.connection.Phase
import io.github.anatolijlaptev1991_ctrl.lavray.ui.screens.connection.markFor
import io.github.anatolijlaptev1991_ctrl.lavray.ui.screens.connection.stateSubRes
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.SupervisorJob
import kotlinx.coroutines.awaitCancellation
import kotlinx.coroutines.cancel
import kotlinx.coroutines.flow.first
import kotlinx.coroutines.flow.flow
import kotlinx.coroutines.launch
import kotlinx.coroutines.runBlocking
import kotlinx.coroutines.withTimeout
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNotNull
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test

/**
 * E-01 and review M1/L3/L4: the Connection screen must not say "connected, channel protected" when the core found that
 * nothing passes, and "verified" is tied to the tunnel it describes.
 */
class LinkHealthTest {
    private val t0 = 1_000_000L
    private fun signal(error: String? = null, connected: Boolean = true, started: Long = t0) = HealthSignal(connected, error, started)

    /** The tunnel was first seen connected 2 s after it started, with the setting [enabled]. */
    private fun basis(enabled: Boolean = true, started: Long = t0, seenAt: Long = t0 + 2_000) =
        HealthBasis.firstSight(signal(started = started), enabled, seenAt)

    @Test fun aFailureReasonFromTheCoreIsShownWhateverElseIsUnknown() {
        assertEquals(LinkHealth.NoTraffic("refused"), HealthRules.of(signal("refused"), basis(), t0 + 3_000))
        assertEquals(LinkHealth.NoTraffic("x"), HealthRules.of(signal("x"), null, t0 + 60_000))
        assertEquals(LinkHealth.NoTraffic("x"), HealthRules.of(signal("x"), basis(enabled = false), t0 + 60_000))
    }

    @Test fun theWindowIsCountedFromWhenTheTunnelWasSeenReady() {
        val b = basis(seenAt = t0 + 10_000)
        assertEquals(LinkHealth.Checking, HealthRules.of(signal(), b, t0 + 10_000 + HealthRules.CHECK_WINDOW_MS - 1))
        assertEquals(LinkHealth.Verified, HealthRules.of(signal(), b, t0 + 10_000 + HealthRules.CHECK_WINDOW_MS))
        // Not from the start of the run: a slow start must not shorten the window in which a failure can still arrive.
        assertEquals(LinkHealth.Checking, HealthRules.of(signal(), b, t0 + HealthRules.CHECK_WINDOW_MS + 5_000))
    }

    @Test fun aTunnelConnectedWithTheCheckOffIsNeverVerifiedEvenIfTheSettingIsOnLater() {
        // The setting was read as off when this tunnel came up; it is not consulted again.
        assertEquals(LinkHealth.NotChecked, HealthRules.of(signal(), basis(enabled = false), t0 + 600_000))
    }

    @Test fun aBasisOfAnotherTunnelDoesNotVerifyThisOne() {
        val old = basis(started = t0 - 90_000, seenAt = t0 - 88_000)
        assertEquals(LinkHealth.NotChecked, HealthRules.of(signal(started = t0), old, t0 + 600_000))
        assertEquals(LinkHealth.NotChecked, HealthRules.of(signal(), null, t0 + 600_000))
    }

    @Test fun aTunnelFirstSeenLongAfterItStartedIsNotVerifiedBecauseTheSettingMayHaveChanged() {
        val late = HealthBasis.firstSight(signal(), checkEnabled = true, nowMs = t0 + HealthRules.TRUST_MS + 1)
        assertFalse(late.trusted)
        assertEquals(LinkHealth.NotChecked, HealthRules.of(signal(), late, t0 + 600_000))
        assertTrue(basis().trusted)
        assertFalse("no start time, no trust", HealthBasis.firstSight(signal(started = 0), true, t0).trusted)
    }

    @Test fun withoutATunnelOrAFrameNothingIsKnown() {
        assertEquals(LinkHealth.Unknown, HealthRules.of(HealthSignal.None, basis(), t0))
        assertEquals(LinkHealth.Unknown, HealthRules.of(signal(connected = false), basis(), t0))
    }

    @Test fun theTimerWaitsOnlyForTheRestOfTheWindow() {
        val b = basis(seenAt = t0 + 2_000)
        assertEquals(HealthRules.CHECK_WINDOW_MS - 3_000, HealthRules.msUntilVerdict(signal(), b, t0 + 5_000))
        assertEquals(0L, HealthRules.msUntilVerdict(signal(), b, t0 + 2_000 + HealthRules.CHECK_WINDOW_MS))
        assertEquals("nothing to wait for when the state is not Checking", 0L, HealthRules.msUntilVerdict(signal(), null, t0))
        assertEquals(0L, HealthRules.msUntilVerdict(signal("x"), b, t0))
    }

    @Test fun theFourCodesOfTheCoreHaveTranslatedText() {
        assertEquals(R.string.conn_noTraffic_refused, HealthRules.reasonText("refused"))
        assertEquals(R.string.conn_noTraffic_reset, HealthRules.reasonText("reset"))
        assertEquals(R.string.conn_noTraffic_no_host, HealthRules.reasonText("no_host"))
        assertEquals(R.string.conn_noTraffic_timeout, HealthRules.reasonText("timeout"))
        assertNull("another reason is shown as the core wrote it", HealthRules.reasonText("remote error: tls: bad certificate"))
    }

    @Test fun theCardNeverSaysProtectedWhileNothingPasses() {
        assertEquals(R.string.cx_health_failed_sub, stateSubRes(Phase.On, LinkHealth.NoTraffic("reset")))
        assertEquals(R.string.cx_health_checking, stateSubRes(Phase.On, LinkHealth.Checking))
        assertEquals(R.string.conn_state_on_sub, stateSubRes(Phase.On, LinkHealth.Verified))
        assertEquals(R.string.cx_health_unknown_sub, stateSubRes(Phase.On, LinkHealth.NotChecked))
        assertEquals(R.string.cx_health_unknown_sub, stateSubRes(Phase.On, LinkHealth.Unknown))
        assertEquals(R.string.conn_state_off_sub, stateSubRes(Phase.Off, LinkHealth.Unknown))
        assertEquals(MarkState.Error, markFor(Phase.On, LinkHealth.NoTraffic("reset")))
        assertEquals(MarkState.On, markFor(Phase.On, LinkHealth.Verified))
        assertEquals(MarkState.Off, markFor(Phase.Off, LinkHealth.NoTraffic("stale")))
    }

    @Test fun aKnownFailureKindHasWordsAndUnknownOnesFallBackToTheGenericLine() {
        assertEquals(R.string.fail_no_server, stateSubRes(Phase.Error, LinkHealth.Unknown, FailureInfo("no_server", "raw")))
        assertEquals(R.string.fail_apps_none, ErrorDetail.kindText("apps_none"))
        assertEquals(R.string.fail_protection_off, ErrorDetail.kindText("protection_off"))
        assertEquals(R.string.conn_state_err_sub, stateSubRes(Phase.Error, LinkHealth.Unknown, FailureInfo(null, "something")))
        assertEquals(R.string.conn_state_err_sub, stateSubRes(Phase.Error, LinkHealth.Unknown, FailureInfo("brand_new_kind", null)))
        listOf("no_server", "core_start", "apps_none", "permission", "connect_failed", "restricted_start", "tunnel_lost", "protection_off")
            .forEach { assertNotNull(it, ErrorDetail.kindText(it)) }
    }

    @Test fun anErrorDetailIsOneShortLineWithoutAddresses() {
        assertNull(ErrorDetail.clean(null))
        assertNull(ErrorDetail.clean("  \n "))
        assertEquals("bad config: unsupported transport", ErrorDetail.clean("bad config:\n  unsupported\ttransport "))
        assertEquals(ErrorDetail.MAX, ErrorDetail.clean("x".repeat(500))!!.length)
        assertEquals("dial … failed", ErrorDetail.clean("dial 203.0.113.5:443 failed"))
        assertEquals("dial … failed", ErrorDetail.clean("dial vpn.example.com:443 failed"))
        assertEquals("dial … failed", ErrorDetail.clean("dial [2001:db8::1]:443 failed"))
        assertEquals("dial … failed", ErrorDetail.clean("dial 2001:db8::1 failed"))
        assertNull("nothing but an address is no detail", ErrorDetail.clean("203.0.113.5:443"))
    }

    @Test fun theModelFixesTheBasisOfATunnelWhenItIsFirstSeenConnected() = runBlocking {
        val frames = listOf(HealthSignal(true, null, 5_000L), HealthSignal(true, "timeout", 5_000L))
        val source = HealthSource { flow { frames.forEach { emit(it) }; awaitCancellation() } }
        val reads = java.util.concurrent.atomic.AtomicInteger()
        val scope = CoroutineScope(SupervisorJob() + Dispatchers.Default)
        val model = ConnectionModel(
            CoreSession({ CoreEndpoint("/x", "t") }), scope, source,
            healthCheckReader = { reads.incrementAndGet(); false },
            clock = { 6_000L },
        )
        val job = scope.launch { model.watchHealth() }
        withTimeout(5_000) { model.health.first { it.healthError == "timeout" } }
        val basis = withTimeout(5_000) { model.healthBasis.first { it != null } }!!
        assertEquals(HealthBasis(5_000L, checkEnabled = false, readyAtMs = 6_000L, trusted = true), basis)
        assertEquals("the setting is read once per tunnel, not once per frame", 1, reads.get())
        job.cancel()
        scope.cancel()
    }

    @Test fun anUnreadableSettingMakesTheBasisUntrusted() = runBlocking {
        val source = HealthSource { flow { emit(HealthSignal(true, null, 5_000L)); awaitCancellation() } }
        val scope = CoroutineScope(SupervisorJob() + Dispatchers.Default)
        val model = ConnectionModel(CoreSession({ CoreEndpoint("/x", "t") }), scope, source, healthCheckReader = { null }, clock = { 6_000L })
        val job = scope.launch { model.watchHealth() }
        val basis = withTimeout(5_000) { model.healthBasis.first { it != null } }!!
        assertFalse(basis.trusted)
        job.cancel()
        scope.cancel()
    }

    @Test fun leavingTheScreenKeepsTheLastFrameAndTheTunnelGoingAwayClearsIt() = runBlocking {
        val source = HealthSource { flow { emit(HealthSignal(true, "timeout", 5_000L)); awaitCancellation() } }
        val scope = CoroutineScope(SupervisorJob() + Dispatchers.Default)
        val model = ConnectionModel(CoreSession({ CoreEndpoint("/x", "t") }), scope, source, healthCheckReader = { true }, clock = { 6_000L })
        val job = scope.launch { model.watchHealth() }
        withTimeout(5_000) { model.health.first { it.healthError == "timeout" } }
        job.cancel()
        job.join()
        assertEquals("the screen was left: the last frame stays until a fresh one arrives", "timeout", model.health.value.healthError)
        model.resetHealth()
        assertEquals(HealthSignal.None, model.health.value)
        scope.cancel()
    }

    @Test fun aCoreStatusBecomesASignal() {
        val failing = CoreStatus(state = CoreState.CORE_STATE_CONNECTED, healthError = " reset ", startedAtMs = 7L)
        assertEquals(HealthSignal(true, "reset", 7L), HealthSignal.of(failing))
        assertEquals(HealthSignal(true, null, 7L), HealthSignal.of(failing.copy(healthError = "  ")))
        assertEquals(HealthSignal(true, null, 0L), HealthSignal.of(CoreStatus(state = CoreState.CORE_STATE_CONNECTED)))
        // A reason that outlived a disconnect is not shown for a tunnel that is not connected.
        assertEquals(false, HealthSignal.of(failing.copy(state = CoreState.CORE_STATE_IDLE)).connected)
    }
}
