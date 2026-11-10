// SPDX-License-Identifier: GPL-3.0-only
package io.github.anatolijlaptev1991_ctrl.lavray.ui.importer

import io.github.anatolijlaptev1991_ctrl.lavray.api.ProtoJson
import io.github.anatolijlaptev1991_ctrl.lavray.api.gen.CreateSubscriptionRequest
import io.github.anatolijlaptev1991_ctrl.lavray.api.gen.DeleteSubscriptionRequest
import io.github.anatolijlaptev1991_ctrl.lavray.api.gen.Empty
import io.github.anatolijlaptev1991_ctrl.lavray.api.gen.GetSettingsResponse
import io.github.anatolijlaptev1991_ctrl.lavray.api.gen.Group
import io.github.anatolijlaptev1991_ctrl.lavray.api.gen.ImportContentRequest
import io.github.anatolijlaptev1991_ctrl.lavray.api.gen.ImportContentResponse
import io.github.anatolijlaptev1991_ctrl.lavray.api.gen.ImportContentResponse.ContentKind
import io.github.anatolijlaptev1991_ctrl.lavray.api.gen.ListProfilesResponse
import io.github.anatolijlaptev1991_ctrl.lavray.api.gen.ListSubscriptionsResponse
import io.github.anatolijlaptev1991_ctrl.lavray.api.gen.Profile
import io.github.anatolijlaptev1991_ctrl.lavray.api.gen.ProfileService
import io.github.anatolijlaptev1991_ctrl.lavray.api.gen.SettingValue
import io.github.anatolijlaptev1991_ctrl.lavray.api.gen.SettingsService
import io.github.anatolijlaptev1991_ctrl.lavray.api.gen.SubscriptionProgress
import io.github.anatolijlaptev1991_ctrl.lavray.api.gen.SubscriptionProgress.Phase
import io.github.anatolijlaptev1991_ctrl.lavray.api.gen.SubscriptionService
import io.github.anatolijlaptev1991_ctrl.lavray.ui.core.ErrorKind
import io.github.anatolijlaptev1991_ctrl.lavray.ui.screens.subscriptions.SubErrorKind
import io.github.anatolijlaptev1991_ctrl.lavray.ui.screens.subscriptions.SubFailure
import io.github.anatolijlaptev1991_ctrl.lavray.ui.screens.subscriptions.SubFailureStorage
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
import java.util.concurrent.atomic.AtomicInteger

private class MemoryFailures : SubFailureStorage {
    override fun load(): Map<Long, SubFailure> = emptyMap()
    override fun save(failures: Map<Long, SubFailure>) = Unit
}

private const val PROVIDER = "https://provider.example/sub/TOKEN"
private const val HAPP = "happ://crypt3/AAAA"

/**
 * The import flow against a core that lives in memory. The owner's bug: a subscription link (or a happ link
 * that opens to one) came back from the core as kind SUBSCRIPTION_URL with nothing saved, and the sheet
 * reported "Saved". Each test here names a branch of what the sheet must do instead.
 */
class ImportFlowTests {
    private val core = FakeCore()
    private val scope = CoroutineScope(SupervisorJob() + Dispatchers.Default)
    private val serversChanged = AtomicInteger()
    private val subscriptions = SubscriptionsModel(core.session, scope, MemoryFailures())
    private val model = ImportModel(core.session, scope, subscriptions, { serversChanged.incrementAndGet() }, previewDelayMs = 20)

    @After
    fun stop() = scope.cancel()

    // ---- helpers

    private fun group(id: Long, url: String, name: String = "provider.example") =
        Group(id = id, name = name, kind = Group.Kind.KIND_SUBSCRIPTION, subscriptionUrl = url)

    private fun importReply(r: ImportContentResponse) = reply(ImportContentResponse.serializer(), r)

    private fun subscriptionKind() = importReply(ImportContentResponse(kind = ContentKind.CONTENT_KIND_SUBSCRIPTION_URL))

    private fun existing(vararg groups: Group) =
        core.on(SubscriptionService.LIST_SUBSCRIPTIONS, reply(ListSubscriptionsResponse.serializer(), ListSubscriptionsResponse(groups.toList())))

    private fun created(g: Group) = core.on(SubscriptionService.CREATE_SUBSCRIPTION, reply(Group.serializer(), g))

    private fun fetchOk(id: Long, added: Int, removed: Int = 0, total: Int = added, detail: String? = null) = Reply.Frames(
        listOf(
            SubscriptionProgress(groupId = id, phase = Phase.PHASE_REQUESTING),
            SubscriptionProgress(groupId = id, phase = Phase.PHASE_DIFFING, addedCount = added, removedCount = removed, totalCount = total),
            SubscriptionProgress(groupId = id, phase = Phase.PHASE_DONE, detail = detail),
        ),
    )

    private fun fetchFailed(id: Long, detail: String) = Reply.Frames(
        listOf(SubscriptionProgress(groupId = id, phase = Phase.PHASE_FAILED, detail = detail)),
        endError = "internal" to detail,
    )

    private fun hwid(enabled: Boolean) = core.on(
        SettingsService.GET_SETTINGS,
        reply(GetSettingsResponse.serializer(), GetSettingsResponse(mapOf("sub.hwid_enabled" to SettingValue(boolValue = enabled)))),
    )

    private fun listProfiles(total: Long = 0) = core.on(ProfileService.LIST_PROFILES, reply(ListProfilesResponse.serializer(), ListProfilesResponse(total = total)))

    private fun open(text: String) {
        model.open(text)
        runBlocking { withTimeout(5_000) { model.ui.first { it.input != ImportInput.Empty } } }
    }

    /** Taps Import and waits for the outcome: the report the sheet closes with, or the sheet's final state. */
    private fun submit(): Pair<ImportEvent?, ImportUi> = runBlocking {
        val event = async(Dispatchers.Default) { withTimeout(5_000) { model.events.first() } }
        // The collector must be subscribed before the model can emit; a short pause is enough on the JVM.
        kotlinx.coroutines.delay(50)
        model.submit(null)
        val settled = withTimeout(5_000) { model.ui.first { !it.busy && (it.problem != null || it.error != null || !it.open) } }
        val e = if (settled.open) { event.cancel(); null } else event.await()
        e to settled
    }

    private fun report(outcome: Pair<ImportEvent?, ImportUi>): ImportReport = (outcome.first as ImportEvent.Done).report

    private fun importRequests() = core.callsTo(ProfileService.IMPORT_CONTENT).map { ProtoJson.decodeFromString(ImportContentRequest.serializer(), it.body) }

    // ---- the bug: a subscription link

    @Test
    fun aSubscriptionLinkCreatesTheSubscriptionAndFetchesItInOneAction() {
        core.on(ProfileService.IMPORT_CONTENT, subscriptionKind())
        existing()
        created(group(7, PROVIDER))
        core.on(SubscriptionService.UPDATE_SUBSCRIPTION, fetchOk(7, added = 12))
        listProfiles()
        open(PROVIDER)

        val outcome = submit()

        val r = report(outcome) as ImportReport.Subscribed
        assertEquals("provider.example", r.name)
        assertEquals(12, r.added)
        assertEquals(12, r.total)
        assertFalse(r.existed)
        assertFalse("the sheet closes on success", outcome.second.open)
        assertEquals(1, core.callsTo(SubscriptionService.CREATE_SUBSCRIPTION).size)
        assertEquals(1, core.callsTo(SubscriptionService.UPDATE_SUBSCRIPTION).size)
        assertTrue("the servers list is refreshed", serversChanged.get() >= 1)
        // The link goes to CreateSubscription as typed: the core decrypts happ links there.
        assertEquals(PROVIDER, ProtoJson.decodeFromString(CreateSubscriptionRequest.serializer(), core.callsTo(SubscriptionService.CREATE_SUBSCRIPTION).single().body).url)
        // The core is asked what it will do with the text, not only the local classifier.
        assertEquals(PROVIDER, importRequests().last().content)
    }

    @Test
    fun aHappLinkThatOpensToASubscriptionIsHandledTheSameWay() {
        core.on(ProfileService.IMPORT_CONTENT, subscriptionKind())
        existing()
        created(group(9, "https://panel.example/api/sub/ABC", name = "panel.example"))
        core.on(SubscriptionService.UPDATE_SUBSCRIPTION, fetchOk(9, added = 3))
        listProfiles()
        open(HAPP)

        val r = report(submit()) as ImportReport.Subscribed

        assertEquals("panel.example", r.name)
        assertEquals(3, r.added)
        assertEquals(HAPP, ProtoJson.decodeFromString(CreateSubscriptionRequest.serializer(), core.callsTo(SubscriptionService.CREATE_SUBSCRIPTION).single().body).url)
    }

    @Test
    fun aHappLinkThatOpensToProfilesSavesThemAndCreatesNoSubscription() {
        core.on(
            ProfileService.IMPORT_CONTENT,
            importReply(ImportContentResponse(kind = ContentKind.CONTENT_KIND_HAPP, parsedProfiles = listOf(Profile(), Profile()), addedProfileIds = listOf(1, 2))),
        )
        open(HAPP)

        val r = report(submit()) as ImportReport.Servers

        assertEquals(2, r.added)
        assertEquals(0, r.alreadyHad)
        assertTrue(core.callsTo(SubscriptionService.CREATE_SUBSCRIPTION).isEmpty())
        assertTrue(serversChanged.get() >= 1)
    }

    // ---- never "saved" when nothing was

    @Test
    fun nothingAddedBecauseEverythingWasAlreadyThereIsSaidSo() {
        core.on(
            ProfileService.IMPORT_CONTENT,
            importReply(ImportContentResponse(kind = ContentKind.CONTENT_KIND_SHARE_LINKS, parsedProfiles = listOf(Profile(), Profile(), Profile()), log = listOf("пропущено уже имеющихся серверов: 3"))),
        )
        open("vless://a@h:1\nvless://b@h:2\nvless://c@h:3")

        val (event, ui) = submit()

        assertNull("no success message", event)
        assertTrue("the sheet stays open", ui.open)
        assertEquals(ImportProblem.AlreadyHave(3), ui.problem)
    }

    @Test
    fun nothingAddedAndNothingParsedIsNotASuccessEither() {
        core.on(ProfileService.IMPORT_CONTENT, importReply(ImportContentResponse(kind = ContentKind.CONTENT_KIND_BASE64, hint = "looks like HTML")))
        open("dmxlc3M6Ly9hQGg6MQp0cm9qYW46Ly9wQGg6Mg==")

        val (event, ui) = submit()

        assertNull(event)
        assertEquals(ImportProblem.NothingFound("looks like HTML"), ui.problem)
    }

    @Test
    fun someNewAndSomeKnownReportsBothNumbers() {
        core.on(
            ProfileService.IMPORT_CONTENT,
            importReply(ImportContentResponse(kind = ContentKind.CONTENT_KIND_SHARE_LINKS, parsedProfiles = listOf(Profile(), Profile(), Profile()), addedProfileIds = listOf(5))),
        )
        open("vless://a@h:1\nvless://b@h:2\nvless://c@h:3")

        val r = report(submit()) as ImportReport.Servers

        assertEquals(1, r.added)
        assertEquals(2, r.alreadyHad)
    }

    @Test
    fun warningsTheCoreRaisedTravelWithTheReport() {
        val dropped = io.github.anatolijlaptev1991_ctrl.lavray.api.gen.CustomFieldWarning(0, listOf("custom_config"))
        core.on(
            ProfileService.IMPORT_CONTENT,
            importReply(
                ImportContentResponse(
                    kind = ContentKind.CONTENT_KIND_SHARE_LINKS,
                    parsedProfiles = listOf(Profile(), Profile()), addedProfileIds = listOf(1, 2),
                    customFieldWarnings = listOf(dropped),
                    log = listOf("xhttp_unsupported: профиль «A» использует транспорт", "xhttp_unsupported: профиль «B» использует транспорт", "something else"),
                ),
            ),
        )
        open("vless://a@h:1\nvless://b@h:2")

        val r = report(submit()) as ImportReport.Servers

        assertEquals(2, r.xhttp)
        assertEquals(1, r.customDropped)
    }

    // ---- duplicates

    @Test
    fun aSubscriptionThatAlreadyExistsIsUpdatedNotDuplicated() {
        core.on(ProfileService.IMPORT_CONTENT, subscriptionKind())
        existing(group(3, PROVIDER, name = "Mine"))
        core.on(SubscriptionService.UPDATE_SUBSCRIPTION, fetchOk(3, added = 1, removed = 0, total = 10))
        listProfiles()
        open(PROVIDER)

        val r = report(submit()) as ImportReport.Subscribed

        assertTrue(r.existed)
        assertEquals("Mine", r.name)
        assertEquals(10, r.total)
        assertTrue("no second group", core.callsTo(SubscriptionService.CREATE_SUBSCRIPTION).isEmpty())
        assertEquals(3L, ProtoJson.decodeFromString(io.github.anatolijlaptev1991_ctrl.lavray.api.gen.UpdateSubscriptionRequest.serializer(), core.callsTo(SubscriptionService.UPDATE_SUBSCRIPTION).single().body).groupId)
    }

    @Test
    fun theSameAddressWrittenInAnotherCaseIsTheSameSubscription() {
        core.on(ProfileService.IMPORT_CONTENT, subscriptionKind())
        existing(group(3, "https://Provider.Example/sub/TOKEN"))
        core.on(SubscriptionService.UPDATE_SUBSCRIPTION, fetchOk(3, added = 0, total = 4))
        listProfiles()
        open(PROVIDER)

        assertTrue((report(submit()) as ImportReport.Subscribed).existed)
        assertTrue(core.callsTo(SubscriptionService.CREATE_SUBSCRIPTION).isEmpty())
    }

    @Test
    fun aHappLinkThatOpensToAnExistingSubscriptionDropsTheGroupItJustMade() {
        // The client cannot read a happ link, so it finds out from the group the core made: same address as another one.
        core.on(ProfileService.IMPORT_CONTENT, subscriptionKind())
        existing(group(3, "https://panel.example/api/sub/ABC", name = "Panel"))
        created(group(8, "https://panel.example/api/sub/ABC", name = "panel.example"))
        core.on(SubscriptionService.DELETE_SUBSCRIPTION, reply(Empty.serializer(), Empty))
        core.on(SubscriptionService.UPDATE_SUBSCRIPTION, fetchOk(3, added = 0, total = 5))
        listProfiles()
        open(HAPP)

        val r = report(submit()) as ImportReport.Subscribed

        assertTrue(r.existed)
        assertEquals("Panel", r.name)
        val deleted = ProtoJson.decodeFromString(DeleteSubscriptionRequest.serializer(), core.callsTo(SubscriptionService.DELETE_SUBSCRIPTION).single().body)
        assertEquals(8L, deleted.groupId)
        val updated = ProtoJson.decodeFromString(io.github.anatolijlaptev1991_ctrl.lavray.api.gen.UpdateSubscriptionRequest.serializer(), core.callsTo(SubscriptionService.UPDATE_SUBSCRIPTION).single().body)
        assertEquals(3L, updated.groupId)
    }

    @Test
    fun linesOfTheProvidersAnswerThatCouldNotBeReadAreCounted() {
        core.on(ProfileService.IMPORT_CONTENT, subscriptionKind())
        existing()
        created(group(7, PROVIDER))
        core.on(SubscriptionService.UPDATE_SUBSCRIPTION, fetchOk(7, added = 4, detail = "не разобрано строк ответа: 2 (причины — в журнале)"))
        listProfiles()
        open(PROVIDER)

        assertEquals(2, (report(submit()) as ImportReport.Subscribed).unparsedLines)
    }

    @Test
    fun theSubscriptionsScreenAddSheetDoesNotDuplicateEitherAndStillStartsTheUpdate() {
        existing(group(3, PROVIDER, name = "Mine"))
        core.on(SubscriptionService.UPDATE_SUBSCRIPTION, fetchOk(3, added = 0, total = 4))
        listProfiles()

        val r = runBlocking { subscriptions.create("", PROVIDER) }

        assertTrue(r is io.github.anatolijlaptev1991_ctrl.lavray.ui.core.CoreResult.Ok)
        assertTrue(core.callsTo(SubscriptionService.CREATE_SUBSCRIPTION).isEmpty())
        runBlocking { withTimeout(5_000) { while (core.callsTo(SubscriptionService.UPDATE_SUBSCRIPTION).isEmpty()) kotlinx.coroutines.delay(20) } }
    }

    // ---- fetch failures: the subscription is kept, and the text says so

    @Test
    fun aRefusedFetchKeepsTheSubscriptionAndSaysSoWithTheHwidHintWhenItIsOff() {
        core.on(ProfileService.IMPORT_CONTENT, subscriptionKind())
        existing()
        created(group(7, PROVIDER))
        core.on(SubscriptionService.UPDATE_SUBSCRIPTION, fetchFailed(7, "подписка: HTTP 403 Forbidden"))
        hwid(false)
        listProfiles()
        open(PROVIDER)

        val (event, ui) = submit()

        assertNull(event)
        assertTrue("the sheet stays open for a retry", ui.open)
        val p = ui.problem as ImportProblem.NotFetched
        assertEquals("provider.example", p.name)
        assertEquals(SubErrorKind.Auth, p.kind)
        assertEquals("403", p.code)
        assertTrue(p.hwidHint)
        assertTrue("not rolled back", core.callsTo(SubscriptionService.DELETE_SUBSCRIPTION).isEmpty())
        assertTrue("the list shows the saved, empty subscription", serversChanged.get() >= 1)
    }

    @Test
    fun noHwidHintWhenTheSwitchIsAlreadyOn() {
        core.on(ProfileService.IMPORT_CONTENT, subscriptionKind())
        existing()
        created(group(7, PROVIDER))
        core.on(SubscriptionService.UPDATE_SUBSCRIPTION, fetchFailed(7, "подписка: HTTP 403 Forbidden"))
        hwid(true)
        listProfiles()
        open(PROVIDER)

        assertFalse((submit().second.problem as ImportProblem.NotFetched).hwidHint)
    }

    @Test
    fun anEmptyOrStubAnswerSuggestsTheHwidSwitchAnATimeoutDoesNot() {
        for ((detail, kind, hint) in listOf(
            Triple("пустой ответ подписки", SubErrorKind.Zero, true),
            Triple("подписка 7: разбор: ответ похож на HTML-заглушку (captive portal / DPI)", SubErrorKind.Format, true),
            Triple("context deadline exceeded (timeout)", SubErrorKind.Timeout, false),
        )) {
            val fresh = FakeCore()
            val scope = CoroutineScope(SupervisorJob() + Dispatchers.Default)
            val subs = SubscriptionsModel(fresh.session, scope, MemoryFailures())
            val m = ImportModel(fresh.session, scope, subs, {}, previewDelayMs = 20)
            fresh.on(ProfileService.IMPORT_CONTENT, subscriptionKind())
            fresh.on(SubscriptionService.LIST_SUBSCRIPTIONS, reply(ListSubscriptionsResponse.serializer(), ListSubscriptionsResponse()))
            fresh.on(SubscriptionService.CREATE_SUBSCRIPTION, reply(Group.serializer(), group(7, PROVIDER)))
            fresh.on(SubscriptionService.UPDATE_SUBSCRIPTION, fetchFailed(7, detail))
            fresh.on(SettingsService.GET_SETTINGS, reply(GetSettingsResponse.serializer(), GetSettingsResponse()))
            fresh.on(ProfileService.LIST_PROFILES, reply(ListProfilesResponse.serializer(), ListProfilesResponse()))
            val ui = runBlocking {
                m.open(PROVIDER)
                withTimeout(5_000) { m.ui.first { it.input != ImportInput.Empty } }
                m.submit(null)
                withTimeout(5_000) { m.ui.first { !it.busy && it.problem != null } }
            }
            val p = ui.problem as ImportProblem.NotFetched
            assertEquals(detail, kind, p.kind)
            assertEquals(detail, hint, p.hwidHint)
            scope.cancel()
        }
    }

    @Test
    fun aFetchThatWouldRemoveServersAsksOnTheSubscriptionsScreen() {
        core.on(ProfileService.IMPORT_CONTENT, subscriptionKind())
        existing(group(3, PROVIDER, name = "Mine"))
        core.on(
            SubscriptionService.UPDATE_SUBSCRIPTION,
            Reply.Frames(listOf(SubscriptionProgress(groupId = 3, phase = Phase.PHASE_CONFIRM_REQUIRED, wouldRemoveCount = 8, totalCount = 10))),
        )
        listProfiles(total = 10)
        open(PROVIDER)

        val r = report(submit()) as ImportReport.SubscriptionConfirm

        assertEquals("Mine", r.name)
        assertEquals(8, r.remove)
        val ask = runBlocking { withTimeout(5_000) { subscriptions.state.first { it.confirm != null } } }.confirm!!
        assertEquals(3L, ask.id)
        assertEquals(8, ask.remove)
    }

    @Test
    fun anUpdateAlreadyRunningIsSaidBeforeAnythingIsCreated() {
        core.on(ProfileService.IMPORT_CONTENT, subscriptionKind())
        existing()
        // A real update of all subscriptions, held open by the core while the link is imported.
        val gate = java.util.concurrent.CountDownLatch(1)
        core.on(SubscriptionService.UPDATE_ALL_SUBSCRIPTIONS) { gate.await(5, java.util.concurrent.TimeUnit.SECONDS); Reply.Frames(emptyList()) }
        subscriptions.updateAll()
        runBlocking { withTimeout(5_000) { subscriptions.state.first { it.isBusy } } }
        open(PROVIDER)

        val (event, ui) = submit()
        gate.countDown()

        assertNull(event)
        assertEquals(ImportProblem.Busy, ui.problem)
        assertTrue(core.callsTo(SubscriptionService.CREATE_SUBSCRIPTION).isEmpty())
    }

    @Test
    fun aLinkTheCoreRefusesToCreateIsAnErrorNotASavedSubscription() {
        core.on(ProfileService.IMPORT_CONTENT, subscriptionKind())
        existing()
        core.on(SubscriptionService.CREATE_SUBSCRIPTION, Reply.Fail("invalid_argument", "URL подписки указывает на локальный адрес"))
        open("http://127.0.0.1/sub/x")

        val (event, ui) = submit()

        assertNull(event)
        // The core had said "subscription link": a refusal of the address is about the address (M3), not "input could not be read".
        assertEquals(ImportProblem.BadAddress, ui.problem)
        assertNull(ui.error)
        assertTrue(core.callsTo(SubscriptionService.UPDATE_SUBSCRIPTION).isEmpty())
    }

    @Test
    fun aRefusalOfTheImportItselfKeepsTheLineTheCoreNamed() {
        // The preview passes (it cannot see everything), the import itself is refused.
        core.on(ProfileService.IMPORT_CONTENT) { body ->
            if ("previewOnly" in body) importReply(ImportContentResponse(kind = ContentKind.CONTENT_KIND_SHARE_LINKS, parsedProfiles = listOf(Profile())))
            else Reply.Fail("invalid_argument", "не распознано ни одного профиля (первая ошибка: строка 2: bad)")
        }
        open("vless://a@h:1\nvless://bad")

        val ui = submit().second

        assertEquals(ErrorKind.InvalidInput, ui.error)
        assertEquals(2, ui.errorLine)
        assertNull(ui.problem)
    }

    // ---- the core's own preview

    @Test
    fun theCorePreviewIsAskedOnceForTheSettledTextAndAnswersWithWhatItWillDo() {
        core.on(
            ProfileService.IMPORT_CONTENT,
            importReply(
                ImportContentResponse(
                    kind = ContentKind.CONTENT_KIND_SHARE_LINKS, parsedProfiles = listOf(Profile(), Profile()),
                    log = listOf("xhttp_unsupported: профиль «A»"), hint = "careful",
                ),
            ),
        )
        model.open()
        model.setPayload("vless://a@h:1")
        model.setPayload("vless://a@h:1\nvless://b@h:2")

        val ui = runBlocking { withTimeout(5_000) { model.ui.first { it.preview != null } } }

        assertEquals(2, ui.preview!!.count)
        assertEquals(1, ui.preview.xhttp)
        assertEquals("careful", ui.preview.hint)
        val asked = importRequests()
        assertEquals("one call, for the text that settled", 1, asked.size)
        assertEquals(true, asked.single().previewOnly)
        assertEquals("vless://a@h:1\nvless://b@h:2", asked.single().content)
    }

    @Test
    fun thePreviewOfASubscriptionLinkSaysSoAndSavesNothing() {
        core.on(ProfileService.IMPORT_CONTENT, subscriptionKind())
        model.open(HAPP)

        val ui = runBlocking { withTimeout(5_000) { model.ui.first { it.preview != null } } }

        assertEquals(ContentKind.CONTENT_KIND_SUBSCRIPTION_URL, ui.preview!!.kind)
        assertTrue(core.callsTo(SubscriptionService.CREATE_SUBSCRIPTION).isEmpty())
    }

    @Test
    fun aPreviewTheCoreRefusesIsShownAndBlocksTheButtonOnlyForBadInput() {
        core.on(ProfileService.IMPORT_CONTENT, Reply.Fail("invalid_argument", "не удалось расшифровать ссылку"))
        model.open(HAPP)

        val ui = runBlocking { withTimeout(5_000) { model.ui.first { it.previewError != null } } }

        assertEquals(ErrorKind.InvalidInput, ui.previewError!!.kind)
        assertTrue("a happ link that cannot be opened gets its own wording", ui.previewError.happ)
        assertFalse(ui.canSubmit)
    }

    @Test
    fun aPreviewThatCouldNotReachTheCoreDoesNotBlockTheButton() {
        core.on(ProfileService.IMPORT_CONTENT, Reply.Fail("unavailable", "down"))
        model.open("vless://a@h:1")

        val ui = runBlocking { withTimeout(5_000) { model.ui.first { it.previewError != null } } }

        assertEquals(ErrorKind.CoreUnavailable, ui.previewError!!.kind)
        assertTrue(ui.canSubmit)
    }

    @Test
    fun nothingIsAskedOfTheCoreWhileTheSheetIsClosedOrTheTextIsNotImportable() {
        core.on(ProfileService.IMPORT_CONTENT, importReply(ImportContentResponse()))
        model.open()
        model.setPayload("just some words")
        model.close()
        model.setPayload("vless://a@h:1")
        Thread.sleep(300)
        assertTrue(importRequests().isEmpty())
    }

    // ---- a sheet that was closed while the work went on

    @Test
    fun closingTheSheetDuringTheFetchStillReportsAndDoesNotTouchANewSheet() {
        core.on(ProfileService.IMPORT_CONTENT, subscriptionKind())
        existing()
        created(group(7, PROVIDER))
        core.on(SubscriptionService.UPDATE_SUBSCRIPTION) { Thread.sleep(300); fetchOk(7, added = 2) }
        listProfiles()
        open(PROVIDER)
        val outcome = runBlocking {
            val event = async(Dispatchers.Default) { withTimeout(5_000) { model.events.first() } }
            kotlinx.coroutines.delay(50)
            model.submit(null)
            kotlinx.coroutines.delay(100)
            model.close()
            model.open("vless://other@h:1") // the person starts something else
            event.await()
        }
        assertTrue((outcome as ImportEvent.Done).report is ImportReport.Subscribed)
        val ui = model.ui.value
        assertTrue("the new sheet is still open with its own text", ui.open && ui.payload == "vless://other@h:1")
    }
}
