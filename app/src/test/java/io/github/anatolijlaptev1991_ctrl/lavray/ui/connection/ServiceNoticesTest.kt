// SPDX-License-Identifier: GPL-3.0-only
package io.github.anatolijlaptev1991_ctrl.lavray.ui.connection

import io.github.anatolijlaptev1991_ctrl.lavray.R
import io.github.anatolijlaptev1991_ctrl.lavray.ipc.FailureKind
import io.github.anatolijlaptev1991_ctrl.lavray.ipc.VpnState
import io.github.anatolijlaptev1991_ctrl.lavray.ipc.VpnStatus
import io.github.anatolijlaptev1991_ctrl.lavray.ui.screens.connection.Dismissed
import io.github.anatolijlaptev1991_ctrl.lavray.ui.screens.connection.ErrorDetail
import io.github.anatolijlaptev1991_ctrl.lavray.ui.screens.connection.FailureInfo
import io.github.anatolijlaptev1991_ctrl.lavray.ui.screens.connection.LinkHealth
import io.github.anatolijlaptev1991_ctrl.lavray.ui.screens.connection.Phase
import io.github.anatolijlaptev1991_ctrl.lavray.ui.screens.connection.ServiceNotices
import io.github.anatolijlaptev1991_ctrl.lavray.ui.screens.connection.WarningNote
import io.github.anatolijlaptev1991_ctrl.lavray.ui.screens.connection.phaseOf
import io.github.anatolijlaptev1991_ctrl.lavray.ui.screens.connection.stateSubRes
import io.github.anatolijlaptev1991_ctrl.lavray.ui.screens.connection.warningNotes
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNotNull
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test

/** What the Connection screen says about the service's status fields: nothing the service reports stays invisible. */
class ServiceNoticesTest {
    private fun notices(status: VpnStatus, dismissed: Dismissed = Dismissed(), tunnel: Int = 1, notificationsOn: Boolean = true) =
        ServiceNotices.of(status, phaseOf(status.state), tunnel, dismissed, notificationsOn)

    private val stopped = VpnStatus(VpnState.IDLE, unexpectedStop = true, stopReason = "low_memory")

    @Test fun anUnexpectedStopIsShownWithItsReasonAndWhatToDo() {
        val n = notices(stopped).stop!!
        assertEquals(R.string.nt_stop_memory, n.reason)
        assertTrue("memory kills are the case the battery hint is for", n.tip)
        assertFalse(n.notificationsOff)
    }

    @Test fun withNotificationsDeniedTheNoticeSaysThisIsTheOnlyPlace() {
        assertTrue(notices(stopped, notificationsOn = false).stop!!.notificationsOff)
    }

    @Test fun everyExitReasonOfTheServiceHasASentence() {
        val reasons = listOf(
            "low_memory", "crash", "crash_native", "anr", "user_stopped", "user_requested", "package_updated",
            "process_died", "tunnel_lost", "freezer", "excessive_resource_usage", "initialization_failure", null, "something_new",
        )
        reasons.forEach { r -> assertNotNull(r, notices(stopped.copy(stopReason = r)).stop) }
        assertEquals(R.string.nt_stop_crash, notices(stopped.copy(stopReason = "crash_native")).stop!!.reason)
        assertEquals(R.string.nt_stop_user, notices(stopped.copy(stopReason = "user_stopped")).stop!!.reason)
        assertEquals(R.string.nt_stop_lost, notices(stopped.copy(stopReason = "tunnel_lost")).stop!!.reason)
        assertEquals("an unknown code still says something true", R.string.nt_stop_other, notices(stopped.copy(stopReason = "something_new")).stop!!.reason)
    }

    @Test fun aDismissedStopStaysHiddenUntilItsCauseEndsAndTheNextOneIsShown() {
        val gone = Dismissed().withStop()
        assertNull(notices(stopped, gone).stop)
        // The service reports the same stop again and again: still hidden.
        assertNull(notices(stopped.copy(error = "x"), gone.after(stopped)).stop)
        // The tunnel came back (the flag is cleared): the dismissal is forgotten, so the next stop is shown.
        // The service process was replaced: a default IDLE arrives first and must not bring the closed notice back.
        assertTrue(gone.after(VpnStatus.IDLE).stop)
        val after = gone.after(VpnStatus(VpnState.CONNECTED))
        assertFalse(after.stop)
        assertNotNull(notices(stopped, after).stop)
    }

    @Test fun whileTheServiceBringsTheTunnelBackNothingIsSaidAboutAStop() {
        assertNull(notices(stopped.copy(state = VpnState.CONNECTING)).stop)
        assertNull(notices(stopped.copy(state = VpnState.CONNECTED)).stop)
        assertNull(notices(stopped.copy(state = VpnState.WAITING_NETWORK)).stop)
        assertNotNull(notices(stopped.copy(state = VpnState.ERROR, error = "x")).stop)
    }

    @Test fun aMissingPermissionStartsTheConsentFromTheStateItself() {
        assertTrue(notices(VpnStatus(VpnState.NEEDS_PERMISSION, needsPermission = true)).needsPermission)
        // Even a stale flag of the service without the state, and the state without the flag, count.
        assertTrue(notices(VpnStatus(VpnState.NEEDS_PERMISSION)).needsPermission)
        assertTrue(notices(VpnStatus(VpnState.IDLE, needsPermission = true)).needsPermission)
        assertFalse(notices(VpnStatus(VpnState.IDLE)).needsPermission)
        assertEquals(R.string.fail_permission, stateSubRes(Phase.NeedsPermission, LinkHealth.Unknown))
    }

    @Test fun heldTrafficIsSaidWhereTheStateIsShownAndNotMistakenForASlowConnection() {
        val held = VpnStatus(VpnState.CONNECTING, blackholeUntilMs = 123L)
        val n = notices(held)
        assertTrue(n.held)
        assertEquals(R.string.nt_held_sub, stateSubRes(Phase.Connecting, LinkHealth.Unknown, n.failure, n.held))
        assertEquals(R.string.conn_state_connecting_sub, stateSubRes(Phase.Connecting, LinkHealth.Unknown, FailureInfo.None, held = false))
        assertFalse(notices(VpnStatus(VpnState.CONNECTING)).held)
    }

    @Test fun whenTheHoldRanOutProtectionIsOffIsShownWhateverTheStateIs() {
        val off = VpnStatus(VpnState.CONNECTING, failureKind = FailureKind.PROTECTION_OFF, lastFailure = "x")
        val silent = notices(off).silentFailure!!
        assertEquals(R.string.fail_protection_off, silent.title)
        // The hold itself, still running, is not "protection off".
        assertNull(notices(off.copy(blackholeUntilMs = 5L)).silentFailure)
        // In the ERROR state the state card words it already.
        assertNull(notices(off.copy(state = VpnState.ERROR, error = "x")).silentFailure)
        assertEquals("dismissed once, hidden until it ends", null, notices(off, Dismissed().withFailure(FailureKind.PROTECTION_OFF)).silentFailure)
        // A default IDLE (all a freshly bound controller knows) proves nothing: the notice stays closed. A connected tunnel ends it.
        assertEquals(FailureKind.PROTECTION_OFF, Dismissed().withFailure(FailureKind.PROTECTION_OFF).after(VpnStatus(VpnState.IDLE)).failureKind)
        assertEquals(null, Dismissed().withFailure(FailureKind.PROTECTION_OFF).after(VpnStatus(VpnState.CONNECTED)).failureKind)
        assertEquals(null, Dismissed().withFailure(FailureKind.PROTECTION_OFF).after(VpnStatus(VpnState.ERROR, failureKind = "connect_failed")).failureKind)
    }

    @Test fun aCoreThatWouldNotStopIsToldOnceTheTunnelIsDown() {
        val timeout = VpnStatus(VpnState.IDLE, failureKind = FailureKind.CORE_STOP_TIMEOUT)
        assertEquals(R.string.fail_core_stop_timeout, notices(timeout).silentFailure!!.title)
        assertNull("not while the person has connected again", notices(timeout.copy(state = VpnState.CONNECTING)).silentFailure)
    }

    @Test fun aFailureIsWordedByItsKindThenByTheCoreReason() {
        assertEquals(R.string.fail_repeated_crash, ErrorDetail.kindText("repeated_crash"))
        assertEquals(R.string.fail_core_stop_timeout, ErrorDetail.kindText("core_stop_timeout"))
        assertEquals(R.string.fail_no_server, ErrorDetail.kindText(null, "no_server"))
        assertEquals("the kind wins over the reason", R.string.fail_connect_failed, ErrorDetail.kindText("connect_failed", "no_server"))
        assertNull(ErrorDetail.kindText("brand_new", "also_new"))
        val n = notices(VpnStatus(VpnState.ERROR, error = "boom", lastFailure = "old", failureKind = "connect_failed", failureReason = "no_server"))
        assertEquals(FailureInfo("connect_failed", "boom", "no_server"), n.failure)
        assertEquals("the persisted text stands in when the state carries none", "old", notices(VpnStatus(VpnState.IDLE, lastFailure = "old")).failure.raw)
    }

    @Test fun theNetworkHintFitsOnlyAFailureToReachTheServer() {
        assertTrue(ErrorDetail.suggestsNetworkCheck(null))
        assertTrue(ErrorDetail.suggestsNetworkCheck("connect_failed"))
        assertFalse(ErrorDetail.suggestsNetworkCheck("no_server"))
        assertFalse(ErrorDetail.suggestsNetworkCheck("repeated_crash"))
        assertFalse(ErrorDetail.suggestsNetworkCheck("apps_none"))
    }

    @Test fun theCoreNotesAreShownOncePerConnectionAndOnlyWhileConnected() {
        val up = VpnStatus(VpnState.CONNECTED, warnings = listOf("custom_unconfirmed: custom-поля не применяются", "tun_mixed_enabled: слушатель", "weird_code: x"))
        val shown = notices(up, tunnel = 2).warnings
        assertEquals(listOf(WarningNote(R.string.nt_warn_custom), WarningNote(R.string.nt_warn_mixed), WarningNote(R.string.nt_warn_other, "weird_code")), shown)
        assertTrue("closed for this tunnel", notices(up, Dismissed().withWarnings(2), tunnel = 2).warnings.isEmpty())
        assertEquals("a rebuilt tunnel is another connection", 3, notices(up, Dismissed().withWarnings(2), tunnel = 3).warnings.size)
        assertTrue(notices(up.copy(state = VpnState.CONNECTING), tunnel = 2).warnings.isEmpty())
    }

    @Test fun aNoteWithoutACodeIsNeverShownAsASentenceInTheCoresLanguage() {
        assertEquals(listOf(WarningNote(R.string.nt_warn_other, "unknown")), warningNotes(listOf("Просто предложение без кода")))
        assertEquals(emptyList<WarningNote>(), warningNotes(listOf("  ", "")))
        assertEquals("duplicates are one note", 1, warningNotes(listOf("tun_mixed_enabled: a", "tun_mixed_enabled: b")).size)
    }
}
