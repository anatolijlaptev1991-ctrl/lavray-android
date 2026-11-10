// SPDX-License-Identifier: GPL-3.0-only
package io.github.anatolijlaptev1991_ctrl.lavray.ui.connection

import io.github.anatolijlaptev1991_ctrl.lavray.R
import io.github.anatolijlaptev1991_ctrl.lavray.api.gen.CoreState
import io.github.anatolijlaptev1991_ctrl.lavray.api.gen.CoreStatus
import io.github.anatolijlaptev1991_ctrl.lavray.ui.design.MarkState
import io.github.anatolijlaptev1991_ctrl.lavray.ui.screens.connection.ConnectionModel
import io.github.anatolijlaptev1991_ctrl.lavray.ui.screens.connection.ErrorDetail
import io.github.anatolijlaptev1991_ctrl.lavray.ui.screens.connection.HealthRules
import io.github.anatolijlaptev1991_ctrl.lavray.ui.screens.connection.HealthSignal
import io.github.anatolijlaptev1991_ctrl.lavray.ui.screens.connection.HealthSource
import io.github.anatolijlaptev1991_ctrl.lavray.ui.screens.connection.LinkHealth
import io.github.anatolijlaptev1991_ctrl.lavray.ui.screens.connection.Phase
import io.github.anatolijlaptev1991_ctrl.lavray.ui.screens.connection.markFor
import io.github.anatolijlaptev1991_ctrl.lavray.ui.screens.connection.stateSubRes
import io.github.anatolijlaptev1991_ctrl.lavray.ui.core.CoreSession
import io.github.anatolijlaptev1991_ctrl.lavray.api.CoreEndpoint
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
import org.junit.Assert.assertNull
import org.junit.Test

/** E-01: the Connection screen must not say "connected, channel protected" when the core found that nothing passes. */
class LinkHealthTest {
    private val t0 = 1_000_000L
    private fun signal(error: String? = null, connected: Boolean = true) = HealthSignal(connected, error, t0)

    @Test fun aFailureReasonFromTheCoreWinsOverEverythingElse() {
        assertEquals(LinkHealth.NoTraffic("refused"), HealthRules.of(signal("refused"), checkEnabled = true, nowMs = t0 + 1_000))
        assertEquals(LinkHealth.NoTraffic("x"), HealthRules.of(signal("x"), checkEnabled = true, nowMs = t0 + 60_000))
    }

    @Test fun beforeTheWindowEndsTheCheckIsStillRunning() {
        assertEquals(LinkHealth.Checking, HealthRules.of(signal(), true, t0 + 5_000))
        assertEquals(LinkHealth.Checking, HealthRules.of(signal(), true, t0 + HealthRules.CHECK_WINDOW_MS - 1))
    }

    @Test fun afterTheWindowWithoutAFailureTheLinkIsVerified() {
        assertEquals(LinkHealth.Verified, HealthRules.of(signal(), true, t0 + HealthRules.CHECK_WINDOW_MS))
    }

    @Test fun withTheCheckSwitchedOffNothingIsClaimed() {
        assertEquals(LinkHealth.NotChecked, HealthRules.of(signal(), false, t0 + 60_000))
        assertEquals(LinkHealth.NotChecked, HealthRules.of(signal(), false, t0 + 1_000))
    }

    @Test fun withoutATunnelOrAFrameNothingIsKnown() {
        assertEquals(LinkHealth.Unknown, HealthRules.of(HealthSignal.None, true, t0))
        assertEquals(LinkHealth.Unknown, HealthRules.of(signal(connected = false), true, t0))
    }

    @Test fun theTimerWaitsOnlyForTheRestOfTheWindow() {
        assertEquals(15_000L, HealthRules.msUntilVerdict(signal(), t0 + 5_000))
        assertEquals(0L, HealthRules.msUntilVerdict(signal(), t0 + 25_000))
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

    @Test fun anErrorDetailIsOneShortLine() {
        assertNull(ErrorDetail.clean(null))
        assertNull(ErrorDetail.clean("  \n "))
        assertEquals("bad config: unsupported transport", ErrorDetail.clean("bad config:\n  unsupported\ttransport "))
        assertEquals(ErrorDetail.MAX, ErrorDetail.clean("x".repeat(500))!!.length)
    }

    @Test fun theModelKeepsTheLatestFrameAndForgetsItWhenTheScreenLeaves() = runBlocking {
        val source = HealthSource {
            flow {
                emit(HealthSignal(connected = true, healthError = null, startedAtMs = 5L))
                emit(HealthSignal(connected = true, healthError = "timeout", startedAtMs = 5L))
                awaitCancellation()
            }
        }
        val scope = CoroutineScope(SupervisorJob() + Dispatchers.Default)
        val model = ConnectionModel(CoreSession({ CoreEndpoint("/x", "t") }), scope, source)
        val job = scope.launch { model.watchHealth() }
        withTimeout(5_000) { model.health.first { it.healthError == "timeout" } }
        job.cancel()
        withTimeout(5_000) { model.health.first { it == HealthSignal.None } }
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
