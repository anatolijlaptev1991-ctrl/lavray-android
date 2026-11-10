// SPDX-License-Identifier: GPL-3.0-only
package io.github.anatolijlaptev1991_ctrl.lavray.ui.connection

import io.github.anatolijlaptev1991_ctrl.lavray.R
import io.github.anatolijlaptev1991_ctrl.lavray.api.CoreEndpoint
import io.github.anatolijlaptev1991_ctrl.lavray.ipc.VpnState
import io.github.anatolijlaptev1991_ctrl.lavray.ipc.VpnStatus
import io.github.anatolijlaptev1991_ctrl.lavray.ui.core.CoreSession
import io.github.anatolijlaptev1991_ctrl.lavray.ui.design.MarkState
import io.github.anatolijlaptev1991_ctrl.lavray.ui.screens.connection.ConnectionModel
import io.github.anatolijlaptev1991_ctrl.lavray.ui.screens.connection.ErrorDetail
import io.github.anatolijlaptev1991_ctrl.lavray.ui.screens.connection.FailureInfo
import io.github.anatolijlaptev1991_ctrl.lavray.ui.screens.connection.HealthBasis
import io.github.anatolijlaptev1991_ctrl.lavray.ui.screens.connection.HealthRules
import io.github.anatolijlaptev1991_ctrl.lavray.ui.screens.connection.HealthSignal
import io.github.anatolijlaptev1991_ctrl.lavray.ui.screens.connection.TunnelSight
import io.github.anatolijlaptev1991_ctrl.lavray.ui.screens.connection.LinkHealth
import io.github.anatolijlaptev1991_ctrl.lavray.ui.screens.connection.Phase
import io.github.anatolijlaptev1991_ctrl.lavray.ui.screens.connection.markFor
import io.github.anatolijlaptev1991_ctrl.lavray.ui.screens.connection.stateSubRes
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.SupervisorJob
import kotlinx.coroutines.cancel
import kotlinx.coroutines.flow.first
import kotlinx.coroutines.flow.MutableStateFlow
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
    private fun signal(error: String? = null, connected: Boolean = true, tunnel: Int = 1) = HealthSignal(connected, error, tunnel)

    /** Tunnel [tunnel] was first seen connected 2 s after the core started it, with the setting [enabled]. */
    private fun basis(enabled: Boolean = true, tunnel: Int = 1, started: Long = t0, seenAt: Long = t0 + 2_000) =
        HealthBasis.firstSight(tunnel, started, enabled, seenAt)

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
        val old = basis(tunnel = 1, started = t0 - 90_000, seenAt = t0 - 88_000)
        assertEquals(LinkHealth.NotChecked, HealthRules.of(signal(tunnel = 2), old, t0 + 600_000))
        assertEquals(LinkHealth.NotChecked, HealthRules.of(signal(), null, t0 + 600_000))
    }

    @Test fun aTunnelFirstSeenLongAfterItStartedIsNotVerifiedBecauseTheSettingMayHaveChanged() {
        val late = HealthBasis.firstSight(1, startedAtMs = t0, checkEnabled = true, nowMs = t0 + HealthRules.TRUST_MS + 1)
        assertFalse(late.trusted)
        assertEquals(LinkHealth.NotChecked, HealthRules.of(signal(), late, t0 + 600_000))
        assertTrue(basis().trusted)
        assertFalse("no start time, no trust", HealthBasis.firstSight(1, null, true, t0).trusted)
        assertFalse("a start time of 0 is no start time", HealthBasis.firstSight(1, 0L, true, t0).trusted)
        assertFalse("an unreadable setting is no reading", HealthBasis.firstSight(1, t0, null, t0 + 1).trusted)
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
        listOf("no_server", "core_start", "apps_none", "permission", "connect_failed", "restricted_start", "tunnel_lost", "protection_off", "core_stop_timeout", "repeated_crash")
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

    private fun model(
        status: MutableStateFlow<VpnStatus>, scope: CoroutineScope,
        enabled: suspend () -> Boolean? = { true }, startedAt: suspend () -> Long? = { 5_000L }, now: Long = 6_000L,
    ) = ConnectionModel(CoreSession({ CoreEndpoint("/x", "t") }), scope, status, enabled, startedAt, clock = { now })

    private fun up(error: String? = null) = VpnStatus(VpnState.CONNECTED, healthError = error)

    @Test fun theModelFixesTheBasisOfATunnelWhenItIsFirstSeenConnected() = runBlocking {
        val status = MutableStateFlow(VpnStatus.IDLE)
        val reads = java.util.concurrent.atomic.AtomicInteger()
        val scope = CoroutineScope(SupervisorJob() + Dispatchers.Default)
        val model = model(status, scope, enabled = { reads.incrementAndGet(); false })
        status.value = up()
        val basis = withTimeout(5_000) { model.healthBasis.first { it != null } }!!
        assertEquals(HealthBasis(1, checkEnabled = false, readyAtMs = 6_000L, trusted = true, startedAtMs = 5_000L), basis)
        status.value = up("timeout")
        withTimeout(5_000) { model.health.first { it.healthError == "timeout" } }
        assertEquals("the setting is read once per tunnel, not once per status", 1, reads.get())
        scope.cancel()
    }

    @Test fun aRebuiltTunnelIsANewOneAndGetsItsOwnBasis() = runBlocking {
        val status = MutableStateFlow(VpnStatus.IDLE)
        val scope = CoroutineScope(SupervisorJob() + Dispatchers.Default)
        val model = model(status, scope)
        status.value = up()
        withTimeout(5_000) { model.healthBasis.first { it?.tunnel == 1 } }
        // The service shows CONNECTING while it rebuilds, so the next CONNECTED is another tunnel.
        status.value = VpnStatus(VpnState.CONNECTING)
        withTimeout(5_000) { model.health.first { !it.connected } }
        status.value = up()
        val second = withTimeout(5_000) { model.healthBasis.first { it?.tunnel == 2 } }
        assertEquals(2, model.health.value.tunnel)
        assertEquals(LinkHealth.Checking, HealthRules.of(model.health.value, second, 6_001L))
        scope.cancel()
    }

    @Test fun aFailureReasonOfTheServiceReachesTheSignalWithoutAnyCoreStream() = runBlocking {
        val status = MutableStateFlow(up("refused"))
        val scope = CoroutineScope(SupervisorJob() + Dispatchers.Default)
        val model = model(status, scope)
        val signal = withTimeout(5_000) { model.health.first { it.healthError != null } }
        assertEquals(HealthSignal(true, "refused", 1), signal)
        scope.cancel()
    }

    @Test fun anUnreadableSettingOrStartTimeMakesTheBasisUntrusted() = runBlocking {
        val scope = CoroutineScope(SupervisorJob() + Dispatchers.Default)
        val noSetting = MutableStateFlow(up())
        assertFalse(withTimeout(5_000) { model(noSetting, scope, enabled = { null }).healthBasis.first { it != null } }!!.trusted)
        val noStart = MutableStateFlow(up())
        assertFalse(withTimeout(5_000) { model(noStart, scope, startedAt = { null }).healthBasis.first { it != null } }!!.trusted)
        scope.cancel()
    }

    @Test fun aTunnelThatWasAlreadyOldWhenTheAppSawItIsNotTrusted() = runBlocking {
        // The app process started while the tunnel had been up for ten minutes: the setting may have changed since.
        val scope = CoroutineScope(SupervisorJob() + Dispatchers.Default)
        val status = MutableStateFlow(up())
        val m = model(status, scope, startedAt = { 5_000L }, now = 5_000L + 600_000L)
        val basis = withTimeout(5_000) { m.healthBasis.first { it != null } }!!
        assertFalse(basis.trusted)
        assertEquals(LinkHealth.NotChecked, HealthRules.of(m.health.value, basis, 5_000L + 700_000L))
        scope.cancel()
    }

    @Test fun theTunnelGoingAwayClearsTheSignal() = runBlocking {
        val status = MutableStateFlow(up("timeout"))
        val scope = CoroutineScope(SupervisorJob() + Dispatchers.Default)
        val m = model(status, scope)
        withTimeout(5_000) { m.health.first { it.healthError == "timeout" } }
        status.value = VpnStatus(VpnState.IDLE)
        val cleared = withTimeout(5_000) { m.health.first { !it.connected } }
        assertEquals(null, cleared.healthError)
        scope.cancel()
    }

    @Test fun tunnelsAreCountedOnlyWhenTheyComeUp() {
        var sight = TunnelSight()
        sight = sight.next(VpnState.CONNECTING)
        assertEquals(0, sight.tunnel)
        sight = sight.next(VpnState.CONNECTED)
        assertEquals(1, sight.tunnel)
        sight = sight.next(VpnState.CONNECTED)
        assertEquals("the same tunnel", 1, sight.tunnel)
        sight = sight.next(VpnState.CONNECTING).next(VpnState.CONNECTED)
        assertEquals(2, sight.tunnel)
        assertEquals(3, sight.next(VpnState.IDLE).next(VpnState.CONNECTED).tunnel)
    }

    @Test fun aStatusBecomesASignalAndAReasonOutsideConnectedIsNotShown() {
        assertEquals(HealthSignal(true, "reset", 2), HealthSignal.of(up(" reset "), 2))
        assertEquals(HealthSignal(true, null, 2), HealthSignal.of(up("  "), 2))
        assertEquals(HealthSignal(true, null, 2), HealthSignal.of(up(), 2))
        // A reason that outlived the tunnel is not shown for a tunnel that is not connected.
        assertEquals(HealthSignal(false, null, 2), HealthSignal.of(VpnStatus(VpnState.CONNECTING, healthError = "reset"), 2))
    }

}
