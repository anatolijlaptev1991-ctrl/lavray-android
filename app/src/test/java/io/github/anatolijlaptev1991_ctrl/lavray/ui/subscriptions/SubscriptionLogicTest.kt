// SPDX-License-Identifier: GPL-3.0-only
package io.github.anatolijlaptev1991_ctrl.lavray.ui.subscriptions

import io.github.anatolijlaptev1991_ctrl.lavray.api.gen.Group
import io.github.anatolijlaptev1991_ctrl.lavray.api.gen.SubscriptionInfo
import io.github.anatolijlaptev1991_ctrl.lavray.api.gen.SubscriptionProgress.Phase
import io.github.anatolijlaptev1991_ctrl.lavray.ui.screens.subscriptions.RelUnit
import io.github.anatolijlaptev1991_ctrl.lavray.ui.screens.subscriptions.RelativeSpan
import io.github.anatolijlaptev1991_ctrl.lavray.ui.screens.subscriptions.RelativeTime
import io.github.anatolijlaptev1991_ctrl.lavray.ui.screens.subscriptions.SubErrorKind
import io.github.anatolijlaptev1991_ctrl.lavray.ui.screens.subscriptions.SubFailure
import io.github.anatolijlaptev1991_ctrl.lavray.ui.screens.subscriptions.SubRow
import io.github.anatolijlaptev1991_ctrl.lavray.ui.screens.subscriptions.SubscriptionLogic
import io.github.anatolijlaptev1991_ctrl.lavray.ui.screens.subscriptions.UpdatePhase
import io.github.anatolijlaptev1991_ctrl.lavray.ui.screens.subscriptions.UpdateTally
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test

class SubscriptionLogicTest {
    @Test
    fun errorCodeTakesTheHttpStatusFromTheText() {
        assertEquals("404", SubscriptionLogic.errorCode("GET failed: HTTP 404 Not Found"))
        assertEquals("403", SubscriptionLogic.errorCode("403 forbidden"))
        assertEquals("401", SubscriptionLogic.errorCode("unauthorized"))
        assertEquals("404", SubscriptionLogic.errorCode("the page was not found"))
    }

    @Test
    fun errorCodeRecognisesTimeoutAndEmptyAnswers() {
        assertEquals("timeout", SubscriptionLogic.errorCode("context deadline exceeded"))
        assertEquals("timeout", SubscriptionLogic.errorCode("Сервер не ответил"))
        assertEquals("zero", SubscriptionLogic.errorCode("parsed 0 servers"))
        assertEquals("error", SubscriptionLogic.errorCode("something odd"))
        assertEquals("error", SubscriptionLogic.errorCode(""))
    }

    @Test
    fun errorKindsMatchTheWordingOfTheDesktop() {
        assertEquals(SubErrorKind.Auth, SubscriptionLogic.kindOf("401"))
        assertEquals(SubErrorKind.Auth, SubscriptionLogic.kindOf("403"))
        assertEquals(SubErrorKind.NotFound, SubscriptionLogic.kindOf("404"))
        assertEquals(SubErrorKind.Timeout, SubscriptionLogic.kindOf("timeout"))
        assertEquals(SubErrorKind.Zero, SubscriptionLogic.kindOf("zero"))
        assertEquals(SubErrorKind.Generic, SubscriptionLogic.kindOf("500"))
        assertTrue(SubscriptionLogic.failureOf("HTTP 502").showCode)
        assertFalse(SubscriptionLogic.failureOf("???").showCode)
    }

    @Test
    fun nextUpdateUsesTheOwnIntervalThenTheProvidersAndNothingWhenOff() {
        val last = 1_000_000L
        assertEquals(last + 120 * 60_000L, SubscriptionLogic.nextUpdateMs(false, last, 120, 6))
        assertEquals(last + 6 * 3_600_000L, SubscriptionLogic.nextUpdateMs(false, last, 0, 6))
        assertEquals(last + 6 * 3_600_000L, SubscriptionLogic.nextUpdateMs(false, last, null, 6))
        assertNull(SubscriptionLogic.nextUpdateMs(true, last, 120, 6))
        assertNull(SubscriptionLogic.nextUpdateMs(false, null, 120, 6))
        assertNull(SubscriptionLogic.nextUpdateMs(false, last, null, null))
    }

    @Test
    fun nearestHoursSnapsToThePresetsAndDefaultsToSix() {
        assertEquals(6, SubscriptionLogic.nearestHours(0))
        assertEquals(1, SubscriptionLogic.nearestHours(30))
        assertEquals(3, SubscriptionLogic.nearestHours(200))
        assertEquals(24, SubscriptionLogic.nearestHours(60 * 100))
    }

    @Test
    fun expiryAndQuotaFigures() {
        val now = 10_000_000_000L
        assertTrue(SubscriptionLogic.expiresSoon(now + 2 * SubscriptionLogic.DAY_MS, now))
        assertFalse(SubscriptionLogic.expiresSoon(now + 4 * SubscriptionLogic.DAY_MS, now))
        assertFalse(SubscriptionLogic.expiresSoon(0, now))
        assertEquals(0, SubscriptionLogic.usedPercent(500, 0))
        assertEquals(25, SubscriptionLogic.usedPercent(250, 1000))
        assertEquals(100, SubscriptionLogic.usedPercent(5000, 1000))
    }

    @Test
    fun displayUrlHidesTheTokenAndTheCredentials() {
        assertEquals("sub.example.com/api/v1?…", SubscriptionLogic.displayUrl("https://sub.example.com/api/v1?token=SECRET"))
        assertEquals("sub.example.com", SubscriptionLogic.displayUrl("https://user:pw@sub.example.com"))
        assertEquals("sub.example.com/…", SubscriptionLogic.displayUrl("https://sub.example.com?token=SECRET"))
        val long = SubscriptionLogic.displayUrl("https://sub.example.com/a/very/long/path/with/a/token/ABCDEFGHIJKLMNOP")
        assertTrue(long.endsWith("…"))
        assertFalse(long.contains("ABCDEFGHIJKLMNOP"))
        assertFalse(SubscriptionLogic.displayUrl("https://u:SECRET@h.example/x").contains("SECRET"))
    }

    @Test
    fun linksAndHosts() {
        assertTrue(SubscriptionLogic.isWebLink(" HTTPS://a.example/x"))
        assertFalse(SubscriptionLogic.isWebLink("javascript:alert(1)"))
        assertFalse(SubscriptionLogic.isWebLink("vless://abc"))
        assertEquals("sub.example.com", SubscriptionLogic.hostOf("https://u:p@sub.example.com:8443/x?y=1"))
        assertEquals("", SubscriptionLogic.hostOf("not a url"))
    }

    @Test
    fun updateAllTallyCountsTheFinalFramePerSubscription() {
        val phases = listOf(
            Phase.PHASE_STARTED, Phase.PHASE_REQUESTING, Phase.PHASE_DONE,
            Phase.PHASE_STARTED, Phase.PHASE_FAILED,
            Phase.PHASE_SKIPPED, Phase.PHASE_PARSING, Phase.PHASE_CONFIRM_REQUIRED, Phase.PHASE_DONE,
        )
        val t = phases.fold(UpdateTally()) { acc, p -> SubscriptionLogic.tally(acc, p) }
        assertEquals(UpdateTally(ok = 2, skip = 1, fail = 1, confirm = 1), t)
    }

    @Test
    fun onlyFollowablePhasesAreShown() {
        assertEquals(UpdatePhase.Requesting, SubscriptionLogic.phaseOf(Phase.PHASE_REQUESTING))
        assertEquals(UpdatePhase.Decoding, SubscriptionLogic.phaseOf(Phase.PHASE_DECRYPTING_HAPP))
        assertNull(SubscriptionLogic.phaseOf(Phase.PHASE_DONE))
        assertNull(SubscriptionLogic.phaseOf(Phase.PHASE_STARTED))
    }

    @Test
    fun relativeSpanPicksTheUnitAsTheDesktopDoes() {
        val now = 1_000_000_000L
        assertEquals(RelativeSpan(30, RelUnit.Minute), RelativeTime.span(now + 30 * 60_000L, now))
        assertEquals(RelativeSpan(-2, RelUnit.Hour), RelativeTime.span(now - 2 * 3_600_000L, now))
        assertEquals(RelativeSpan(3, RelUnit.Day), RelativeTime.span(now + 3 * 86_400_000L, now))
        assertEquals(RelativeSpan(0, RelUnit.Minute), RelativeTime.span(now - 20_000L, now))
        assertEquals(RelativeSpan(1, RelUnit.Minute), RelativeTime.span(now + 30_000L, now))
    }

    @Test
    fun rowMapsAGroupWithTrafficExpiryAndTheNextUpdate() {
        val g = Group(
            id = 7, name = "Provider", subscriptionUrl = "https://p.example/s", lastUpdateMs = 1_000L,
            updateIntervalMin = 60, userAgent = "clash.meta",
            subscriptionInfo = SubscriptionInfo(uploadBytes = 100, downloadBytes = 300, totalBytes = 1000, expireMs = 5_000_000L),
        )
        val row = SubRow.from(g, count = 12, failure = SubFailure.fromCode("404"))
        assertEquals(12, row.count)
        assertEquals(400L, row.usedBytes)
        assertEquals(40, row.usedPercent)
        assertEquals(1_000L + 60 * 60_000L, row.nextUpdateMs)
        assertEquals(5_000_000L, row.expireMs)
        assertEquals("clash.meta", row.userAgent)
        assertTrue(row.autoUpdate)
        assertEquals(SubErrorKind.NotFound, row.failure?.kind)
    }

    @Test
    fun rowOfASubscriptionWithoutInfoHasNoQuotaAndNoExpiry() {
        val row = SubRow.from(Group(id = 1, name = "x", skipAutoUpdate = true), count = 0)
        assertEquals(0, row.usedPercent)
        assertEquals(0L, row.expireMs)
        assertNull(row.nextUpdateMs)
        assertFalse(row.autoUpdate)
    }
}
