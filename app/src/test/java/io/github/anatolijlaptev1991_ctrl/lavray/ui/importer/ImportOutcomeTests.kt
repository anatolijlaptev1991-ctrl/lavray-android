// SPDX-License-Identifier: GPL-3.0-only
package io.github.anatolijlaptev1991_ctrl.lavray.ui.importer

import io.github.anatolijlaptev1991_ctrl.lavray.api.gen.CustomFieldWarning
import io.github.anatolijlaptev1991_ctrl.lavray.api.gen.ImportContentResponse
import io.github.anatolijlaptev1991_ctrl.lavray.api.gen.ImportContentResponse.ContentKind
import io.github.anatolijlaptev1991_ctrl.lavray.api.gen.Profile
import io.github.anatolijlaptev1991_ctrl.lavray.api.gen.ProfileType
import io.github.anatolijlaptev1991_ctrl.lavray.ui.core.ErrorKind
import io.github.anatolijlaptev1991_ctrl.lavray.ui.screens.subscriptions.SubErrorKind
import io.github.anatolijlaptev1991_ctrl.lavray.ui.screens.subscriptions.SubFailure
import io.github.anatolijlaptev1991_ctrl.lavray.ui.screens.subscriptions.SubscribeResult
import io.github.anatolijlaptev1991_ctrl.lavray.ui.screens.subscriptions.SubscriptionLogic
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test

/** The pure reading of the core's answers: every branch of what the person is told. */
class ImportOutcomeTests {
    private fun p(type: ProfileType = ProfileType.PROFILE_TYPE_VLESS) = Profile(type = type)

    @Test
    fun savedServersAreReportedWithWhatWasAlreadyThere() {
        val r = ImportOutcomes.saved(ImportContentResponse(parsedProfiles = listOf(p(), p(), p()), addedProfileIds = listOf(1, 2)))
        assertEquals(ImportResult.Report(ImportReport.Servers(added = 2, alreadyHad = 1, xhttp = 0, customDropped = 0)), r)
    }

    @Test
    fun aListWithNoAddedIdIsNeverASuccess() {
        assertEquals(ImportResult.Problem(ImportProblem.AlreadyHave(2)), ImportOutcomes.saved(ImportContentResponse(parsedProfiles = listOf(p(), p()))))
        assertEquals(ImportResult.Problem(ImportProblem.NothingFound(null)), ImportOutcomes.saved(ImportContentResponse()))
        assertEquals(ImportResult.Problem(ImportProblem.NothingFound("why")), ImportOutcomes.saved(ImportContentResponse(hint = "why")))
        assertEquals(ImportResult.Problem(ImportProblem.NothingFound(null)), ImportOutcomes.saved(ImportContentResponse(hint = "  ")))
    }

    @Test
    fun moreAddedThanParsedNeverGivesANegativeCount() {
        val r = ImportOutcomes.saved(ImportContentResponse(parsedProfiles = listOf(p()), addedProfileIds = listOf(1, 2))) as ImportResult.Report
        assertEquals(0, (r.report as ImportReport.Servers).alreadyHad)
    }

    @Test
    fun onlyTheCodedLogLinesCountAsXhttpAndOnlyRealDropsAsCustom() {
        val r = ImportContentResponse(
            parsedProfiles = listOf(p()), addedProfileIds = listOf(1),
            log = listOf("xhttp_unsupported: x", "not xhttp_unsupported: y", "пропущено уже имеющихся серверов: 0"),
            customFieldWarnings = listOf(CustomFieldWarning(0, listOf("custom_config")), CustomFieldWarning(1, emptyList())),
        )
        val s = (ImportOutcomes.saved(r) as ImportResult.Report).report as ImportReport.Servers
        assertEquals(1, s.xhttp)
        assertEquals(1, s.customDropped)
    }

    @Test
    fun previewKeepsTheKindTheDistinctTypesAndTheHint() {
        val r = ImportContentResponse(
            kind = ContentKind.CONTENT_KIND_HAPP,
            parsedProfiles = listOf(p(), p(ProfileType.PROFILE_TYPE_TROJAN), p(), p(ProfileType.PROFILE_TYPE_UNSPECIFIED)),
            hint = " ",
        )
        val v = ImportOutcomes.preview(r)
        assertEquals(4, v.count)
        assertEquals(listOf(ProfileType.PROFILE_TYPE_VLESS, ProfileType.PROFILE_TYPE_TROJAN), v.types)
        assertNull(v.hint)
        assertFalse(v.isSubscription)
        assertTrue(ImportOutcomes.preview(ImportContentResponse(kind = ContentKind.CONTENT_KIND_SUBSCRIPTION_URL)).isSubscription)
    }

    @Test
    fun everySubscribeResultMapsToOneOutcome() {
        val failure = SubFailure.fromCode("403")
        assertEquals(
            ImportResult.Report(ImportReport.Subscribed("n", true, 1, 2, 3, 4)),
            ImportOutcomes.subscribed(SubscribeResult.Fetched("n", true, 1, 2, 3, 4)),
        )
        assertEquals(ImportResult.Report(ImportReport.SubscriptionConfirm("n", 5)), ImportOutcomes.subscribed(SubscribeResult.NeedsConfirm("n", true, 5)))
        assertEquals(
            ImportResult.Problem(ImportProblem.NotFetched("n", false, SubErrorKind.Auth, "403", true)),
            ImportOutcomes.subscribed(SubscribeResult.NotFetched("n", false, failure, hwidHint = true)),
        )
        assertEquals(ImportResult.Rejected(ErrorKind.InvalidInput, 2), ImportOutcomes.subscribed(SubscribeResult.Refused(ErrorKind.InvalidInput, 2)))
        assertEquals(ImportResult.Problem(ImportProblem.Busy), ImportOutcomes.subscribed(SubscribeResult.Busy))
    }

    @Test
    fun onlyBadInputBlocksTheButton() {
        assertTrue(PreviewError(ErrorKind.InvalidInput, null, happ = false).blocksImport)
        assertFalse(PreviewError(ErrorKind.CoreUnavailable, null, happ = false).blocksImport)
        assertFalse(PreviewError(ErrorKind.Generic, null, happ = false).blocksImport)
    }
}

class SubscriptionAddressTests {
    @Test
    fun sameUrlIgnoresCaseOfSchemeAndHostAndALoneTrailingSlash() {
        assertTrue(SubscriptionLogic.sameUrl("HTTPS://Provider.Example/sub/T", "https://provider.example/sub/T"))
        assertTrue(SubscriptionLogic.sameUrl("https://p.example/", "https://p.example"))
        assertTrue(SubscriptionLogic.sameUrl("  https://p.example:8443/a?b=1 ", "https://p.example:8443/a?b=1"))
    }

    @Test
    fun sameUrlKeepsTheTokenCaseSensitiveAndDistinguishesPathsAndQueries() {
        assertFalse(SubscriptionLogic.sameUrl("https://p.example/sub/Abc", "https://p.example/sub/abc"))
        assertFalse(SubscriptionLogic.sameUrl("https://p.example/sub?t=1", "https://p.example/sub?t=2"))
        assertFalse(SubscriptionLogic.sameUrl("https://p.example/a", "https://p.example/a/"))
        assertFalse(SubscriptionLogic.sameUrl("http://p.example/a", "https://p.example/a"))
    }

    @Test
    fun sameUrlNeverMatchesNothingWithNothing() {
        assertFalse(SubscriptionLogic.sameUrl(null, null))
        assertFalse(SubscriptionLogic.sameUrl("", ""))
        assertFalse(SubscriptionLogic.sameUrl(null, "https://p.example/a"))
    }

    @Test
    fun theCoresStubAndEmptyAnswersHaveTheirOwnKinds() {
        assertEquals("format", SubscriptionLogic.errorCode("подписка 7: разбор: ответ похож на HTML-заглушку (captive portal / DPI)"))
        assertEquals("format", SubscriptionLogic.errorCode("подписка 7: разбор: формат подписки не распознан (начало: abc)"))
        assertEquals("format", SubscriptionLogic.errorCode("не распознано ни одного профиля (первая ошибка: строка 1: x)"))
        assertEquals("zero", SubscriptionLogic.errorCode("подписка 7: разбор: пустой ответ подписки"))
        assertEquals(SubErrorKind.Format, SubscriptionLogic.kindOf("format"))
    }

    @Test
    fun theHttpStatusStillWinsOverTheWordsAroundIt() {
        assertEquals("403", SubscriptionLogic.errorCode("подписка: HTTP 403 Forbidden"))
    }

    @Test
    fun skippedLinesAreReadFromTheCoresNote() {
        assertEquals(3, SubscriptionLogic.skippedLinesOf("не разобрано строк ответа: 3 (причины — в журнале)"))
        assertNull(SubscriptionLogic.skippedLinesOf("не разобрано строк ответа: 0 (причины — в журнале)"))
        assertNull(SubscriptionLogic.skippedLinesOf(null))
        assertNull(SubscriptionLogic.skippedLinesOf("something else"))
    }
}
