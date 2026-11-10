// SPDX-License-Identifier: GPL-3.0-only
package io.github.anatolijlaptev1991_ctrl.lavray.ui.importer

import android.content.Context
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.runtime.Composable
import androidx.compose.ui.res.pluralStringResource
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.unit.dp
import io.github.anatolijlaptev1991_ctrl.lavray.R
import io.github.anatolijlaptev1991_ctrl.lavray.ui.core.ErrorKind
import io.github.anatolijlaptev1991_ctrl.lavray.ui.core.textRes
import io.github.anatolijlaptev1991_ctrl.lavray.ui.design.Lav
import io.github.anatolijlaptev1991_ctrl.lavray.ui.design.components.ButtonVariant
import io.github.anatolijlaptev1991_ctrl.lavray.ui.design.components.LavBanner
import io.github.anatolijlaptev1991_ctrl.lavray.ui.design.components.LavButton
import io.github.anatolijlaptev1991_ctrl.lavray.ui.design.components.StatusTone
import io.github.anatolijlaptev1991_ctrl.lavray.ui.design.LavText
import io.github.anatolijlaptev1991_ctrl.lavray.ui.screens.servers.label
import io.github.anatolijlaptev1991_ctrl.lavray.ui.screens.subscriptions.UpdatePhase
import io.github.anatolijlaptev1991_ctrl.lavray.ui.screens.subscriptions.labelRes
import io.github.anatolijlaptev1991_ctrl.lavray.ui.screens.subscriptions.textRes

/** What the sheet says under the text field about what the core will do, and what it said about the last import. */

/** The core's reading of the text: what it is, how many servers, and what it warns about. */
@Composable
internal fun CoreSummary(preview: CorePreview, input: ImportInput) {
    Column(verticalArrangement = Arrangement.spacedBy(8.dp)) {
        if (preview.isSubscription) {
            LavText(stringResource(R.string.imp_pv_sub), style = Lav.type.bodyStrong)
            val insecure = (input as? ImportInput.SubscriptionUrl)?.insecure == true
            if (insecure) LavBanner(StatusTone.Connecting, stringResource(R.string.imp_httpWarn))
        } else {
            val types = preview.types.map { it.label() }
            LavText(
                pluralStringResource(R.plurals.pl_srv, preview.count, preview.count) +
                    if (types.isEmpty()) "" else " · " + types.joinToString(", "),
                style = Lav.type.bodyStrong,
            )
        }
        if (preview.xhttp > 0) LavBanner(StatusTone.Connecting, stringResource(R.string.imp_warn_xhttp, preview.xhttp.toString()))
        if (preview.customDropped > 0) LavBanner(StatusTone.Connecting, stringResource(R.string.imp_cfgIgnored))
        preview.hint?.let { LavText(it, style = Lav.type.small, color = Lav.colors.textSecondary) }
    }
}

/** The core refused to preview the text. A happ link it could not open says how to fix it; the rest uses the common error words. */
@Composable
internal fun PreviewErrorBanner(error: PreviewError) {
    when {
        error.happ && error.kind == ErrorKind.InvalidInput ->
            LavBanner(StatusTone.Error, stringResource(R.string.imp_happErrT), text = stringResource(R.string.imp_happErrCtx))
        error.line != null -> LavBanner(StatusTone.Error, stringResource(R.string.imp_err_line, error.line.toString()))
        else -> LavBanner(StatusTone.Error, stringResource(error.kind.textRes()))
    }
}

/** Why the last import brought nothing in. The subscription itself, when there is one, is saved: the words say that and what to do. */
@Composable
internal fun ProblemBanner(problem: ImportProblem, onOpenSubscriptions: () -> Unit) {
    when (problem) {
        is ImportProblem.AlreadyHave -> LavBanner(
            StatusTone.Connecting, stringResource(R.string.imp_p_nothing), text = stringResource(R.string.imp_p_dup, problem.count.toString()),
        )
        is ImportProblem.NothingFound -> LavBanner(
            StatusTone.Connecting, stringResource(R.string.imp_p_nothing),
            text = listOfNotNull(stringResource(R.string.imp_p_none), problem.hint).joinToString("\n"),
        )
        ImportProblem.Busy -> LavBanner(StatusTone.Connecting, stringResource(R.string.imp_p_busy))
        is ImportProblem.NotFetched -> NotFetchedBanner(problem, onOpenSubscriptions)
    }
}

@Composable
private fun NotFetchedBanner(p: ImportProblem.NotFetched, onOpenSubscriptions: () -> Unit) {
    val reason = stringResource(p.kind.textRes()) + if (p.code.all { it.isDigit() }) " (${p.code})" else ""
    val hwid = if (p.hwidHint) stringResource(R.string.imp_p_hwid, stringResource(R.string.sub_set_hwid)) else null
    LavBanner(
        StatusTone.Error,
        stringResource(if (p.existed) R.string.imp_p_nf_again else R.string.imp_p_nf_new, p.name),
        text = listOfNotNull(reason, hwid, stringResource(R.string.imp_p_nf_retry)).joinToString("\n"),
        actions = { LavButton(stringResource(R.string.imp_p_open), onOpenSubscriptions, variant = ButtonVariant.Primary) },
    )
}

/** The line shown while the core works: what it is doing with the subscription, when it is one. */
@Composable
internal fun WorkingLine(fetching: Boolean, phase: UpdatePhase?) {
    val text = if (fetching) stringResource(R.string.imp_stage_fetch, stringResource(phase.labelRes())) else stringResource(R.string.imp_stage_import)
    LavText(text, style = Lav.type.small, color = Lav.colors.textSecondary)
}

/** The toast text for what an import did. [context] is the app's, since the toast lives outside any composition. */
internal fun ImportReport.toastText(context: Context): String = when (this) {
    is ImportReport.Servers -> listOfNotNull(
        context.getString(R.string.toast_imported, added.toString()) + ".",
        if (alreadyHad > 0) context.getString(R.string.imp_done_dup, alreadyHad.toString()) else null,
        if (xhttp > 0) context.getString(R.string.imp_warn_xhttp, xhttp.toString()) else null,
        if (customDropped > 0) context.getString(R.string.imp_cfgIgnored) else null,
    ).joinToString(" ")
    is ImportReport.Subscribed -> listOfNotNull(
        if (existed) {
            context.getString(R.string.imp_done_sub_again, added.toString(), name, removed.toString(), total.toString())
        } else {
            context.getString(R.string.imp_done_sub, name, total.toString())
        },
        if (unparsedLines > 0) context.getString(R.string.imp_done_unparsed, unparsedLines.toString()) else null,
    ).joinToString(" ")
    is ImportReport.SubscriptionConfirm -> context.getString(R.string.imp_done_confirm, name, remove.toString())
}

/** The toast text for an import that brought nothing in after the sheet was closed. */
internal fun ImportProblem.toastText(context: Context): String = when (this) {
    is ImportProblem.AlreadyHave -> context.getString(R.string.imp_p_dup, count.toString())
    is ImportProblem.NothingFound -> context.getString(R.string.imp_p_none)
    ImportProblem.Busy -> context.getString(R.string.imp_p_busy)
    is ImportProblem.NotFetched -> context.getString(if (existed) R.string.imp_p_nf_again else R.string.imp_p_nf_new, name) +
        ". " + context.getString(R.string.imp_p_nf_retry)
}
