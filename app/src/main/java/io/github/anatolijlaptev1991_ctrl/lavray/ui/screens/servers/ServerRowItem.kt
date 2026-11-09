// SPDX-License-Identifier: GPL-3.0-only
package io.github.anatolijlaptev1991_ctrl.lavray.ui.screens.servers

import androidx.compose.foundation.background
import androidx.compose.foundation.border
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.heightIn
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.runtime.Composable
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.semantics.Role
import androidx.compose.ui.semantics.contentDescription
import androidx.compose.ui.semantics.heading
import androidx.compose.ui.semantics.selected
import androidx.compose.ui.semantics.semantics
import androidx.compose.ui.text.style.TextAlign
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import io.github.anatolijlaptev1991_ctrl.lavray.R
import io.github.anatolijlaptev1991_ctrl.lavray.ui.design.Lav
import io.github.anatolijlaptev1991_ctrl.lavray.ui.design.LavIcon
import io.github.anatolijlaptev1991_ctrl.lavray.ui.design.LavIconView
import io.github.anatolijlaptev1991_ctrl.lavray.ui.design.LavText
import io.github.anatolijlaptev1991_ctrl.lavray.ui.design.components.LavBadge
import io.github.anatolijlaptev1991_ctrl.lavray.ui.design.components.LavIconButton
import io.github.anatolijlaptev1991_ctrl.lavray.ui.design.components.StatusDot
import io.github.anatolijlaptev1991_ctrl.lavray.ui.design.components.StatusTone
import io.github.anatolijlaptev1991_ctrl.lavray.ui.design.components.lavClickable
import io.github.anatolijlaptev1991_ctrl.lavray.ui.i18n.UnitFormat

/** Visual flags of one row; computed by the list so the row itself reads nothing else. */
data class RowFlags(
    val active: Boolean,
    val connecting: Boolean,
    val current: Boolean,
    val selecting: Boolean,
    val checked: Boolean,
    val compact: Boolean,
)

/**
 * One server. Name, protocol badge (neutral), address in mono, latency with its status in words as well as
 * colour (an X for unreachable, "n/a" when the test does not apply), favourite star and the actions button.
 * Rows are 56 dp or more, the actions button is a 48 dp target.
 */
@Composable
fun ServerRowItem(
    row: ServerRow,
    flags: RowFlags,
    fmt: UnitFormat,
    onClick: (ServerRow) -> Unit,
    onLongClick: (ServerRow) -> Unit,
    onMenu: (ServerRow) -> Unit,
    onFavorite: (ServerRow) -> Unit,
) {
    val c = Lav.colors
    val shape = RoundedCornerShape(10.dp)
    val vertical = if (flags.compact) 4.dp else 8.dp
    Row(
        Modifier
            .fillMaxWidth()
            .heightIn(min = if (flags.compact) 48.dp else 60.dp)
            .background(if (flags.current || flags.checked) c.accentSoft else androidx.compose.ui.graphics.Color.Transparent, shape)
            .then(if (flags.active) Modifier.border(1.dp, c.connected, shape) else Modifier)
            .lavClickable({ onClick(row) }, shape, role = Role.Button, onLongClick = { onLongClick(row) })
            .semantics { selected = flags.checked || flags.current }
            .padding(start = 12.dp, top = vertical, bottom = vertical),
        verticalAlignment = Alignment.CenterVertically,
        horizontalArrangement = Arrangement.spacedBy(8.dp),
    ) {
        if (flags.selecting) CheckMark(flags.checked)
        Column(Modifier.weight(1f), verticalArrangement = Arrangement.spacedBy(2.dp)) {
            LavText(row.name, style = Lav.type.body, maxLines = 2, overflow = TextOverflow.Ellipsis)
            Row(horizontalArrangement = Arrangement.spacedBy(6.dp), verticalAlignment = Alignment.CenterVertically) {
                LavBadge(row.type.label())
                if (!flags.compact) {
                    LavText("${row.address}:${row.port}", Modifier.weight(1f, fill = false), style = Lav.type.mono.copy(fontSize = Lav.type.caption.fontSize), color = c.textTertiary, maxLines = 1, overflow = TextOverflow.Ellipsis)
                }
            }
            if (flags.active) {
                StatusDot(if (flags.connecting) StatusTone.Connecting else StatusTone.Connected, stringResource(R.string.srv_active))
            }
            if (row.unsupported) LavText(stringResource(R.string.prof_unsupported), style = Lav.type.caption, color = c.errorText)
        }
        LatencyText(row.latency, fmt)
        LavIconButton(
            LavIcon.Star,
            stringResource(if (row.favorite) R.string.prof_favRm else R.string.prof_favAdd),
            { onFavorite(row) },
            tint = if (row.favorite) c.accent else c.textTertiary,
        )
        LavIconButton(LavIcon.More, stringResource(R.string.prof_menu), { onMenu(row) })
    }
}

@Composable
private fun LatencyText(cell: LatencyCell, fmt: UnitFormat) {
    val c = Lav.colors
    val (text, color) = when (cell.status) {
        LatencyStatus.Ok -> fmt.latency(cell.ms) to c.textSecondary
        LatencyStatus.Down -> "✕" to c.errorText
        LatencyStatus.NotApplicable -> stringResource(R.string.test_na) to c.textTertiary
        LatencyStatus.Unknown -> "—" to c.textTertiary
    }
    val description = if (cell.status == LatencyStatus.Down) stringResource(R.string.filter_down) else text
    LavText(
        text, Modifier.semantics { contentDescription = description },
        style = Lav.type.mono, color = color, textAlign = TextAlign.End, maxLines = 1,
    )
}

/** A check box that is drawn, not an icon font: frame 1.5 dp, filled with the accent when checked. */
@Composable
private fun CheckMark(checked: Boolean) {
    val c = Lav.colors
    val shape = RoundedCornerShape(6.dp)
    Box(
        Modifier.size(22.dp).background(if (checked) c.accent else androidx.compose.ui.graphics.Color.Transparent, shape)
            .border(1.5.dp, if (checked) c.accent else c.borderControl, shape),
        contentAlignment = Alignment.Center,
    ) { if (checked) LavIconView(LavIcon.Check, c.onAccent, size = 16.dp) }
}

/** Group header row: name and count; subscription groups carry a small feed icon. */
@Composable
fun GroupHeaderItem(group: GroupInfo, count: Int) {
    val c = Lav.colors
    Row(
        Modifier.fillMaxWidth().padding(start = 12.dp, end = 12.dp, top = 12.dp, bottom = 4.dp),
        verticalAlignment = Alignment.CenterVertically,
        horizontalArrangement = Arrangement.spacedBy(8.dp),
    ) {
        LavIconView(if (group.subscription) LavIcon.Rss else LavIcon.Folder, c.textTertiary, size = 16.dp)
        LavText(group.name, Modifier.weight(1f, fill = false).semantics { heading() }, style = Lav.type.bodyStrong, color = c.textSecondary, maxLines = 1, overflow = TextOverflow.Ellipsis)
        LavText(count.toString(), style = Lav.type.caption, color = c.textTertiary)
    }
}

