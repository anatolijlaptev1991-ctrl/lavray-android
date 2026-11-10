// SPDX-License-Identifier: GPL-3.0-only
package io.github.anatolijlaptev1991_ctrl.lavray.ui.importer

import io.github.anatolijlaptev1991_ctrl.lavray.api.ProtoJson
import io.github.anatolijlaptev1991_ctrl.lavray.api.gen.DeleteSubscriptionRequest
import io.github.anatolijlaptev1991_ctrl.lavray.api.gen.Empty
import io.github.anatolijlaptev1991_ctrl.lavray.api.gen.Group
import io.github.anatolijlaptev1991_ctrl.lavray.api.gen.ListProfilesResponse
import io.github.anatolijlaptev1991_ctrl.lavray.api.gen.ListSubscriptionsResponse
import io.github.anatolijlaptev1991_ctrl.lavray.api.gen.ProfileService
import io.github.anatolijlaptev1991_ctrl.lavray.api.gen.RollbackSubscriptionResponse
import io.github.anatolijlaptev1991_ctrl.lavray.api.gen.SubscriptionProgress
import io.github.anatolijlaptev1991_ctrl.lavray.api.gen.SubscriptionProgress.Phase
import io.github.anatolijlaptev1991_ctrl.lavray.api.gen.SubscriptionService
import io.github.anatolijlaptev1991_ctrl.lavray.api.gen.UpdateGroupRequest
import io.github.anatolijlaptev1991_ctrl.lavray.ui.core.ErrorKind
import io.github.anatolijlaptev1991_ctrl.lavray.ui.screens.subscriptions.EditForm
import io.github.anatolijlaptev1991_ctrl.lavray.ui.screens.subscriptions.SubFailure
import io.github.anatolijlaptev1991_ctrl.lavray.ui.screens.subscriptions.SubFailureStorage
import io.github.anatolijlaptev1991_ctrl.lavray.ui.screens.subscriptions.SubNotes
import io.github.anatolijlaptev1991_ctrl.lavray.ui.screens.subscriptions.SubRow
import io.github.anatolijlaptev1991_ctrl.lavray.ui.screens.subscriptions.SubscribeResult
import io.github.anatolijlaptev1991_ctrl.lavray.ui.screens.subscriptions.SubscriptionsEvent
import io.github.anatolijlaptev1991_ctrl.lavray.ui.screens.subscriptions.SubscriptionsModel
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.SupervisorJob
import kotlinx.coroutines.async
import kotlinx.coroutines.cancel
import kotlinx.coroutines.flow.first
import kotlinx.coroutines.runBlocking
import kotlinx.coroutines.withTimeout
import org.junit.After
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test
import java.util.concurrent.CountDownLatch
import java.util.concurrent.TimeUnit
import java.util.concurrent.atomic.AtomicInteger

private class NoStore : SubFailureStorage {
    override fun load(): Map<Long, SubFailure> = emptyMap()
    override fun save(failures: Map<Long, SubFailure>) = Unit
}

private const val LINK = "https://provider.example/sub/TOKEN"

/** The findings M1, M3, M4, M5 and L1-L10 of the review of the import commits, each pinned by what the core answers. */
class ImportReviewTests {
    private val core = FakeCore()
    private val scope = CoroutineScope(SupervisorJob() + Dispatchers.Default)
    private val subscriptions = SubscriptionsModel(core.session, scope, NoStore())

    @After
    fun stop() = scope.cancel()

    private fun group(id: Long, url: String, name: String = "provider.example", archive: Boolean = false, skipAuto: Boolean = false) =
        Group(id = id, name = name, kind = Group.Kind.KIND_SUBSCRIPTION, subscriptionUrl = url, archive = archive, skipAutoUpdate = skipAuto)

    private fun existing(vararg groups: Group) =
        core.on(SubscriptionService.LIST_SUBSCRIPTIONS, reply(ListSubscriptionsResponse.serializer(), ListSubscriptionsResponse(groups.toList())))

    private fun created(g: Group) = core.on(SubscriptionService.CREATE_SUBSCRIPTION, reply(Group.serializer(), g))

    private fun fetchOk(id: Long) = Reply.Frames(
        listOf(
            SubscriptionProgress(groupId = id, phase = Phase.PHASE_DIFFING, addedCount = 2, removedCount = 0, totalCount = 2),
            SubscriptionProgress(groupId = id, phase = Phase.PHASE_DONE),
        ),
    )

    private fun listProfiles() = core.on(ProfileService.LIST_PROFILES, reply(ListProfilesResponse.serializer(), ListProfilesResponse(total = 2)))

    private fun updatedGroups() =
        core.callsTo(ProfileService.UPDATE_GROUP).map { ProtoJson.decodeFromString(UpdateGroupRequest.serializer(), it.body).group!! }

    private fun deleted() =
        core.callsTo(SubscriptionService.DELETE_SUBSCRIPTION).map { ProtoJson.decodeFromString(DeleteSubscriptionRequest.serializer(), it.body).groupId }

    // ---- M1: a twin that cannot be deleted must not be filled by the scheduler

    @Test fun aTwinThatCannotBeDeletedIsRetriedOnceThenParkedAndSaid() {
        existing(group(1, "https://panel.example/api/sub/ABC", name = "panel.example"))
        created(group(2, "https://panel.example/api/sub/ABC", name = "panel.example"))
        core.on(SubscriptionService.DELETE_SUBSCRIPTION, Reply.Fail("unavailable", "db locked"))
        core.on(ProfileService.UPDATE_GROUP, reply(Empty.serializer(), Empty))
        core.on(SubscriptionService.UPDATE_SUBSCRIPTION, fetchOk(1))
        listProfiles()

        val r = runBlocking { subscriptions.addAndFetch("happ://crypt3/AAAA") } as SubscribeResult.Fetched

        assertEquals("the existing group is the one updated", 1, core.callsTo(SubscriptionService.UPDATE_SUBSCRIPTION).size)
        assertTrue(r.existed)
        assertTrue("the person is told a duplicate is left", r.notes.duplicateLeft)
        assertEquals("the delete was tried twice", listOf(2L, 2L), deleted())
        val parked = updatedGroups().single()
        assertEquals(2L, parked.id)
        assertTrue("no automatic updates for the leftover", parked.skipAutoUpdate)
    }

    @Test fun aTwinThatIsDeletedOnTheSecondTryLeavesNothingToSay() {
        existing(group(1, "https://panel.example/api/sub/ABC", name = "panel.example"))
        created(group(2, "https://panel.example/api/sub/ABC", name = "panel.example"))
        val tries = AtomicInteger()
        core.on(SubscriptionService.DELETE_SUBSCRIPTION) { if (tries.incrementAndGet() == 1) Reply.Fail("unavailable", "x") else reply(Empty.serializer(), Empty) }
        core.on(SubscriptionService.UPDATE_SUBSCRIPTION, fetchOk(1))
        listProfiles()

        val r = runBlocking { subscriptions.addAndFetch("happ://crypt3/AAAA") } as SubscribeResult.Fetched

        assertFalse(r.notes.duplicateLeft)
        assertTrue(core.callsTo(ProfileService.UPDATE_GROUP).isEmpty())
    }

    // ---- L5: the name of a decrypted link is its host, not the whole address

    @Test fun theGroupOfADecryptedLinkIsNamedAfterItsHostAndRenamedWhenTheCoreChoseMore() {
        existing()
        created(group(5, "https://user:secret@panel.example:8443/api/sub/ABC?token=x", name = "user:secret@panel.example:8443"))
        core.on(ProfileService.UPDATE_GROUP, reply(Empty.serializer(), Empty))
        core.on(SubscriptionService.UPDATE_SUBSCRIPTION, fetchOk(5))
        listProfiles()

        val r = runBlocking { subscriptions.addAndFetch("happ://crypt3/AAAA") } as SubscribeResult.Fetched

        assertEquals("panel.example", r.name)
        assertEquals("panel.example", updatedGroups().single().name)
    }

    // ---- L6: an archived twin is not preferred to a live one

    @Test fun aLiveGroupWithTheSameAddressIsPreferredToAnArchivedOne() {
        existing(group(1, LINK, archive = true), group(2, LINK))
        core.on(SubscriptionService.UPDATE_SUBSCRIPTION, fetchOk(2))
        listProfiles()

        val r = runBlocking { subscriptions.addAndFetch(LINK) } as SubscribeResult.Fetched

        assertTrue(r.existed)
        assertTrue(core.callsTo(SubscriptionService.CREATE_SUBSCRIPTION).isEmpty())
        val updated = core.callsTo(SubscriptionService.UPDATE_SUBSCRIPTION).single().body
        assertTrue("the live group was updated: $updated", updated.contains("\"groupId\":\"2\"") || updated.contains("\"groupId\":2"))
    }

    @Test fun aCoreThatDidNotAnswerTheUpdateIsTheCoresTroubleNotTheProviders() {
        existing()
        created(group(3, LINK))
        core.on(SubscriptionService.UPDATE_SUBSCRIPTION, Reply.Frames(emptyList(), endError = "unavailable" to "core socket closed"))

        val r = runBlocking { subscriptions.addAndFetch(LINK) } as SubscribeResult.NotFetched

        assertEquals(ErrorKind.CoreUnavailable, r.coreProblem)
    }

    // ---- M4: a decrypted link that turns out to be plain http is said so

    @Test fun aDecryptedLinkThatIsPlainHttpCarriesTheCleartextNote() {
        existing()
        created(group(4, "http://panel.example/api/sub/ABC", name = "panel.example"))
        core.on(SubscriptionService.UPDATE_SUBSCRIPTION, fetchOk(4))
        listProfiles()

        val r = runBlocking { subscriptions.addAndFetch("happ://crypt3/AAAA") } as SubscribeResult.Fetched

        assertEquals(SubNotes(duplicateLeft = false, insecure = true), r.notes)
        // An https address has nothing to warn about.
        existing()
        created(group(6, LINK))
        core.on(SubscriptionService.UPDATE_SUBSCRIPTION, fetchOk(6))
        assertFalse((runBlocking { subscriptions.addAndFetch(LINK) } as SubscribeResult.Fetched).notes.insecure)
    }

    @Test fun aSingleHttpAddressWithoutAPathIsAnHttpProxyLinkNotASubscription() {
        // The core's rule (import_kind.go): a path, or a query with a key other than security and sni, makes it a subscription.
        assertTrue(ImportTestsAccess.isSub("http://p.example/sub"))
        assertTrue(ImportTestsAccess.isSub("https://p.example/?token=1"))
        assertTrue(ImportTestsAccess.isSub("https://p.example:8443/a/b?security=tls"))
        assertFalse(ImportTestsAccess.isSub("http://1.2.3.4:8080"))
        assertFalse(ImportTestsAccess.isSub("https://user:pass@host:443/"))
        assertFalse(ImportTestsAccess.isSub("http://host:80?security=tls&sni=a.example"))
        assertTrue(LinkClassifier.classify("http://1.2.3.4:8080") is ImportInput.Links)
        assertTrue(LinkClassifier.classify("https://p.example/sub/X") is ImportInput.SubscriptionUrl)
    }

    // ---- L2: two imports in the window before the state says "busy" make one group

    @Test fun twoImportsOfTheSameLinkAtOnceMakeOneGroupAndTheSecondIsToldItIsBusy() {
        val gate = CountDownLatch(1)
        existing()
        core.on(SubscriptionService.CREATE_SUBSCRIPTION) { gate.await(5, TimeUnit.SECONDS); reply(Group.serializer(), group(7, LINK)) }
        core.on(SubscriptionService.UPDATE_SUBSCRIPTION, fetchOk(7))
        listProfiles()

        val first = scope.async { subscriptions.addAndFetch(LINK) }
        runBlocking { withTimeout(5_000) { while (core.callsTo(SubscriptionService.LIST_SUBSCRIPTIONS).isEmpty()) kotlinx.coroutines.delay(5) } }
        val second = runBlocking { subscriptions.addAndFetch(LINK) }
        gate.countDown()

        assertEquals(SubscribeResult.Busy, second)
        assertTrue(runBlocking { first.await() } is SubscribeResult.Fetched)
        assertEquals(1, core.callsTo(SubscriptionService.CREATE_SUBSCRIPTION).size)
    }

    // ---- L4: the core's scheduler holds the group

    @Test fun aGroupTheCoreIsAlreadyUpdatingIsSavedAndSaidNotMarkedFailed() {
        existing(group(8, LINK))
        core.on(SubscriptionService.UPDATE_SUBSCRIPTION, Reply.Frames(emptyList(), endError = "already_exists" to "обновление уже идёт"))
        listProfiles()

        val r = runBlocking { subscriptions.addAndFetch(LINK) }

        assertTrue(r is SubscribeResult.Updating)
        assertEquals(ImportResult.Report(ImportReport.SubscriptionUpdating(group(8, LINK).name)), ImportOutcomes.subscribed(r))
        assertTrue("no failure badge on the card", subscriptions.state.value.failures.isEmpty())
    }

    @Test fun aScreenUpdateOfAGroupTheSchedulerHoldsSaysBusyAndLeavesNoFailure() {
        existing(group(8, LINK))
        core.on(SubscriptionService.UPDATE_SUBSCRIPTION, Reply.Frames(emptyList(), endError = "already_exists" to "обновление уже идёт"))
        listProfiles()
        val event = scope.async { withTimeout(5_000) { subscriptions.events.first() } }
        runBlocking { kotlinx.coroutines.delay(50) }

        subscriptions.refresh()
        runBlocking { withTimeout(5_000) { subscriptions.state.first { it.rows.isNotEmpty() } } }
        subscriptions.update(8)

        assertEquals(SubscriptionsEvent.Busy, runBlocking { event.await() })
        assertTrue(subscriptions.state.value.failures.isEmpty())
    }

    // ---- L7: a second update while one runs is refused and said

    @Test fun aSecondUpdateWhileOneRunsSaysItDidNotStart() {
        val gate = CountDownLatch(1)
        existing(group(8, LINK))
        core.on(SubscriptionService.UPDATE_SUBSCRIPTION) { gate.await(5, TimeUnit.SECONDS); fetchOk(8) }
        listProfiles()
        subscriptions.update(8)
        runBlocking { withTimeout(5_000) { subscriptions.state.first { it.isBusy } } }
        val event = scope.async { withTimeout(5_000) { subscriptions.events.first() } }
        runBlocking { kotlinx.coroutines.delay(50) }

        subscriptions.update(9)
        subscriptions.updateAll()
        val said = runBlocking { event.await() }
        gate.countDown()

        assertEquals(SubscriptionsEvent.Busy, said)
        assertEquals("no second request reached the core", 1, core.callsTo(SubscriptionService.UPDATE_SUBSCRIPTION).size)
        assertTrue(core.callsTo(SubscriptionService.UPDATE_ALL_SUBSCRIPTIONS).isEmpty())
    }

    // ---- L8 and the rollback after a core restart

    @Test fun aRollbackThatWorkedWithoutACountSaysSoInsteadOfRestoredZero() {
        existing(group(8, LINK))
        core.on(SubscriptionService.ROLLBACK_SUBSCRIPTION, reply(RollbackSubscriptionResponse.serializer(), RollbackSubscriptionResponse()))
        core.on(ProfileService.LIST_PROFILES, Reply.Fail("unavailable", "core socket closed"))
        val event = scope.async { withTimeout(5_000) { subscriptions.events.first() } }
        runBlocking { kotlinx.coroutines.delay(50) }

        subscriptions.rollback(row(8))

        assertEquals(SubscriptionsEvent.RolledBack("Mine", null), runBlocking { event.await() })
    }

    @Test fun aRollbackAfterTheCoreRestartedSaysThereIsNothingToGoBackTo() {
        core.on(SubscriptionService.ROLLBACK_SUBSCRIPTION, Reply.Fail("failed_precondition", "нет снапшота для отката этой подписки"))
        val event = scope.async { withTimeout(5_000) { subscriptions.events.first() } }
        runBlocking { kotlinx.coroutines.delay(50) }

        subscriptions.rollback(row(8))

        assertEquals(SubscriptionsEvent.RollbackUnavailable, runBlocking { event.await() })
    }

    private fun row(id: Long): SubRow = SubRow.from(group(id, LINK, name = "Mine"), 0)

    // ---- L10: renaming a subscription whose stored address is not http(s)

    @Test fun aStoredAddressThatIsNotHttpDoesNotBlockRenamingButAChangedOneMustBeAWebLink() {
        val old = SubRow.from(group(9, "happ://crypt3/AAAA", name = "Old"), 3)
        val form = EditForm.from(old)
        assertTrue("unchanged address, new name", form.copy(name = "New").valid)
        assertFalse("a changed address must be http(s)", form.copy(url = "happ://crypt3/BBBB").valid)
        assertTrue(form.copy(url = "https://p.example/sub").valid)
        assertFalse(form.copy(name = " ").valid)
    }

    // ---- L9 and L1

    @Test fun onlyASubscriptionImportIsCalledSubscriptionWorking() {
        assertTrue(ImportUi(input = ImportInput.SubscriptionUrl("https://p.example/s", false)).isSubscriptionImport)
        assertFalse(ImportUi(input = ImportInput.Links(listOf("vless://a@h:1"), listOf("vless"), 0)).isSubscriptionImport)
        // The core's answer wins over the local guess.
        val share = CorePreview(io.github.anatolijlaptev1991_ctrl.lavray.api.gen.ImportContentResponse.ContentKind.CONTENT_KIND_SHARE_LINKS, 1, emptyList(), 0, 0, null)
        assertFalse(ImportUi(input = ImportInput.SubscriptionUrl("https://p.example/s", false), preview = share).isSubscriptionImport)
    }

    @Test fun whileAnImportRunsTheTextCannotBeChangedAndAScanOpensANewSheet() {
        val model = ImportModel(core.session, scope, subscriptions, {}, previewDelayMs = 20)
        model.open("vless://a@h:1")
        runBlocking { withTimeout(5_000) { model.ui.first { it.input != ImportInput.Empty } } }
        val sheet = model.ui.value.sheet
        // Mark the import as running the way submit() does.
        val gate = CountDownLatch(1)
        core.on(ProfileService.IMPORT_CONTENT) { gate.await(5, TimeUnit.SECONDS); Reply.Fail("internal", "x") }
        model.submit(null)
        runBlocking { withTimeout(5_000) { model.ui.first { it.busy } } }

        model.setPayload("vless://other@h:2")
        model.selectTab(ImportTab.File)
        assertEquals("the text of the running import is kept", "vless://a@h:1", model.ui.value.payload)
        assertEquals(ImportTab.Link, model.ui.value.tab)

        // A scan that is not importable reopens the sheet: it is a new sheet, so the old import's result cannot touch it.
        model.offerScanned("not a link")
        assertTrue(model.ui.value.sheet > sheet)
        assertFalse(model.ui.value.busy)
        assertNull(model.ui.value.error)
        gate.countDown()
    }
}

/** Reaches the classifier's rule from the test without widening its visibility further than the module. */
private object ImportTestsAccess {
    fun isSub(url: String) = LinkClassifier.isSubscriptionPage(url)
}
