// SPDX-License-Identifier: GPL-3.0-only
package io.github.anatolijlaptev1991_ctrl.lavray.ui.screens.connection

import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.FlowRow
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.runtime.Composable
import androidx.compose.runtime.remember
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.LocalConfiguration
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.semantics.semantics
import androidx.compose.ui.text.style.TextAlign
import androidx.compose.ui.unit.dp
import io.github.anatolijlaptev1991_ctrl.lavray.R
import io.github.anatolijlaptev1991_ctrl.lavray.ui.design.Lav
import io.github.anatolijlaptev1991_ctrl.lavray.ui.design.LavIcon
import io.github.anatolijlaptev1991_ctrl.lavray.ui.design.LavIconView
import io.github.anatolijlaptev1991_ctrl.lavray.ui.design.LavText
import io.github.anatolijlaptev1991_ctrl.lavray.ui.design.components.ButtonVariant
import io.github.anatolijlaptev1991_ctrl.lavray.ui.design.components.LavBanner
import io.github.anatolijlaptev1991_ctrl.lavray.ui.design.components.LavButton
import io.github.anatolijlaptev1991_ctrl.lavray.ui.design.components.LavCard
import io.github.anatolijlaptev1991_ctrl.lavray.ui.design.components.LavIconButton
import io.github.anatolijlaptev1991_ctrl.lavray.ui.design.components.Spinner
import io.github.anatolijlaptev1991_ctrl.lavray.ui.design.components.StatusTone
import io.github.anatolijlaptev1991_ctrl.lavray.ui.i18n.UnitFormat
import java.text.DateFormat
import java.util.Date

/** What a card shows of the probe: a finished report (maybe being refreshed), a pass under way, or a quiet failure. */
private data class ProbeView(val report: ExitReport?, val running: Boolean, val failed: Boolean)

private fun viewOf(state: ProbeState, key: String?): ProbeView {
    if (key == null || state.key != key) return ProbeView(null, running = false, failed = false)
    return when (state) {
        is ProbeState.Ready -> ProbeView(state.report, running = false, failed = false)
        is ProbeState.Running -> ProbeView(state.previous, running = true, failed = false)
        is ProbeState.Failed -> ProbeView(null, running = false, failed = true)
        ProbeState.Idle -> ProbeView(null, running = false, failed = false)
    }
}

/** Exit address through the tunnel, the country of the server and the quality figures that were measured. */
@Composable
fun ExitCard(state: ProbeState, key: String?, server: ServerSummary?, fmt: UnitFormat, onRefresh: () -> Unit) {
    val view = viewOf(state, key)
    val c = Lav.colors
    LavCard(Modifier.fillMaxWidth()) {
        Column(Modifier.padding(16.dp), verticalArrangement = Arrangement.spacedBy(12.dp)) {
            CardTitle(LavIcon.Globe, stringResource(R.string.cx_exit_t)) {
                LavIconButton(LavIcon.Refresh, stringResource(R.string.health_ip_refresh), onRefresh, enabled = !view.running && key != null)
            }
            when {
                // No line limit: an IPv6 address or a very large font would otherwise lose its last digits.
                view.report?.exitIp != null -> LavText(view.report.exitIp, style = Lav.type.mono.copy(fontSize = IP_SIZE))
                view.running -> Checking()
                view.failed -> QuietFailure(onRefresh)
                view.report != null -> LavText(stringResource(R.string.st_na), style = Lav.type.small, color = c.textSecondary)
                else -> Checking()
            }
            val rows = exitRows(server, view.report, fmt)
            if (rows.isNotEmpty()) {
                Column(verticalArrangement = Arrangement.spacedBy(8.dp)) { rows.forEach { (label, value) -> InfoRow(label, value) } }
            }
        }
    }
}

@Composable
private fun exitRows(server: ServerSummary?, report: ExitReport?, fmt: UnitFormat): List<Pair<String, String>> {
    val locale = LocalConfiguration.current.locales[0]
    val country = server?.geo?.let { code ->
        Geo.name(code, locale)?.let { name -> listOfNotNull(Geo.flag(code), name).joinToString(" ") }
    }
    return listOfNotNull(
        country?.let { stringResource(R.string.cx_country_server) to it },
        report?.latencyMs?.let { stringResource(R.string.health_latency) to fmt.latency(it) },
        report?.dns?.let { stringResource(R.string.health_dns) to dnsText(it) },
    )
}

@Composable
private fun dnsText(label: DnsLabel): String = when (label) {
    is DnsLabel.Card -> stringResource(label.card.title)
    is DnsLabel.Host -> label.host
    DnsLabel.Advanced -> stringResource(R.string.dns_adv)
}

/** Leak status: the two addresses that were compared, a plain verdict, and what the phone cannot measure. */
@Composable
fun LeakCard(state: ProbeState, key: String?, onCheck: () -> Unit) {
    val view = viewOf(state, key)
    val c = Lav.colors
    val report = view.report
    LavCard(Modifier.fillMaxWidth()) {
        Column(Modifier.padding(16.dp), verticalArrangement = Arrangement.spacedBy(12.dp)) {
            CardTitle(LavIcon.Shield, stringResource(R.string.leak_title)) {
                LavButton(stringResource(R.string.leak_run), onCheck, loading = view.running, enabled = key != null && !view.running)
            }
            when {
                report != null -> LeakBody(report)
                view.failed -> QuietFailure(onCheck)
                else -> Checking()
            }
            if (report != null) {
                LavText(
                    stringResource(R.string.leak_last, timeOfDay(report.checkedAtMs)),
                    style = Lav.type.caption, color = c.textTertiary,
                )
            }
        }
    }
}

@Composable
private fun LeakBody(report: ExitReport) {
    val c = Lav.colors
    val (tone, icon, text) = when (report.leak) {
        LeakStatus.Clear -> Triple(StatusTone.Connected, LavIcon.Check, stringResource(R.string.cx_leak_clear))
        LeakStatus.Visible -> Triple(StatusTone.Connecting, LavIcon.Alert, stringResource(R.string.chk_leak_same))
        LeakStatus.Unchecked -> Triple(StatusTone.Neutral, LavIcon.Info, stringResource(R.string.cx_leak_unchecked))
    }
    Row(
        Modifier.fillMaxWidth().semantics(mergeDescendants = true) {},
        horizontalArrangement = Arrangement.spacedBy(10.dp), verticalAlignment = Alignment.Top,
    ) {
        LavIconView(icon, tone.text(c), size = 20.dp, modifier = Modifier.padding(top = 1.dp))
        LavText(text, Modifier.weight(1f), style = Lav.type.body)
    }
    LavText(stringResource(R.string.chk_leak_scope), style = Lav.type.caption, color = c.textTertiary)
}

@Composable
private fun Checking() {
    Row(horizontalArrangement = Arrangement.spacedBy(8.dp), verticalAlignment = Alignment.CenterVertically) {
        Spinner(Lav.colors.textTertiary, size = 16.dp)
        LavText(stringResource(R.string.chk_st_run), style = Lav.type.small, color = Lav.colors.textSecondary)
    }
}

/** A check that did not complete: said once, in neutral colours, with a way to try again. Nothing is claimed about the tunnel. */
@Composable
private fun QuietFailure(onRetry: () -> Unit) {
    Row(Modifier.fillMaxWidth(), horizontalArrangement = Arrangement.SpaceBetween, verticalAlignment = Alignment.CenterVertically) {
        LavText(stringResource(R.string.cx_probe_fail), Modifier.weight(1f).padding(end = 8.dp), style = Lav.type.small, color = Lav.colors.textSecondary)
        LavButton(stringResource(R.string.err_retry), onRetry, variant = ButtonVariant.Ghost)
    }
}

@Composable
private fun CardTitle(icon: LavIcon, title: String, action: @Composable () -> Unit) {
    FlowRow(
        Modifier.fillMaxWidth(),
        horizontalArrangement = Arrangement.SpaceBetween,
        verticalArrangement = Arrangement.spacedBy(4.dp),
        itemVerticalAlignment = Alignment.CenterVertically,
    ) {
        Row(verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(10.dp)) {
            LavIconView(icon, Lav.colors.textSecondary, size = 20.dp)
            LavText(title, style = Lav.type.h2)
        }
        action()
    }
}

@Composable
private fun InfoRow(label: String, value: String) {
    Row(
        Modifier.fillMaxWidth().semantics(mergeDescendants = true) {},
        horizontalArrangement = Arrangement.SpaceBetween, verticalAlignment = Alignment.CenterVertically,
    ) {
        LavText(label, style = Lav.type.small, color = Lav.colors.textTertiary)
        LavText(value, Modifier.weight(1f).padding(start = 12.dp), style = Lav.type.bodyStrong, textAlign = TextAlign.End)
    }
}

@Composable
private fun timeOfDay(ms: Long): String {
    val locale = LocalConfiguration.current.locales[0]
    return remember(ms, locale) { DateFormat.getTimeInstance(DateFormat.SHORT, locale).format(Date(ms)) }
}

/** The three shortcuts of the desktop's quick row: check servers, add a server, update subscriptions. */
@Composable
fun QuickActions(
    testing: Boolean,
    testProgress: String?,
    updatingSubs: Boolean,
    canTest: Boolean,
    canUpdateSubs: Boolean,
    onTest: () -> Unit,
    onAdd: () -> Unit,
    onSubs: () -> Unit,
) {
    // Two to a row, each filling its share, so the third does not hang alone at the left edge.
    FlowRow(
        Modifier.fillMaxWidth(),
        horizontalArrangement = Arrangement.spacedBy(8.dp), verticalArrangement = Arrangement.spacedBy(8.dp),
        maxItemsInEachRow = 2,
    ) {
        LavButton(
            testProgress ?: stringResource(R.string.quick_test), onTest, Modifier.weight(1f),
            icon = LavIcon.Gauge, loading = testing, enabled = canTest && !testing,
        )
        LavButton(stringResource(R.string.quick_import), onAdd, Modifier.weight(1f), icon = LavIcon.Plus)
        LavButton(
            stringResource(R.string.quick_subs), onSubs, Modifier.weight(1f),
            icon = LavIcon.Rss, loading = updatingSubs, enabled = canUpdateSubs && !updatingSubs,
        )
    }
}

private val IP_SIZE = androidx.compose.ui.unit.TextUnit(20f, androidx.compose.ui.unit.TextUnitType.Sp)
