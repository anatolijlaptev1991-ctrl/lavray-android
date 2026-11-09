// SPDX-License-Identifier: GPL-3.0-only
package io.github.anatolijlaptev1991_ctrl.lavray.ui.screens.subscriptions

import androidx.compose.foundation.background
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.runtime.Composable
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.semantics.clearAndSetSemantics
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import io.github.anatolijlaptev1991_ctrl.lavray.R
import io.github.anatolijlaptev1991_ctrl.lavray.ui.design.Lav
import io.github.anatolijlaptev1991_ctrl.lavray.ui.design.LavIcon
import io.github.anatolijlaptev1991_ctrl.lavray.ui.design.LavIconView
import io.github.anatolijlaptev1991_ctrl.lavray.ui.design.LavText
import io.github.anatolijlaptev1991_ctrl.lavray.ui.design.components.LavBadge
import io.github.anatolijlaptev1991_ctrl.lavray.ui.design.components.LavCard
import io.github.anatolijlaptev1991_ctrl.lavray.ui.design.components.LavIconButton
import io.github.anatolijlaptev1991_ctrl.lavray.ui.design.components.LavSwitchRow
import io.github.anatolijlaptev1991_ctrl.lavray.ui.design.components.Spinner
import io.github.anatolijlaptev1991_ctrl.lavray.ui.design.components.StatusTone
import io.github.anatolijlaptev1991_ctrl.lavray.ui.i18n.UnitFormat

/** Callbacks of one card; the screen owns what each does. */
class CardActions(
    val onSettings: (SubRow) -> Unit,
    val onToggleAuto: (SubRow) -> Unit,
    val onUpdate: (SubRow) -> Unit,
    val onMenu: (SubRow) -> Unit,
)

/** What a card needs to draw besides the row: formatters and the state of a running update. */
class CardEnv(
    val fmt: UnitFormat,
    val relative: (Long) -> String,
    val nowMs: Long,
    val busyOn: Boolean,
    val progress: UpdateProgress?,
    val otherBusy: Boolean,
)

/**
 * One subscription: name, last error, address (without its token), servers, update times, expiry, traffic
 * with a bar against the quota, the auto-update switch and the actions. Touch targets are 48 dp.
 */
@Composable
fun SubscriptionCard(row: SubRow, env: CardEnv, actions: CardActions, modifier: Modifier = Modifier) {
    val c = Lav.colors
    LavCard(modifier.fillMaxWidth(), strong = row.failure != null) {
        Column(Modifier.padding(horizontal = 14.dp, vertical = 10.dp), verticalArrangement = Arrangement.spacedBy(8.dp)) {
            CardHeader(row, env, actions)
            if (row.url.isNotEmpty()) {
                LavText(SubscriptionLogic.displayUrl(row.url), style = Lav.type.mono, color = c.textTertiary, maxLines = 1, overflow = TextOverflow.Ellipsis)
            }
            MetaRows(row, env)
            row.failure?.let { FailureLine(it) }
            Traffic(row, env.fmt)
            if (env.busyOn) ProgressLine(env.progress)
            LavSwitchRow(stringResource(R.string.subs_auto), row.autoUpdate, { actions.onToggleAuto(row) })
        }
    }
}

@Composable
private fun CardHeader(row: SubRow, env: CardEnv, actions: CardActions) {
    Row(verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(4.dp)) {
        Column(Modifier.weight(1f), verticalArrangement = Arrangement.spacedBy(4.dp)) {
            LavText(row.name, style = Lav.type.h2, maxLines = 2, overflow = TextOverflow.Ellipsis)
            if (row.failure != null) LavBadge(stringResource(R.string.sub_err_badge), tone = StatusTone.Error)
        }
        if (env.busyOn) {
            Box(Modifier.padding(12.dp)) { Spinner(Lav.colors.accent, size = 24.dp) }
        } else {
            LavIconButton(LavIcon.Refresh, stringResource(R.string.common_refresh), { actions.onUpdate(row) }, enabled = !env.otherBusy)
        }
        LavIconButton(LavIcon.Sliders, stringResource(R.string.sub_settings), { actions.onSettings(row) })
        LavIconButton(LavIcon.More, stringResource(R.string.sa_actions), { actions.onMenu(row) })
    }
}

@Composable
private fun MetaRows(row: SubRow, env: CardEnv) {
    Column(verticalArrangement = Arrangement.spacedBy(2.dp)) {
        MetaRow(stringResource(R.string.nav_servers)) { MetaValue(row.count.toString()) }
        MetaRow(stringResource(R.string.subs_lastUp)) { MetaValue(row.lastUpdateMs?.let(env.relative) ?: "—") }
        if (row.failure == null) {
            MetaRow(stringResource(R.string.subs_next)) {
                MetaValue(row.nextUpdateMs?.let { env.relative(maxOf(it, env.nowMs)) } ?: "—")
            }
        }
        MetaRow(stringResource(R.string.subs_expiry)) { Expiry(row, env) }
    }
}

@Composable
private fun Expiry(row: SubRow, env: CardEnv) {
    if (row.expireMs <= 0) {
        MetaValue(stringResource(R.string.sub_noExpiry))
        return
    }
    Row(horizontalArrangement = Arrangement.spacedBy(6.dp), verticalAlignment = Alignment.CenterVertically) {
        MetaValue(stringResource(R.string.sub_expires, env.relative(row.expireMs)))
        if (SubscriptionLogic.expiresSoon(row.expireMs, env.nowMs)) {
            LavBadge(stringResource(R.string.sub_expiresSoon), tone = StatusTone.Connecting)
        }
    }
}

@Composable
private fun MetaRow(label: String, value: @Composable () -> Unit) {
    Row(Modifier.fillMaxWidth(), horizontalArrangement = Arrangement.SpaceBetween, verticalAlignment = Alignment.CenterVertically) {
        LavText(label, Modifier.weight(1f), style = Lav.type.small, color = Lav.colors.textTertiary)
        value()
    }
}

@Composable
private fun MetaValue(text: String) = LavText(text, style = Lav.type.small, color = Lav.colors.textSecondary)

@Composable
private fun FailureLine(failure: SubFailure) {
    val c = Lav.colors
    val text = stringResource(failure.kind.textRes())
    Row(horizontalArrangement = Arrangement.spacedBy(8.dp), verticalAlignment = Alignment.Top) {
        LavIconView(LavIcon.Alert, c.errorText, size = 18.dp)
        LavText(
            if (failure.showCode) "$text (${failure.code})" else text,
            Modifier.weight(1f), style = Lav.type.small, color = c.errorText,
        )
    }
}

/** Dictionary text of a failed update, by kind. */
fun SubErrorKind.textRes(): Int = when (this) {
    SubErrorKind.Auth -> R.string.sub_err_auth
    SubErrorKind.NotFound -> R.string.sub_err_notFound
    SubErrorKind.Timeout -> R.string.sub_err_timeout
    SubErrorKind.Zero -> R.string.sub_err_zero
    SubErrorKind.Generic -> R.string.sub_err_generic
}

@Composable
private fun Traffic(row: SubRow, fmt: UnitFormat) {
    val c = Lav.colors
    Column(verticalArrangement = Arrangement.spacedBy(4.dp)) {
        Row(Modifier.fillMaxWidth(), horizontalArrangement = Arrangement.SpaceBetween) {
            LavText(stringResource(R.string.subs_traffic), style = Lav.type.small, color = c.textTertiary)
            LavText("↓ ${fmt.bytes(row.downloadBytes)} · ↑ ${fmt.bytes(row.uploadBytes)}", style = Lav.type.small, color = c.textPrimary)
        }
        UsageBar(row.usedPercent / 100f)
        val nb = " "
        LavText(
            if (row.totalBytes > 0) "${fmt.bytes(row.usedBytes)}$nb${stringResource(R.string.subs_of)}$nb${fmt.bytes(row.totalBytes)}" else stringResource(R.string.sub_unlimited),
            style = Lav.type.caption, color = c.textTertiary,
        )
    }
}

@Composable
private fun UsageBar(fraction: Float) {
    val c = Lav.colors
    // Decorative: the figures next to it carry the meaning for TalkBack.
    Box(Modifier.fillMaxWidth().height(4.dp).background(c.bgSurface2, RoundedCornerShape(2.dp)).clearAndSetSemantics { }) {
        Box(Modifier.fillMaxWidth(fraction.coerceIn(0f, 1f)).height(4.dp).background(c.accent, RoundedCornerShape(2.dp)))
    }
}

@Composable
private fun ProgressLine(p: UpdateProgress?) {
    val c = Lav.colors
    val phase = p?.phase
    val head = stringResource(phase.labelRes())
    val counts = if (p?.added != null || p?.removed != null || p?.total != null) {
        stringResource(R.string.sa_progress_counts, (p.added ?: 0).toString(), (p.removed ?: 0).toString(), (p.total ?: 0).toString())
    } else {
        null
    }
    Column(verticalArrangement = Arrangement.spacedBy(2.dp)) {
        LavText(head, style = Lav.type.small, color = c.textSecondary)
        if (counts != null) LavText(counts, style = Lav.type.caption, color = c.textTertiary)
    }
}

fun UpdatePhase?.labelRes(): Int = when (this) {
    UpdatePhase.Requesting -> R.string.sa_phase_requesting
    UpdatePhase.Decoding -> R.string.sa_phase_decoding
    UpdatePhase.Parsing -> R.string.sa_phase_parsing
    UpdatePhase.Diffing -> R.string.sa_phase_diffing
    UpdatePhase.Saving -> R.string.sa_phase_saving
    null -> R.string.sa_phase_started
}
