// SPDX-License-Identifier: GPL-3.0-only
package io.github.anatolijlaptev1991_ctrl.lavray.ui.subscriptions

import io.github.anatolijlaptev1991_ctrl.lavray.api.gen.Group
import io.github.anatolijlaptev1991_ctrl.lavray.ui.core.ErrorKind
import io.github.anatolijlaptev1991_ctrl.lavray.ui.screens.subscriptions.Busy
import io.github.anatolijlaptev1991_ctrl.lavray.ui.screens.subscriptions.EditForm
import io.github.anatolijlaptev1991_ctrl.lavray.ui.screens.subscriptions.PendingConfirm
import io.github.anatolijlaptev1991_ctrl.lavray.ui.screens.subscriptions.SubFailure
import io.github.anatolijlaptev1991_ctrl.lavray.ui.screens.subscriptions.SubFailureStore
import io.github.anatolijlaptev1991_ctrl.lavray.ui.screens.subscriptions.SubRow
import io.github.anatolijlaptev1991_ctrl.lavray.ui.screens.subscriptions.SubSettingKeys
import io.github.anatolijlaptev1991_ctrl.lavray.ui.screens.subscriptions.SubscriptionEdit
import io.github.anatolijlaptev1991_ctrl.lavray.ui.screens.subscriptions.SubscriptionLogic
import io.github.anatolijlaptev1991_ctrl.lavray.ui.screens.subscriptions.SubscriptionsAction
import io.github.anatolijlaptev1991_ctrl.lavray.ui.screens.subscriptions.SubscriptionsReducer
import io.github.anatolijlaptev1991_ctrl.lavray.ui.screens.subscriptions.SubscriptionsState
import io.github.anatolijlaptev1991_ctrl.lavray.ui.screens.subscriptions.UaPreset
import io.github.anatolijlaptev1991_ctrl.lavray.ui.screens.subscriptions.UpdatePhase
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNotNull
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test

class SubscriptionsStateTest {
    private fun row(id: Long, name: String = "s$id") = SubRow.from(Group(id = id, name = name, subscriptionUrl = "https://h$id.example/x"), count = 3)
    private val notFound = SubFailure.fromCode("404")

    private fun loaded(vararg ids: Long, base: SubscriptionsState = SubscriptionsState()): SubscriptionsState =
        SubscriptionsReducer.reduce(base, SubscriptionsAction.Loaded(ids.map { row(it) }))

    @Test
    fun loadingClearsTheSpinnerAndTheError() {
        val s = SubscriptionsReducer.reduce(SubscriptionsState(error = ErrorKind.CoreUnavailable), SubscriptionsAction.Loaded(listOf(row(1))))
        assertFalse(s.loading)
        assertNull(s.error)
        assertEquals(1, s.rows.size)
    }

    @Test
    fun aFailedLoadKeepsTheRowsAlreadyShown() {
        val s = SubscriptionsReducer.reduce(loaded(1, 2), SubscriptionsAction.Failed(ErrorKind.CoreUnavailable))
        assertEquals(ErrorKind.CoreUnavailable, s.error)
        assertEquals(2, s.rows.size)
    }

    @Test
    fun aFailureShowsOnItsRowAndIsForgottenWhenTheSubscriptionIsGone() {
        var s = loaded(1, 2)
        s = SubscriptionsReducer.reduce(s, SubscriptionsAction.NoteFailure(2, notFound))
        assertNull(s.rows.first { it.id == 1L }.failure)
        assertEquals(notFound, s.rows.first { it.id == 2L }.failure)
        s = loaded(1, base = s)
        assertTrue(s.failures.isEmpty())
    }

    @Test
    fun aRestoredFailureLandsOnRowsLoadedBeforeIt() {
        val s = SubscriptionsReducer.reduce(loaded(5), SubscriptionsAction.FailuresRestored(mapOf(5L to notFound)))
        assertEquals(notFound, s.rows.single().failure)
    }

    @Test
    fun droppingAFailureClearsTheBadge() {
        var s = SubscriptionsReducer.reduce(loaded(1), SubscriptionsAction.NoteFailure(1, notFound))
        s = SubscriptionsReducer.reduce(s, SubscriptionsAction.DropFailure(1))
        assertNull(s.rows.single().failure)
    }

    @Test
    fun oneUpdateAtATimeAndProgressIsRemembered() {
        var s = SubscriptionsReducer.reduce(loaded(1), SubscriptionsAction.UpdateStarted(1))
        assertEquals(Busy.One(1), s.busy)
        assertTrue(s.isBusy)
        s = SubscriptionsReducer.reduce(s, SubscriptionsAction.Progress(1, UpdatePhase.Parsing, null, null, null))
        s = SubscriptionsReducer.reduce(s, SubscriptionsAction.Progress(1, null, 4, 1, 20))
        assertEquals(UpdatePhase.Parsing, s.progress.phase)
        assertEquals(4, s.progress.added)
        assertEquals(20, s.progress.total)
        s = SubscriptionsReducer.reduce(s, SubscriptionsAction.UpdateFinished)
        assertEquals(Busy.Idle, s.busy)
        assertNull(s.progress.phase)
    }

    @Test
    fun updateAllMarksTheWholeScreenBusy() {
        val s = SubscriptionsReducer.reduce(loaded(1, 2), SubscriptionsAction.UpdateAllStarted)
        assertEquals(Busy.All, s.busy)
    }

    @Test
    fun theConfirmQuestionIsAskedAndClosed() {
        val ask = SubscriptionsReducer.confirmOf(3, "P", remove = 8, knownCount = 5)
        assertEquals(PendingConfirm(3, "P", 8, 8), ask)
        assertEquals(10, SubscriptionsReducer.confirmOf(3, "P", remove = 2, knownCount = 10).total)
        var s = SubscriptionsReducer.reduce(loaded(3), SubscriptionsAction.Confirm(ask))
        assertEquals(ask, s.confirm)
        s = SubscriptionsReducer.reduce(s, SubscriptionsAction.Confirm(null))
        assertNull(s.confirm)
    }

    @Test
    fun aDeletedSubscriptionLeavesTheListAtOnce() {
        val s = SubscriptionsReducer.reduce(loaded(1, 2), SubscriptionsAction.Removed(1))
        assertEquals(listOf(2L), s.rows.map { it.id })
    }

    // ---- stored failures

    @Test
    fun storedFailuresRoundTripAsCodesOnly() {
        val text = SubFailureStore.encode(mapOf(1L to notFound, 2L to SubFailure.fromCode("timeout")))
        assertFalse(text.contains("http"))
        val back = SubFailureStore.decode(text)
        assertEquals(setOf(1L, 2L), back.keys)
        assertEquals(notFound, back[1L])
    }

    @Test
    fun damagedStoredDataIsIgnored() {
        assertTrue(SubFailureStore.decode(null).isEmpty())
        assertTrue(SubFailureStore.decode("").isEmpty())
        assertTrue(SubFailureStore.decode("not json").isEmpty())
        assertEquals(mapOf(2L to SubFailure.fromCode("500")), SubFailureStore.decode("""{"x":"404","2":"500","3":""}"""))
    }

    // ---- settings form

    private val base = SubRow.from(Group(id = 1, name = "Prov", subscriptionUrl = "https://p.example/s", updateIntervalMin = 0, userAgent = ""), count = 1)

    @Test
    fun anUntouchedFormSendsNothing() {
        assertTrue(SubscriptionEdit.plan(base, EditForm.from(base)).isEmpty)
    }

    @Test
    fun onlyTouchedFieldsGoIntoThePlan() {
        val form = EditForm.from(base).copy(hours = 12, intervalTouched = true, hwid = true, hwidTouched = true)
        val plan = SubscriptionEdit.plan(base, form)
        assertEquals(720, plan.intervalMin)
        assertNull(plan.userAgent)
        assertNull(plan.newName)
        assertNull(plan.newUrl)
        assertEquals(mapOf(SubSettingKeys.HWID to true), plan.settings)
        assertTrue(plan.touchesGroup)
    }

    @Test
    fun renameAndNewLinkAreDetectedAgainstTheStoredValues() {
        val form = EditForm.from(base).copy(name = " New ", url = "https://q.example/s")
        val plan = SubscriptionEdit.plan(base, form)
        assertEquals("New", plan.newName)
        assertEquals("https://q.example/s", plan.newUrl)
    }

    @Test
    fun userAgentPresetsMapToHeadersAndBack() {
        assertEquals(UaPreset.Default, UaPreset.of(""))
        assertEquals(UaPreset.Clash, UaPreset.of("clash.meta"))
        assertEquals(UaPreset.Custom, UaPreset.of("my-agent/1"))
        val form = EditForm.from(base).copy(ua = UaPreset.V2rayN, uaTouched = true)
        assertEquals("v2rayN/7.0", SubscriptionEdit.plan(base, form).userAgent)
        val custom = form.copy(ua = UaPreset.Custom, uaCustom = " agent ")
        assertEquals("agent", SubscriptionEdit.plan(base, custom).userAgent)
        assertFalse(custom.copy(uaCustom = " ").valid)
        assertTrue(custom.valid)
    }

    @Test
    fun applyingAPlanKeepsEveryOtherFieldOfTheGroup() {
        val g = Group(
            id = 9, name = "Old", subscriptionUrl = "u", httpHeaders = mapOf("X-A" to "1"), httpBasicUser = "bob",
            profileOrder = listOf(1, 2), skipAutoUpdate = true,
        )
        val plan = SubscriptionEdit.plan(base, EditForm.from(base).copy(name = "New"))
        val next = SubscriptionEdit.apply(g, plan)
        assertEquals("New", next.name)
        assertEquals(g.httpHeaders, next.httpHeaders)
        assertEquals("bob", next.httpBasicUser)
        assertEquals(listOf(1L, 2L), next.profileOrder)
        assertTrue(next.skipAutoUpdate)
        assertNotNull(next.subscriptionUrl)
    }

    @Test
    fun autoUpdateFlipsOnlyTheFlag() {
        val g = Group(id = 1, name = "n", httpBasicUser = "bob")
        val off = SubscriptionEdit.withAutoUpdate(g, on = false)
        assertTrue(off.skipAutoUpdate)
        assertEquals("bob", off.httpBasicUser)
        assertFalse(SubscriptionEdit.withAutoUpdate(off, on = true).skipAutoUpdate)
    }
}

/** The failure store keeps no clock of the core, so a later successful update must retire the failure by its time. */
class SubscriptionFailureClockTest {
    private fun group(id: Long, lastUpdate: Long?) =
        SubRow.from(Group(id = id, name = "s$id", subscriptionUrl = "https://h$id.example/x", lastUpdateMs = lastUpdate), count = 2)

    private val failedAt = SubFailure.fromCode("404", atMs = 1_000L)

    @Test fun anUpdateAfterTheFailureRetiresIt() {
        val s = SubscriptionsState(failures = mapOf(1L to failedAt))
        val after = SubscriptionsReducer.reduce(s, SubscriptionsAction.Loaded(listOf(group(1, lastUpdate = 2_000L))))
        assertTrue(after.failures.isEmpty())
        assertNull(after.rows.single().failure)
    }

    @Test fun aFailureAfterTheLastUpdateStays() {
        val s = SubscriptionsState(failures = mapOf(1L to failedAt))
        val after = SubscriptionsReducer.reduce(s, SubscriptionsAction.Loaded(listOf(group(1, lastUpdate = 500L))))
        assertEquals(failedAt, after.rows.single().failure)
    }

    @Test fun aSubscriptionThatNeverUpdatedKeepsItsFailure() {
        val s = SubscriptionsState(failures = mapOf(1L to failedAt))
        val after = SubscriptionsReducer.reduce(s, SubscriptionsAction.Loaded(listOf(group(1, lastUpdate = null))))
        assertEquals(failedAt, after.rows.single().failure)
    }

    @Test fun aFailureWithoutATimeFromAnOlderBuildIsRetiredByAnyRecordedUpdate() {
        val old = SubFailure.fromCode("timeout")
        val s = SubscriptionsState(failures = mapOf(1L to old))
        assertTrue(SubscriptionsReducer.reduce(s, SubscriptionsAction.Loaded(listOf(group(1, lastUpdate = 10L)))).failures.isEmpty())
    }

    @Test fun restoredFailuresAreCheckedAgainstTheRowsAlreadyLoaded() {
        val loaded = SubscriptionsReducer.reduce(SubscriptionsState(), SubscriptionsAction.Loaded(listOf(group(1, lastUpdate = 2_000L))))
        val after = SubscriptionsReducer.reduce(loaded, SubscriptionsAction.FailuresRestored(mapOf(1L to failedAt)))
        assertTrue(after.failures.isEmpty())
        assertNull(after.rows.single().failure)
    }

    @Test fun theTimeSurvivesTheStore() {
        val text = SubFailureStore.encode(mapOf(1L to failedAt, 2L to SubFailure.fromCode("timeout")))
        val back = SubFailureStore.decode(text)
        assertEquals(failedAt, back[1L])
        assertEquals(SubFailure.fromCode("timeout"), back[2L])
        // The older format had the bare code.
        assertEquals(SubFailure.fromCode("404"), SubFailureStore.decode("""{"5":"404"}""")[5L])
        assertTrue(SubFailureStore.decode("""{"5":"404@-3"}""").isEmpty())
    }

    @Test fun supersededIsAboutTheGroupsOwnTime() {
        assertTrue(SubscriptionLogic.superseded(failedAt, 1_000L))
        assertFalse(SubscriptionLogic.superseded(failedAt, 999L))
        assertFalse(SubscriptionLogic.superseded(failedAt, null))
    }
}

class SubscriptionsEmptyStateTest {
    @Test fun theEmptyStateIsOnlyForALoadedEmptyListWithoutAnError() {
        val loaded = SubscriptionsState(loading = false)
        assertTrue(io.github.anatolijlaptev1991_ctrl.lavray.ui.screens.subscriptions.isEmptyList(loaded))
        assertFalse(io.github.anatolijlaptev1991_ctrl.lavray.ui.screens.subscriptions.isEmptyList(loaded.copy(loading = true)))
        assertFalse(io.github.anatolijlaptev1991_ctrl.lavray.ui.screens.subscriptions.isEmptyList(loaded.copy(error = ErrorKind.CoreUnavailable)))
    }
}
