// SPDX-License-Identifier: GPL-3.0-only
package io.github.anatolijlaptev1991_ctrl.lavray.ui.screens.rules

import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.FlowRow
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.runtime.Composable
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.res.pluralStringResource
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.semantics.Role
import androidx.compose.ui.semantics.selected
import androidx.compose.ui.semantics.semantics
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import io.github.anatolijlaptev1991_ctrl.lavray.R
import io.github.anatolijlaptev1991_ctrl.lavray.api.gen.RoutingPreset
import io.github.anatolijlaptev1991_ctrl.lavray.ui.design.Lav
import io.github.anatolijlaptev1991_ctrl.lavray.ui.design.LavIcon
import io.github.anatolijlaptev1991_ctrl.lavray.ui.design.LavText
import io.github.anatolijlaptev1991_ctrl.lavray.ui.design.Radius
import io.github.anatolijlaptev1991_ctrl.lavray.ui.design.components.ButtonVariant
import io.github.anatolijlaptev1991_ctrl.lavray.ui.design.components.LavBadge
import io.github.anatolijlaptev1991_ctrl.lavray.ui.design.components.LavButton
import io.github.anatolijlaptev1991_ctrl.lavray.ui.design.components.LavCard
import io.github.anatolijlaptev1991_ctrl.lavray.ui.design.components.LavIconButton
import io.github.anatolijlaptev1991_ctrl.lavray.ui.design.components.StatusDot
import io.github.anatolijlaptev1991_ctrl.lavray.ui.design.components.StatusTone
import io.github.anatolijlaptev1991_ctrl.lavray.ui.design.components.lavClickable

/** Callbacks of one preset card. */
class PresetCardActions(
    val onOpen: (RoutingPreset) -> Unit,
    val onActivate: (RoutingPreset) -> Unit,
    val onMenu: (RoutingPreset) -> Unit,
)

/**
 * One preset: name, a "built-in" or "custom" badge, the description of a built-in one, the number of rules and
 * the revision. The active preset shows an "Active" status; any other has an explicit "Use" button, so a
 * stray touch on the card only opens its rules. App rules, which do nothing on a phone, are counted openly.
 */
@Composable
fun PresetCard(p: RoutingPreset, active: Boolean, enabled: Boolean, actions: PresetCardActions, modifier: Modifier = Modifier) {
    val name = presetTitle(p)
    LavCard(modifier.fillMaxWidth(), strong = active) {
        Column(Modifier.padding(start = 14.dp, top = 4.dp, bottom = 10.dp, end = 4.dp)) {
            Row(verticalAlignment = Alignment.CenterVertically) {
                Column(
                    Modifier.weight(1f)
                        .lavClickable({ actions.onOpen(p) }, RoundedCornerShape(Radius.md), role = Role.Button)
                        .semantics { selected = active }
                        .padding(vertical = 10.dp),
                    verticalArrangement = Arrangement.spacedBy(6.dp),
                ) {
                    Title(p, name)
                    Description(p)
                    Facts(p)
                }
                LavIconButton(LavIcon.More, stringResource(R.string.rt_menu), { actions.onMenu(p) })
            }
            if (active) {
                StatusDot(StatusTone.Connected, stringResource(R.string.ra_active), Modifier.padding(bottom = 4.dp))
            } else {
                LavButton(stringResource(R.string.ra_apply), { actions.onActivate(p) }, variant = ButtonVariant.Secondary, enabled = enabled)
            }
        }
    }
}

@Composable
private fun Title(p: RoutingPreset, name: String) {
    Row(horizontalArrangement = Arrangement.spacedBy(8.dp), verticalAlignment = Alignment.CenterVertically) {
        LavText(name, Modifier.weight(1f, fill = false), style = Lav.type.h2, maxLines = 2, overflow = TextOverflow.Ellipsis)
        LavBadge(stringResource(if (p.builtin) R.string.rt_builtin else R.string.rt_mine))
    }
}

@Composable
private fun Description(p: RoutingPreset) {
    val text = PresetCatalog.textOf(p.id) ?: return
    LavText(stringResource(text.descRes), style = Lav.type.small, color = Lav.colors.textSecondary)
}

@Composable
private fun Facts(p: RoutingPreset) {
    val count = PresetCatalog.ruleCount(p)
    FlowRow(horizontalArrangement = Arrangement.spacedBy(6.dp), verticalArrangement = Arrangement.spacedBy(4.dp)) {
        LavBadge(pluralStringResource(R.plurals.pl_r, count, count.toString()))
        LavBadge("${stringResource(R.string.preset_rev)} ${p.revision}")
        if (!p.builtin && p.sourceUpdated) LavBadge(stringResource(R.string.rt_sourceUpdated), tone = StatusTone.Connecting)
    }
    val inert = PresetCatalog.inertOnPhone(p)
    if (inert > 0) {
        LavText(stringResource(R.string.ra_proc_inert, inert.toString()), style = Lav.type.caption, color = Lav.colors.textTertiary)
    }
}
