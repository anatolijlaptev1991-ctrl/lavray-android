// SPDX-License-Identifier: GPL-3.0-only
package io.github.anatolijlaptev1991_ctrl.lavray.ui.screens.diag

import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.FlowRow
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.foundation.background
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.runtime.derivedStateOf
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.semantics.semantics
import androidx.compose.ui.text.input.ImeAction
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
import io.github.anatolijlaptev1991_ctrl.lavray.ui.design.components.ButtonVariant
import io.github.anatolijlaptev1991_ctrl.lavray.ui.design.components.LavBanner
import io.github.anatolijlaptev1991_ctrl.lavray.ui.design.components.LavButton
import io.github.anatolijlaptev1991_ctrl.lavray.ui.design.components.LavChip
import io.github.anatolijlaptev1991_ctrl.lavray.ui.design.components.LavEmptyState
import io.github.anatolijlaptev1991_ctrl.lavray.ui.design.components.LavListRow
import io.github.anatolijlaptev1991_ctrl.lavray.ui.design.components.LavTextField
import io.github.anatolijlaptev1991_ctrl.lavray.ui.design.components.StatusTone
import io.github.anatolijlaptev1991_ctrl.lavray.ui.i18n.UnitFormat
import io.github.anatolijlaptev1991_ctrl.lavray.ui.i18n.rememberUnitFormat
import io.github.anatolijlaptev1991_ctrl.lavray.ui.screens.connection.isLive
import io.github.anatolijlaptev1991_ctrl.lavray.ui.screens.connection.phaseOf

/**
 * Connections (desktop conns/ConnsScreen.tsx): active connections and the closed history, chips, search, pause and a
 * per-connection sheet. The stream is opened only while this screen is composed, the app is started and the tunnel
 * is up; the list is a LazyColumn with stable keys, so hundreds of rows cost what the visible ones cost.
 */
@Composable
fun ConnectionsScreen() {
    val env = LocalEnv.current
    val model = remember { ConnectionsModel(env.session) }
    val state by model.state.collectAsStateWithLifecycle()
    val error by model.streamError.collectAsStateWithLifecycle()
    val vpn by env.vpn.status.collectAsStateWithLifecycle()
    val live = phaseOf(vpn.state).isLive
    val owner = LocalLifecycleOwner.current
    var chip by remember { mutableStateOf(ConnChip.All) }
    var query by remember { mutableStateOf("") }
    var opened by remember { mutableStateOf<ConnRow?>(null) }

    LaunchedEffect(live) {
        if (!live) {
            model.reset()
            return@LaunchedEffect
        }
        owner.lifecycle.repeatOnLifecycle(Lifecycle.State.STARTED) { model.watch() }
    }

    val view = state.shown
    val active by remember(view, chip, query) { derivedStateOf { ConnFilter.apply(view.active, chip, query) } }
    val closed by remember(view, chip, query) { derivedStateOf { ConnFilter.apply(view.closed, chip, query) } }
    val fmt = rememberUnitFormat()

    Column(Modifier.fillMaxSize()) {
        Controls(chip, { chip = it }, query, { query = it }, state.paused, model::setPaused)
        error?.let { LavBanner(StatusTone.Error, stringResource(it.textRes()), Modifier.padding(horizontal = 16.dp), text = stringResource(R.string.conns_retrying)) }
        if (live) Counts(active.size, closed.size, view.truncated)
        val empty = active.isEmpty() && closed.isEmpty()
        if (empty) {
            val hasAny = view.active.isNotEmpty() || view.closed.isNotEmpty()
            val text = when {
                hasAny -> R.string.conns_noMatch
                live -> R.string.conns_emptyOn
                else -> R.string.conns_empty
            }
            LavEmptyState(stringResource(R.string.conns_caption), text = stringResource(text), icon = LavIcon.Activity)
        } else {
            ConnectionList(active, closed, view.nowMs, fmt) { opened = it }
        }
    }
    opened?.let { row ->
        // The sheet shows the latest copy of the row, so bytes keep moving while it is open.
        val current = (view.active + view.closed).firstOrNull { it.id == row.id } ?: row
        ConnectionSheet(current, view.nowMs, model, onDismiss = { opened = null })
    }
}

@Composable
private fun Controls(chip: ConnChip, onChip: (ConnChip) -> Unit, query: String, onQuery: (String) -> Unit, paused: Boolean, onPause: (Boolean) -> Unit) {
    Column(Modifier.padding(horizontal = 16.dp, vertical = 8.dp), verticalArrangement = Arrangement.spacedBy(8.dp)) {
        LavTextField(
            query, onQuery, Modifier.fillMaxWidth(), placeholder = stringResource(R.string.conns_search), leading = LavIcon.Search,
            keyboardOptions = androidx.compose.foundation.text.KeyboardOptions(imeAction = ImeAction.Search),
        )
        FlowRow(horizontalArrangement = Arrangement.spacedBy(8.dp), verticalArrangement = Arrangement.spacedBy(0.dp)) {
            ConnChip.entries.forEach { c -> LavChip(chipLabel(c), chip == c, { onChip(c) }) }
            LavChip(
                stringResource(if (paused) R.string.log_resume else R.string.log_pause), paused, { onPause(!paused) },
                icon = if (paused) LavIcon.Refresh else LavIcon.Stop,
            )
        }
        if (paused) LavText(stringResource(R.string.conns_paused), style = Lav.type.caption, color = Lav.colors.textTertiary)
    }
}

@Composable
private fun chipLabel(c: ConnChip): String = stringResource(
    when (c) {
        ConnChip.All -> R.string.conns_chip_all
        ConnChip.Proxy -> R.string.conns_chip_proxy
        ConnChip.Direct -> R.string.conns_chip_direct
        ConnChip.Block -> R.string.conns_chip_block
    },
)

@Composable
private fun Counts(active: Int, closed: Int, truncated: Boolean) {
    Column(Modifier.padding(horizontal = 20.dp, vertical = 4.dp)) {
        LavText(
            stringResource(R.string.conns_count, active.toString()) + "  ·  " + stringResource(R.string.conns_countClosed, closed.toString()),
            style = Lav.type.caption, color = Lav.colors.textTertiary,
        )
        if (truncated) {
            LavText(stringResource(R.string.conns_truncated, ConnectionsModel.ACTIVE_LIMIT.toString()), style = Lav.type.caption, color = Lav.colors.textTertiary)
        }
    }
}

@Composable
private fun ConnectionList(active: List<ConnRow>, closed: List<ConnRow>, nowMs: Long, fmt: UnitFormat, onOpen: (ConnRow) -> Unit) {
    LazyColumn(Modifier.fillMaxSize().padding(horizontal = 8.dp)) {
        items(active, key = { "a" + it.id }, contentType = { "row" }) { ConnListRow(it, nowMs, fmt, onOpen) }
        items(closed, key = { "c" + it.id }, contentType = { "row" }) { ConnListRow(it, nowMs, fmt, onOpen) }
    }
}

@Composable
private fun ConnListRow(row: ConnRow, nowMs: Long, fmt: UnitFormat, onOpen: (ConnRow) -> Unit) {
    val tone = toneOf(row)
    val net = row.network.uppercase()
    val sub = buildString {
        append(row.finalOutbound)
        if (net.isNotEmpty()) append(" · ").append(net)
        append(" · ↓ ").append(fmt.bytes(row.downloadBytes)).append(" ↑ ").append(fmt.bytes(row.uploadBytes))
        append(" · ").append(fmt.clock(row.durationSeconds(nowMs)))
    }
    LavListRow(
        title = row.destination, subtitle = sub, onClick = { onOpen(row) },
        modifier = Modifier.fillMaxWidth(),
        leading = { Dot(tone, row.status != ConnStatus.Active) },
        trailing = { LavText(statusLabel(row.status), style = Lav.type.caption, color = tone.text(Lav.colors)) },
    )
}

@Composable
private fun statusLabel(s: ConnStatus): String = stringResource(
    when (s) {
        ConnStatus.Active -> R.string.conns_st_active
        ConnStatus.Closed -> R.string.conns_st_closed
        ConnStatus.Blocked -> R.string.conns_st_blocked
    },
)

internal fun toneOf(row: ConnRow): StatusTone = when {
    row.status == ConnStatus.Closed -> StatusTone.Neutral
    row.kind == ConnChip.Block -> StatusTone.Error
    row.kind == ConnChip.Direct -> StatusTone.Direct
    else -> StatusTone.Connected
}

@Composable
private fun Dot(tone: StatusTone, hollow: Boolean) {
    val c = Lav.colors
    Box(
        Modifier.size(10.dp).background(tone.indicator(c).copy(alpha = if (hollow) 0.5f else 1f), CircleShape).semantics {},
        contentAlignment = Alignment.Center,
    ) {}
}
