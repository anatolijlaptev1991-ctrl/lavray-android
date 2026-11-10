// SPDX-License-Identifier: GPL-3.0-only
package io.github.anatolijlaptev1991_ctrl.lavray.ui.screens.connection

import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.FlowRow
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableLongStateOf
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.semantics.Role
import androidx.compose.ui.semantics.semantics
import androidx.compose.ui.text.style.TextAlign
import androidx.compose.ui.unit.dp
import androidx.lifecycle.Lifecycle
import androidx.lifecycle.compose.LocalLifecycleOwner
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import androidx.lifecycle.repeatOnLifecycle
import io.github.anatolijlaptev1991_ctrl.lavray.R
import io.github.anatolijlaptev1991_ctrl.lavray.api.gen.TestLatencyRequest
import io.github.anatolijlaptev1991_ctrl.lavray.ui.LocalEnv
import io.github.anatolijlaptev1991_ctrl.lavray.ui.design.Lav
import io.github.anatolijlaptev1991_ctrl.lavray.ui.design.LavIcon
import io.github.anatolijlaptev1991_ctrl.lavray.ui.design.LavIconView
import io.github.anatolijlaptev1991_ctrl.lavray.ui.design.LavText
import io.github.anatolijlaptev1991_ctrl.lavray.ui.design.components.LavBadge
import io.github.anatolijlaptev1991_ctrl.lavray.ui.design.components.LavCard
import io.github.anatolijlaptev1991_ctrl.lavray.ui.design.components.lavClickable
import io.github.anatolijlaptev1991_ctrl.lavray.ui.i18n.UnitFormat
import io.github.anatolijlaptev1991_ctrl.lavray.ui.i18n.rememberUnitFormat
import io.github.anatolijlaptev1991_ctrl.lavray.ui.importer.ImportTab
import io.github.anatolijlaptev1991_ctrl.lavray.ui.screens.rules.PendingReason
import io.github.anatolijlaptev1991_ctrl.lavray.ui.screens.servers.label
import io.github.anatolijlaptev1991_ctrl.lavray.ui.screens.subscriptions.EventToasts
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.delay
import kotlinx.coroutines.launch

// The cards under the state card of the Connection screen: server, route preset, traffic, exit and leaks, quick actions.

@Composable
internal fun Details(
    phase: Phase,
    info: ConnectionInfo,
    traffic: TrafficReading,
    probe: ProbeUi,
    onChange: () -> Unit,
    onStats: () -> Unit,
) {
    val fmt = rememberUnitFormat()
    // What is saved but not in the running tunnel yet: the preset and DNS shown below are the saved ones.
    val pending by LocalEnv.current.restart.state.collectAsStateWithLifecycle()
    val running = phase.isLive
    if (info.server != null) ServerCard(info.server, onChange)
    if (running) ExitCard(probe.state, probe.key, info.server, fmt, probe.onRefresh, dnsPending = PendingReason.Dns in pending.reasons)
    TrafficCard(phase, info, traffic, fmt, onStats)
    if (running) LeakCard(probe.state, probe.key, probe.onRefresh)
    if (info.preset != null) PresetRow(info.preset, pending = running && PendingReason.Routing in pending.reasons)
}

/** The probe as the cards need it: its state, the connection it belongs to and the manual refresh. */
internal class ProbeUi(val state: ProbeState, val key: String?, val onRefresh: () -> Unit)

internal class QuickState(
    val testing: Boolean, val progress: String?, val updatingSubs: Boolean, val info: ConnectionInfo,
    val onTest: () -> Unit, val onAdd: () -> Unit, val onSubs: () -> Unit,
)

@Composable
internal fun rememberQuickActions(info: ConnectionInfo, scope: kotlinx.coroutines.CoroutineScope): QuickState {
    val env = LocalEnv.current
    val servers by env.servers.state.collectAsStateWithLifecycle()
    var subsAsked by remember { mutableStateOf(false) }
    // The subscriptions model is built (and its events heard) only after the person asks for an update.
    var updating = false
    if (subsAsked) {
        EventToasts(env.subscriptions)
        updating = env.subscriptions.state.collectAsStateWithLifecycle().value.isBusy
    }
    val test = servers.test
    return QuickState(
        testing = test.running,
        progress = if (test.running) stringResource(R.string.test_progress, test.done.toString(), test.total.toString()) else null,
        updatingSubs = updating, info = info,
        onTest = { scope.launch { env.servers.startTest(env.connection.profileIds(), TestLatencyRequest.Mode.MODE_TCP_PING) } },
        onAdd = { env.importer.open(tab = ImportTab.Link) },
        onSubs = { subsAsked = true; env.subscriptions.updateAll() },
    )
}

@Composable
internal fun QuickActionsRow(q: QuickState) {
    QuickActions(
        testing = q.testing, testProgress = q.progress, updatingSubs = q.updatingSubs,
        canTest = q.info.hasServers, canUpdateSubs = q.info.subscriptions > 0,
        onTest = q.onTest, onAdd = q.onAdd, onSubs = q.onSubs,
    )
}

@Composable
internal fun ServerCard(server: ServerSummary, onChange: () -> Unit) {
    val c = Lav.colors
    val shape = RoundedCornerShape(14.dp)
    val change = stringResource(R.string.srv_change)
    LavCard(Modifier.fillMaxWidth()) {
        Row(
            Modifier.fillMaxWidth().lavClickable(onChange, shape, role = Role.Button, onClickLabel = change).padding(16.dp),
            verticalAlignment = Alignment.CenterVertically,
            horizontalArrangement = Arrangement.spacedBy(12.dp),
        ) {
            Column(Modifier.weight(1f), verticalArrangement = Arrangement.spacedBy(4.dp)) {
                LavText(stringResource(R.string.srv_current), style = Lav.type.caption, color = c.textTertiary)
                // A flow: with a large font the badge drops under the name instead of cutting the name to one letter per line.
                FlowRow(horizontalArrangement = Arrangement.spacedBy(8.dp), verticalArrangement = Arrangement.spacedBy(4.dp), itemVerticalAlignment = Alignment.CenterVertically) {
                    LavText(server.name, style = Lav.type.h2, maxLines = 2, overflow = androidx.compose.ui.text.style.TextOverflow.Ellipsis)
                    LavBadge(server.type.label())
                }
                LavText("${server.address}:${server.port}", style = Lav.type.mono, color = c.textTertiary, maxLines = 1, overflow = androidx.compose.ui.text.style.TextOverflow.Ellipsis)
            }
            LavText(change, style = Lav.type.small, color = c.accent)
            LavIconView(LavIcon.ChevronRight, c.accent, size = 18.dp)
        }
    }
}

@Composable
internal fun PresetRow(preset: PresetSummary, pending: Boolean) {
    val c = Lav.colors
    LavCard(Modifier.fillMaxWidth()) {
        Row(Modifier.fillMaxWidth().padding(16.dp), horizontalArrangement = Arrangement.SpaceBetween, verticalAlignment = Alignment.CenterVertically) {
            LavText(stringResource(R.string.health_preset), style = Lav.type.small, color = c.textTertiary)
            Column(Modifier.weight(1f).padding(start = 12.dp), horizontalAlignment = Alignment.End) {
                LavText(presetTitle(preset), style = Lav.type.bodyStrong, textAlign = TextAlign.End)
                // The tunnel still routes by the previous preset until the restart.
                if (pending) LavText(stringResource(R.string.cx_pending_apply), style = Lav.type.caption, color = c.connectingText, textAlign = TextAlign.End)
            }
        }
    }
}

@Composable
internal fun TrafficCard(phase: Phase, info: ConnectionInfo, traffic: TrafficReading, fmt: UnitFormat, onStats: () -> Unit) {
    val c = Lav.colors
    val shape = RoundedCornerShape(14.dp)
    val elapsed = rememberSessionSeconds(phase.isLive, info.startedAtMs)
    LavCard(Modifier.fillMaxWidth()) {
        Column(
            Modifier.fillMaxWidth().lavClickable(onStats, shape, role = Role.Button).padding(16.dp),
            verticalArrangement = Arrangement.spacedBy(12.dp),
        ) {
            Row(Modifier.fillMaxWidth(), horizontalArrangement = Arrangement.spacedBy(12.dp)) {
                SpeedCell(LavIcon.ArrowDown, stringResource(R.string.conn_down), fmt.speed(traffic.downBps), Modifier.weight(1f))
                SpeedCell(LavIcon.ArrowUp, stringResource(R.string.conn_up), fmt.speed(traffic.upBps), Modifier.weight(1f))
            }
            Row(Modifier.fillMaxWidth(), horizontalArrangement = Arrangement.spacedBy(12.dp)) {
                Cell(stringResource(R.string.conn_session), fmt.clock(elapsed), Modifier.weight(1f))
                Cell(stringResource(R.string.conn_traffic), fmt.bytes(traffic.downTotal + traffic.upTotal), Modifier.weight(1f))
            }
        }
    }
}

@Composable
internal fun SpeedCell(icon: LavIcon, label: String, value: String, modifier: Modifier) {
    val c = Lav.colors
    Column(modifier.semantics(mergeDescendants = true) {}, verticalArrangement = Arrangement.spacedBy(2.dp)) {
        Row(horizontalArrangement = Arrangement.spacedBy(4.dp), verticalAlignment = Alignment.CenterVertically) {
            LavIconView(icon, c.textTertiary, size = 14.dp)
            LavText(label, style = Lav.type.caption, color = c.textTertiary)
        }
        LavText(value, style = Lav.type.mono.copy(fontSize = androidx.compose.ui.unit.TextUnit(16f, androidx.compose.ui.unit.TextUnitType.Sp)), maxLines = 1)
    }
}

@Composable
internal fun Cell(label: String, value: String, modifier: Modifier) {
    val c = Lav.colors
    Column(modifier.semantics(mergeDescendants = true) {}, verticalArrangement = Arrangement.spacedBy(2.dp)) {
        LavText(label, style = Lav.type.caption, color = c.textTertiary)
        LavText(value, style = Lav.type.mono, maxLines = 1)
    }
}

/** Seconds since the tunnel came up; ticks once a second, only while the screen is started and the tunnel is on. */
@Composable
internal fun rememberSessionSeconds(live: Boolean, startedAtMs: Long): Long {
    var seconds by remember { mutableLongStateOf(0L) }
    val owner = LocalLifecycleOwner.current
    val fallbackStart = remember(live) { System.currentTimeMillis() }
    LaunchedEffect(live, startedAtMs) {
        if (!live) {
            seconds = 0L
            return@LaunchedEffect
        }
        val start = if (startedAtMs > 0) startedAtMs else fallbackStart
        owner.lifecycle.repeatOnLifecycle(Lifecycle.State.STARTED) {
            while (true) {
                seconds = ((System.currentTimeMillis() - start) / 1000).coerceAtLeast(0)
                delay(1000)
            }
        }
    }
    return seconds
}

