// SPDX-License-Identifier: GPL-3.0-only
package io.github.anatolijlaptev1991_ctrl.lavray.ui.screens.diag

import android.content.ActivityNotFoundException
import android.content.Context
import android.content.Intent
import android.provider.Settings
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.FlowRow
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.runtime.Composable
import androidx.compose.runtime.DisposableEffect
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.semantics.semantics
import androidx.compose.ui.text.input.ImeAction
import androidx.compose.ui.text.input.KeyboardType
import androidx.compose.foundation.text.KeyboardActions
import androidx.compose.foundation.text.KeyboardOptions
import androidx.compose.ui.unit.dp
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import io.github.anatolijlaptev1991_ctrl.lavray.R
import io.github.anatolijlaptev1991_ctrl.lavray.ui.LocalEnv
import io.github.anatolijlaptev1991_ctrl.lavray.ui.core.ErrorKind
import io.github.anatolijlaptev1991_ctrl.lavray.ui.core.textRes
import io.github.anatolijlaptev1991_ctrl.lavray.ui.design.Lav
import io.github.anatolijlaptev1991_ctrl.lavray.ui.design.LavIcon
import io.github.anatolijlaptev1991_ctrl.lavray.ui.design.LavIconView
import io.github.anatolijlaptev1991_ctrl.lavray.ui.design.LavText
import io.github.anatolijlaptev1991_ctrl.lavray.ui.design.components.ButtonVariant
import io.github.anatolijlaptev1991_ctrl.lavray.ui.design.components.LavBanner
import io.github.anatolijlaptev1991_ctrl.lavray.ui.design.components.LavButton
import io.github.anatolijlaptev1991_ctrl.lavray.ui.design.components.LavCard
import io.github.anatolijlaptev1991_ctrl.lavray.ui.design.components.LavScreenColumn
import io.github.anatolijlaptev1991_ctrl.lavray.ui.design.components.LavTextField
import io.github.anatolijlaptev1991_ctrl.lavray.ui.design.components.StatusTone
import io.github.anatolijlaptev1991_ctrl.lavray.ui.nav.LocalNavigator
import io.github.anatolijlaptev1991_ctrl.lavray.ui.nav.Navigator
import io.github.anatolijlaptev1991_ctrl.lavray.ui.screens.connection.isLive
import io.github.anatolijlaptev1991_ctrl.lavray.ui.screens.connection.phaseOf

/**
 * Checks (desktop screen_diag.tsx): the self-check list, public address direct and through the tunnel, the leak
 * check and the route check. Everything runs on request or once on entry, never on a timer.
 */
@Composable
fun ChecksScreen() {
    val env = LocalEnv.current
    val model = remember { ChecksModel(env.session, env.scope) }
    val state by model.state.collectAsStateWithLifecycle()
    val vpn by env.vpn.status.collectAsStateWithLifecycle()
    val live = phaseOf(vpn.state).isLive
    LaunchedEffect(model) { model.runChecks() }
    DisposableEffect(model) { onDispose { model.cancelRunning() } }

    LavScreenColumn {
        state.error?.let { ErrorBanner(it) }
        SelfCheckCard(state, onRun = model::runChecks)
        AddressCard(state, live, onProbe = { model.probeAddresses(live) })
        LeakCard(state, live, onCheck = model::checkLeaks)
        RouteCard(state, onCheck = model::checkRoute)
    }
}

@Composable
private fun ErrorBanner(kind: ErrorKind) {
    LavBanner(StatusTone.Error, stringResource(kind.textRes()))
}

@Composable
private fun CardHeader(icon: LavIcon, title: String, action: (@Composable () -> Unit)? = null) {
    // A flow: with a large font the button drops under the title instead of squeezing it into one word per line.
    FlowRow(
        Modifier.fillMaxWidth(),
        horizontalArrangement = Arrangement.SpaceBetween,
        verticalArrangement = Arrangement.spacedBy(8.dp),
        itemVerticalAlignment = Alignment.CenterVertically,
    ) {
        Row(verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(10.dp)) {
            LavIconView(icon, Lav.colors.textSecondary, size = 20.dp)
            LavText(title, style = Lav.type.h2)
        }
        action?.invoke()
    }
}

@Composable
private fun SelfCheckCard(state: ChecksState, onRun: () -> Unit) {
    val nav = LocalNavigator.current
    val context = LocalContext.current
    LavCard(Modifier.fillMaxWidth()) {
        Column(Modifier.padding(16.dp), verticalArrangement = Arrangement.spacedBy(12.dp)) {
            CardHeader(LavIcon.Activity, stringResource(R.string.chk_title)) {
                LavButton(stringResource(R.string.chk_run), onRun, variant = ButtonVariant.Primary, icon = LavIcon.Refresh, loading = state.running)
            }
            state.summary?.let { LavText(summaryText(it), style = Lav.type.small, color = Lav.colors.textSecondary) }
            state.rows.forEach { row -> CheckRowView(row) { action -> runFix(action, nav, context) } }
        }
    }
}

@Composable
private fun summaryText(s: CheckSummary): String = when (s) {
    CheckSummary.AllGood -> stringResource(R.string.chk_sum_ok)
    is CheckSummary.Problems -> stringResource(R.string.chk_sum_bad, s.count.toString())
    is CheckSummary.Partial -> stringResource(R.string.chk_sum_part, s.skipped.toString(), s.total.toString())
}

@Composable
private fun CheckRowView(row: CheckRow, onFix: (FixAction) -> Unit) {
    val (tone, icon, label) = rowVisual(row.state)
    val c = Lav.colors
    Row(Modifier.fillMaxWidth(), verticalAlignment = Alignment.Top, horizontalArrangement = Arrangement.spacedBy(12.dp)) {
        LavIconView(icon, tone.text(c), size = 20.dp, modifier = Modifier.padding(top = 2.dp))
        Column(Modifier.weight(1f).semantics(mergeDescendants = true) {}) {
            LavText(stringResource(ChecksMapper.title(row.kind)), style = Lav.type.body)
            LavText(stringResource(label), style = Lav.type.caption, color = tone.text(c))
            when (val d = row.detail) {
                DetailText.None -> Unit
                is DetailText.Res -> LavText(stringResource(d.id), style = Lav.type.small, color = c.textSecondary)
                is DetailText.Raw -> LavText(d.text, style = Lav.type.mono, color = c.textSecondary)
            }
        }
        row.fix?.let { LavButton(stringResource(ChecksMapper.fixLabel(it)), { onFix(it) }) }
    }
}

private data class RowVisual(val tone: StatusTone, val icon: LavIcon, val label: Int)

private fun rowVisual(state: RowState): RowVisual = when (state) {
    RowState.Pending -> RowVisual(StatusTone.Neutral, LavIcon.Gauge, R.string.chk_st_run)
    RowState.Ok -> RowVisual(StatusTone.Connected, LavIcon.Check, R.string.st_ok)
    RowState.Warn -> RowVisual(StatusTone.Connecting, LavIcon.Alert, R.string.st_warn)
    RowState.Fail -> RowVisual(StatusTone.Error, LavIcon.Close, R.string.st_fail)
    RowState.Skipped -> RowVisual(StatusTone.Neutral, LavIcon.Info, R.string.chk_st_skip)
}

private fun runFix(action: FixAction, nav: Navigator, context: Context) {
    when (action) {
        FixAction.OpenDns -> nav.openSection("rules", "dns")
        FixAction.OpenSubscriptions -> nav.openSection("servers", "subscriptions")
        FixAction.GoToConnection -> nav.openSection("connection")
        FixAction.SyncClock -> try {
            context.startActivity(Intent(Settings.ACTION_DATE_SETTINGS).addFlags(Intent.FLAG_ACTIVITY_NEW_TASK))
        } catch (_: ActivityNotFoundException) {
            // A device without that page keeps the row as it is: the clock is set in the system settings by hand.
        }
    }
}

@Composable
private fun AddressCard(state: ChecksState, live: Boolean, onProbe: () -> Unit) {
    val busy = state.direct == IpReading.Loading || state.tunnel == IpReading.Loading
    LavCard(Modifier.fillMaxWidth()) {
        Column(Modifier.padding(16.dp), verticalArrangement = Arrangement.spacedBy(12.dp)) {
            CardHeader(LavIcon.Globe, stringResource(R.string.chk_ip_title)) {
                LavButton(stringResource(R.string.chk_ip_run), onProbe, loading = busy)
            }
            IpLine(stringResource(R.string.leak_direct), state.direct, offText = null)
            IpLine(stringResource(R.string.conn_ip), state.tunnel, offText = if (live) null else stringResource(R.string.chk_ip_connect))
        }
    }
}

@Composable
private fun IpLine(label: String, reading: IpReading, offText: String?) {
    val c = Lav.colors
    Column(Modifier.fillMaxWidth().semantics(mergeDescendants = true) {}, verticalArrangement = Arrangement.spacedBy(2.dp)) {
        LavText(label, style = Lav.type.caption, color = c.textTertiary)
        when {
            reading is IpReading.Value -> LavText(reading.ip, style = Lav.type.mono)
            reading is IpReading.Loading -> LavText(stringResource(R.string.chk_st_run), style = Lav.type.small, color = c.textSecondary)
            reading is IpReading.Failed -> LavText(stringResource(reading.kind.textRes()), style = Lav.type.small, color = c.errorText)
            offText != null -> LavText(offText, style = Lav.type.small, color = c.textSecondary)
            else -> LavText("—", style = Lav.type.mono, color = c.textTertiary)
        }
    }
}

@Composable
private fun LeakCard(state: ChecksState, live: Boolean, onCheck: () -> Unit) {
    val c = Lav.colors
    LavCard(Modifier.fillMaxWidth()) {
        Column(Modifier.padding(16.dp), verticalArrangement = Arrangement.spacedBy(12.dp)) {
            CardHeader(LavIcon.Shield, stringResource(R.string.leak_title)) {
                LavButton(stringResource(R.string.leak_run), onCheck, loading = state.leakRunning, enabled = live)
            }
            if (!live) LavText(stringResource(R.string.leak_off), style = Lav.type.small, color = c.textSecondary)
            state.leakError?.let { ErrorBanner(it) }
            state.leak?.takeIf { live || it.tunnelActive }?.let { LeakResult(it) }
        }
    }
}

@Composable
private fun LeakResult(v: LeakVerdict) {
    val c = Lav.colors
    val unchecked = stringResource(R.string.chk_st_skip)
    IpLine(stringResource(R.string.leak_direct), v.directIp?.let { IpReading.Value(it, false) } ?: IpReading.Idle, offText = if (v.directUnchecked) unchecked else null)
    IpLine(stringResource(R.string.conn_ip), v.tunnelIp?.let { IpReading.Value(it, false) } ?: IpReading.Idle, offText = if (v.tunnelUnchecked) unchecked else null)
    when {
        v.sameAddress -> LavBanner(StatusTone.Error, stringResource(R.string.chk_leak_same))
        v.directIp != null && v.tunnelIp != null -> LavBanner(StatusTone.Connected, stringResource(R.string.leak_noLeak))
    }
    if (v.ipv6Direct) LavBanner(StatusTone.Connecting, stringResource(R.string.chk_d_v6_direct))
    Column(verticalArrangement = Arrangement.spacedBy(4.dp)) {
        if (v.ipv6Unchecked) NotMeasured(stringResource(R.string.leak_ipv6), stringResource(R.string.chk_d_v6_unmeasured))
        if (v.dnsUnchecked) NotMeasured(stringResource(R.string.leak_dns), stringResource(R.string.chk_leak_dns_note))
    }
    LavText(stringResource(R.string.chk_leak_scope), style = Lav.type.caption, color = c.textTertiary)
}

@Composable
private fun NotMeasured(title: String, text: String) {
    Column(Modifier.semantics(mergeDescendants = true) {}) {
        LavText("$title: ${stringResource(R.string.chk_st_skip)}", style = Lav.type.small)
        LavText(text, style = Lav.type.caption, color = Lav.colors.textTertiary)
    }
}

@Composable
private fun RouteCard(state: ChecksState, onCheck: (String) -> Unit) {
    var query by remember { mutableStateOf("") }
    val c = Lav.colors
    LavCard(Modifier.fillMaxWidth()) {
        Column(Modifier.padding(16.dp), verticalArrangement = Arrangement.spacedBy(12.dp)) {
            CardHeader(LavIcon.Route, stringResource(R.string.rt_test_t))
            LavTextField(
                value = query, onValueChange = { query = it }, label = stringResource(R.string.chk_route_label),
                placeholder = stringResource(R.string.chk_route_ph), mono = true,
                keyboardOptions = KeyboardOptions(keyboardType = KeyboardType.Uri, imeAction = ImeAction.Search),
                keyboardActions = KeyboardActions(onSearch = { onCheck(query) }),
            )
            LavButton(stringResource(R.string.rt_test_run), { onCheck(query) }, variant = ButtonVariant.Primary, loading = state.routing, enabled = query.isNotBlank())
            state.routeError?.let { ErrorBanner(it) }
            state.route?.let { RouteResult(it) }
            LavText(stringResource(R.string.chk_route_note), style = Lav.type.caption, color = c.textTertiary)
        }
    }
}

@Composable
private fun RouteResult(answer: RouteAnswer) {
    val out = answer.outbound?.let { stringResource(it) } ?: "—"
    val text = if (answer.rule.isNullOrBlank()) {
        stringResource(R.string.rt_test_resultDef, out, answer.address)
    } else {
        stringResource(R.string.chk_route_result, answer.address, out, answer.rule)
    }
    LavBanner(StatusTone.Direct, text)
}
