// SPDX-License-Identifier: GPL-3.0-only
package io.github.anatolijlaptev1991_ctrl.lavray.ui.importer

import androidx.compose.runtime.Immutable
import io.github.anatolijlaptev1991_ctrl.lavray.api.gen.ImportContentResponse
import io.github.anatolijlaptev1991_ctrl.lavray.api.gen.ProfileType
import io.github.anatolijlaptev1991_ctrl.lavray.ui.core.ErrorKind
import io.github.anatolijlaptev1991_ctrl.lavray.ui.screens.subscriptions.SubErrorKind
import io.github.anatolijlaptev1991_ctrl.lavray.ui.screens.subscriptions.SubNotes
import io.github.anatolijlaptev1991_ctrl.lavray.ui.screens.subscriptions.SubscribeResult

/** What the sheet closes with: the person is told, in a toast, what really changed. */
@Immutable
sealed interface ImportReport {
    /** Servers were saved. [alreadyHad] of the parsed ones were in the list already; [xhttp] cannot be connected to; [customDropped] had their own core settings cut. */
    data class Servers(val added: Int, val alreadyHad: Int, val xhttp: Int, val customDropped: Int) : ImportReport

    /** A subscription was added (or found again) and its servers loaded. */
    data class Subscribed(
        val name: String, val existed: Boolean, val added: Int, val removed: Int, val total: Int, val unparsedLines: Int,
        val notes: SubNotes = SubNotes(),
    ) : ImportReport

    /** The subscription exists and its update would remove servers: the question waits on the Subscriptions screen. */
    data class SubscriptionConfirm(val name: String, val remove: Int, val notes: SubNotes = SubNotes()) : ImportReport

    /** The subscription is saved and the core is already updating it (its own scheduler): the servers appear when that ends. */
    data class SubscriptionUpdating(val name: String, val notes: SubNotes = SubNotes()) : ImportReport
}

/** Why nothing was imported although the core did not refuse: the sheet stays open and says so. */
@Immutable
sealed interface ImportProblem {
    /** Every server in the text is in the list already. */
    data class AlreadyHave(val count: Int) : ImportProblem

    /** The core found no server in the text; [hint] is what it said about it, if anything. */
    data class NothingFound(val hint: String?) : ImportProblem

    /** An update of subscriptions is running; the link was not touched. */
    data object Busy : ImportProblem

    /**
     * The subscription [name] is saved, its servers are not: [kind] and [code] say why, [hwidHint] points at the device-limit
     * switch. [coreProblem] is set when the core itself did not answer: the reason is then the core, not the provider.
     */
    data class NotFetched(
        val name: String, val existed: Boolean, val kind: SubErrorKind, val code: String, val hwidHint: Boolean,
        val coreProblem: ErrorKind? = null, val notes: SubNotes = SubNotes(),
    ) : ImportProblem

    /** The core refused the address of a subscription link: a local or private address, or not a valid web address. */
    data object BadAddress : ImportProblem
}

/** The final state of one import: close with a report, stay open with a problem, or stay open with a core error. */
sealed interface ImportResult {
    data class Report(val report: ImportReport) : ImportResult
    data class Problem(val problem: ImportProblem) : ImportResult
    data class Rejected(val kind: ErrorKind, val line: Int?) : ImportResult
}

/** What the core's own preview says it will do with the text; shown instead of the local guess once it arrives. */
@Immutable
data class CorePreview(
    val kind: ImportContentResponse.ContentKind,
    val count: Int,
    val types: List<ProfileType>,
    val xhttp: Int,
    val customDropped: Int,
    val hint: String?,
) {
    val isSubscription: Boolean get() = kind == ImportContentResponse.ContentKind.CONTENT_KIND_SUBSCRIPTION_URL
}

/** A preview the core refused. A happ link it could not open gets its own words. */
@Immutable
data class PreviewError(val kind: ErrorKind, val line: Int?, val happ: Boolean) {
    /** Text that is wrong as it stands: sending it again changes nothing. */
    val blocksImport: Boolean get() = kind == ErrorKind.InvalidInput
}

/** Reads a core answer into what the person is told. Pure: every branch is a test. */
object ImportOutcomes {
    private val xhttpLine = Regex("^xhttp_unsupported:")

    fun preview(r: ImportContentResponse): CorePreview = CorePreview(
        kind = r.kind,
        count = r.parsedProfiles.size,
        types = r.parsedProfiles.map { it.type }.filter { it != ProfileType.PROFILE_TYPE_UNSPECIFIED }.distinct(),
        xhttp = xhttpCount(r),
        customDropped = r.customFieldWarnings.count { it.droppedFields.isNotEmpty() },
        hint = r.hint?.takeIf { it.isNotBlank() },
    )

    /**
     * The answer to a real import of something that is not a subscription link. [ImportContentResponse.addedProfileIds] is what was
     * saved; the parsed list also holds the servers the core skipped as already present, so the difference is "already had".
     * An answer that saved nothing is a problem, never a success.
     */
    fun saved(r: ImportContentResponse): ImportResult {
        val added = r.addedProfileIds.size
        val parsed = r.parsedProfiles.size
        if (added == 0) {
            return ImportResult.Problem(if (parsed > 0) ImportProblem.AlreadyHave(parsed) else ImportProblem.NothingFound(r.hint?.takeIf { it.isNotBlank() }))
        }
        return ImportResult.Report(
            ImportReport.Servers(
                added = added,
                alreadyHad = (parsed - added).coerceAtLeast(0),
                xhttp = xhttpCount(r),
                customDropped = r.customFieldWarnings.count { it.droppedFields.isNotEmpty() },
            ),
        )
    }

    fun subscribed(r: SubscribeResult): ImportResult = when (r) {
        is SubscribeResult.Fetched -> ImportResult.Report(ImportReport.Subscribed(r.name, r.existed, r.added, r.removed, r.total, r.unparsedLines, r.notes))
        is SubscribeResult.NeedsConfirm -> ImportResult.Report(ImportReport.SubscriptionConfirm(r.name, r.remove, r.notes))
        is SubscribeResult.Updating -> ImportResult.Report(ImportReport.SubscriptionUpdating(r.name, r.notes))
        is SubscribeResult.NotFetched ->
            ImportResult.Problem(ImportProblem.NotFetched(r.name, r.existed, r.failure.kind, r.failure.code, r.hwidHint, r.coreProblem, r.notes))
        // The core already said it is a subscription link, so a refusal of the address is about the address, not "the input could not be read".
        is SubscribeResult.Refused ->
            if (r.kind == ErrorKind.InvalidInput) ImportResult.Problem(ImportProblem.BadAddress) else ImportResult.Rejected(r.kind, r.line)
        SubscribeResult.Busy -> ImportResult.Problem(ImportProblem.Busy)
    }

    /** One line of the core's import log per server it will not be able to connect to; the line starts with its code. */
    private fun xhttpCount(r: ImportContentResponse): Int = r.log.count { xhttpLine.containsMatchIn(it) }
}
