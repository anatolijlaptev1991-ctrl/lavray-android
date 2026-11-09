// SPDX-License-Identifier: GPL-3.0-only
package io.github.anatolijlaptev1991_ctrl.lavray.ui.screens.diag

import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.remember
import androidx.compose.ui.Modifier
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.semantics.semantics
import androidx.compose.ui.unit.dp
import androidx.lifecycle.Lifecycle
import androidx.lifecycle.compose.LocalLifecycleOwner
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import androidx.lifecycle.repeatOnLifecycle
import io.github.anatolijlaptev1991_ctrl.lavray.R
import io.github.anatolijlaptev1991_ctrl.lavray.ui.LocalEnv
import io.github.anatolijlaptev1991_ctrl.lavray.ui.core.textRes
import io.github.anatolijlaptev1991_ctrl.lavray.ui.design.Lav
import io.github.anatolijlaptev1991_ctrl.lavray.ui.design.LavIcon
import io.github.anatolijlaptev1991_ctrl.lavray.ui.design.LavText
import io.github.anatolijlaptev1991_ctrl.lavray.ui.design.components.LavBanner
import io.github.anatolijlaptev1991_ctrl.lavray.ui.design.components.LavCard
import io.github.anatolijlaptev1991_ctrl.lavray.ui.design.components.LavScreenColumn
import io.github.anatolijlaptev1991_ctrl.lavray.ui.design.components.LavSectionLabel
import io.github.anatolijlaptev1991_ctrl.lavray.ui.design.components.LavSkeletonRows
import io.github.anatolijlaptev1991_ctrl.lavray.ui.design.components.StatusTone
import io.github.anatolijlaptev1991_ctrl.lavray.ui.i18n.UnitFormat
import io.github.anatolijlaptev1991_ctrl.lavray.ui.i18n.rememberUnitFormat
import io.github.anatolijlaptev1991_ctrl.lavray.ui.screens.connection.isLive
import io.github.anatolijlaptev1991_ctrl.lavray.ui.screens.connection.phaseOf
import java.text.DateFormat
import java.util.Date

/**
 * Statistics: the running session (speed and totals from the live stream), the total over all servers and the
 * per-group summary the core keeps. The stream is subscribed only while the screen is visible and a tunnel is up.
 */
@Composable
fun StatisticsScreen() {
    val env = LocalEnv.current
    val model = remember { StatisticsModel(env.session) }
    val state by model.state.collectAsStateWithLifecycle()
    val vpn by env.vpn.status.collectAsStateWithLifecycle()
    val live = phaseOf(vpn.state).isLive
    val owner = LocalLifecycleOwner.current
    val fmt = rememberUnitFormat()

    LaunchedEffect(model) { model.load() }
    LaunchedEffect(live) {
        if (!live) {
            model.endSession()
            return@LaunchedEffect
        }
        owner.lifecycle.repeatOnLifecycle(Lifecycle.State.STARTED) { model.watch() }
    }

    LavScreenColumn {
        state.error?.let { LavBanner(StatusTone.Error, stringResource(it.textRes())) }
        if (!state.loaded) LavSkeletonRows(3, stringResource(R.string.lazy_loading))
        SessionCard(state, live, fmt)
        if (state.tags.isNotEmpty()) TagsCard(state.tags, fmt)
        if (state.loaded) {
            TotalCard(state.total, fmt)
            if (state.groups.isNotEmpty()) GroupsCard(state.groups, fmt)
        }
    }
}

@Composable
private fun SessionCard(state: StatsState, live: Boolean, fmt: UnitFormat) {
    LavSectionLabel(stringResource(R.string.conn_session))
    LavCard(Modifier.fillMaxWidth()) {
        Column(Modifier.padding(16.dp), verticalArrangement = Arrangement.spacedBy(12.dp)) {
            if (!live) {
                LavText(stringResource(R.string.stats_no_session), style = Lav.type.small, color = Lav.colors.textSecondary)
            } else {
                val s = state.session
                TwoCells(LavIcon.ArrowDown, stringResource(R.string.conn_down), fmt.speed(s.downBps), LavIcon.ArrowUp, stringResource(R.string.conn_up), fmt.speed(s.upBps))
                TwoCells(null, stringResource(R.string.stats_downloaded), fmt.bytes(s.downTotal), null, stringResource(R.string.stats_uploaded), fmt.bytes(s.upTotal))
            }
        }
    }
}

@Composable
private fun TagsCard(tags: List<TagLine>, fmt: UnitFormat) {
    LavSectionLabel(stringResource(R.string.stats_by_route))
    LavCard(Modifier.fillMaxWidth()) {
        Column(Modifier.padding(16.dp), verticalArrangement = Arrangement.spacedBy(12.dp)) {
            tags.forEach { t ->
                Column(Modifier.semantics(mergeDescendants = true) {}, verticalArrangement = Arrangement.spacedBy(2.dp)) {
                    LavText(tagName(t.tag), style = Lav.type.bodyStrong)
                    LavText(
                        "↓ ${fmt.speed(t.downBps)} · ↑ ${fmt.speed(t.upBps)}  |  ↓ ${fmt.bytes(t.traffic.downBytes)} · ↑ ${fmt.bytes(t.traffic.upBytes)}",
                        style = Lav.type.mono, color = Lav.colors.textSecondary,
                    )
                }
            }
        }
    }
}

@Composable
private fun tagName(tag: String): String = when (tag) {
    "proxy" -> stringResource(R.string.out_proxy)
    "direct" -> stringResource(R.string.out_direct)
    "block" -> stringResource(R.string.out_block)
    else -> tag
}

@Composable
private fun TotalCard(total: TrafficPair, fmt: UnitFormat) {
    LavSectionLabel(stringResource(R.string.stats_total))
    LavCard(Modifier.fillMaxWidth()) {
        Column(Modifier.padding(16.dp), verticalArrangement = Arrangement.spacedBy(12.dp)) {
            if (total.total == 0L) {
                LavText(stringResource(R.string.stats_none), style = Lav.type.small, color = Lav.colors.textSecondary)
            } else {
                Cell(stringResource(R.string.conn_traffic), fmt.bytes(total.total), Modifier.fillMaxWidth())
                TwoCells(LavIcon.ArrowDown, stringResource(R.string.conn_down), fmt.bytes(total.downBytes), LavIcon.ArrowUp, stringResource(R.string.conn_up), fmt.bytes(total.upBytes))
            }
        }
    }
}

@Composable
private fun GroupsCard(groups: List<GroupLine>, fmt: UnitFormat) {
    LavSectionLabel(stringResource(R.string.stats_groups))
    val date = remember { DateFormat.getDateTimeInstance(DateFormat.MEDIUM, DateFormat.SHORT) }
    LavCard(Modifier.fillMaxWidth()) {
        Column(Modifier.padding(16.dp), verticalArrangement = Arrangement.spacedBy(14.dp)) {
            groups.forEach { g ->
                Column(Modifier.semantics(mergeDescendants = true) {}, verticalArrangement = Arrangement.spacedBy(2.dp)) {
                    LavText(g.name ?: stringResource(R.string.stats_group_gone), style = Lav.type.bodyStrong)
                    LavText(
                        "↓ ${fmt.bytes(g.traffic.downBytes)} · ↑ ${fmt.bytes(g.traffic.upBytes)}",
                        style = Lav.type.mono, color = Lav.colors.textSecondary,
                    )
                    val last = g.lastSessionMs?.let { stringResource(R.string.stats_last, date.format(Date(it))) }
                    LavText(
                        stringResource(R.string.stats_sessions, g.sessions.toString()) + (last?.let { "  ·  $it" } ?: ""),
                        style = Lav.type.caption, color = Lav.colors.textTertiary,
                    )
                }
            }
        }
    }
}

@Composable
private fun TwoCells(iconA: LavIcon?, labelA: String, valueA: String, iconB: LavIcon?, labelB: String, valueB: String) {
    Row(Modifier.fillMaxWidth(), horizontalArrangement = Arrangement.spacedBy(16.dp)) {
        Cell(labelA, valueA, Modifier.weight(1f), iconA)
        Cell(labelB, valueB, Modifier.weight(1f), iconB)
    }
}

@Composable
private fun Cell(label: String, value: String, modifier: Modifier, icon: LavIcon? = null) {
    Column(modifier.semantics(mergeDescendants = true) {}, verticalArrangement = Arrangement.spacedBy(2.dp)) {
        Row(horizontalArrangement = Arrangement.spacedBy(4.dp)) {
            if (icon != null) io.github.anatolijlaptev1991_ctrl.lavray.ui.design.LavIconView(icon, Lav.colors.textTertiary, size = 14.dp)
            LavText(label, style = Lav.type.caption, color = Lav.colors.textTertiary)
        }
        LavText(value, style = Lav.type.mono, maxLines = 1)
    }
}
