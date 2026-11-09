// SPDX-License-Identifier: GPL-3.0-only
package io.github.anatolijlaptev1991_ctrl.lavray.ui.screens.rules

import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.FlowRow
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.heightIn
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.lazy.LazyListScope
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.foundation.text.KeyboardActions
import androidx.compose.foundation.text.KeyboardOptions
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.semantics.Role
import androidx.compose.ui.semantics.stateDescription
import androidx.compose.ui.semantics.semantics
import androidx.compose.ui.text.input.ImeAction
import androidx.compose.ui.text.input.KeyboardType
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import io.github.anatolijlaptev1991_ctrl.lavray.R
import io.github.anatolijlaptev1991_ctrl.lavray.api.gen.ProcessRule
import io.github.anatolijlaptev1991_ctrl.lavray.api.gen.RoutingPreset
import io.github.anatolijlaptev1991_ctrl.lavray.ui.design.Lav
import io.github.anatolijlaptev1991_ctrl.lavray.ui.design.LavIcon
import io.github.anatolijlaptev1991_ctrl.lavray.ui.design.LavIconView
import io.github.anatolijlaptev1991_ctrl.lavray.ui.design.LavText
import io.github.anatolijlaptev1991_ctrl.lavray.ui.design.Radius
import io.github.anatolijlaptev1991_ctrl.lavray.ui.design.components.LavBadge
import io.github.anatolijlaptev1991_ctrl.lavray.ui.design.components.LavBanner
import io.github.anatolijlaptev1991_ctrl.lavray.ui.design.components.LavIconButton
import io.github.anatolijlaptev1991_ctrl.lavray.ui.design.components.LavSectionLabel
import io.github.anatolijlaptev1991_ctrl.lavray.ui.design.components.LavTextField
import io.github.anatolijlaptev1991_ctrl.lavray.ui.design.components.StatusTone
import io.github.anatolijlaptev1991_ctrl.lavray.ui.design.components.lavClickable

private const val TYPE_PROC = 3

/** Header of one list: "Proxy · Domain" with the number of entries; a tap opens or closes the list. */
@Composable
internal fun SectionHeader(kind: ListKind, count: Int, open: Boolean, onToggle: () -> Unit) {
    val c = Lav.colors
    val shape = RoundedCornerShape(Radius.md)
    val state = stringResource(if (open) R.string.ra_expanded else R.string.ra_collapsed)
    val kindLabel = stringResource(if (kind.ip) R.string.rt_type_ip else R.string.rt_type_domain)
    val title = "${stringResource(PresetCatalog.outboundLabel(kind.outbound))} · $kindLabel"
    Row(
        Modifier.fillMaxWidth().heightIn(min = 56.dp)
            .lavClickable(onToggle, shape, role = Role.Button)
            .semantics { stateDescription = state }
            .padding(horizontal = 12.dp),
        verticalAlignment = Alignment.CenterVertically,
        horizontalArrangement = Arrangement.spacedBy(8.dp),
    ) {
        LavText(title, Modifier.weight(1f), style = Lav.type.bodyStrong)
        LavText(count.toString(), style = Lav.type.small, color = c.textTertiary)
        LavIconView(if (open) LavIcon.ChevronDown else LavIcon.ChevronRight, c.textSecondary, size = 18.dp)
    }
}

/** One rule entry in mono type; the delete button is 48 dp and named after the entry for TalkBack. */
@Composable
internal fun EntryRow(entry: String, editable: Boolean, onDelete: () -> Unit) {
    Row(Modifier.fillMaxWidth().heightIn(min = 44.dp).padding(start = 16.dp), verticalAlignment = Alignment.CenterVertically) {
        LavText(entry, Modifier.weight(1f).padding(vertical = 6.dp), style = Lav.type.mono, color = Lav.colors.textSecondary, maxLines = 3, overflow = TextOverflow.Ellipsis)
        if (editable) {
            val label = "${stringResource(R.string.common_delete)} $entry"
            LavIconButton(LavIcon.Close, label, onDelete)
        }
    }
}

/** Field and button that add an entry; Done on the keyboard adds too. The field is cleared after each add. */
@Composable
internal fun AddEntry(kind: ListKind, onAdd: (String) -> Unit) {
    var text by remember(kind) { mutableStateOf("") }
    val hint = if (kind.ip) "10.0.0.0/8" else "domain.example"
    val submit = { onAdd(text); text = "" }
    LavTextField(
        text, { text = it }, Modifier.padding(start = 12.dp, top = 4.dp, bottom = 8.dp), placeholder = hint, mono = true,
        keyboardOptions = KeyboardOptions(keyboardType = KeyboardType.Uri, imeAction = ImeAction.Done),
        keyboardActions = KeyboardActions(onDone = { if (text.isNotBlank()) submit() }),
        trailing = { LavIconButton(LavIcon.Plus, stringResource(R.string.rt_addRule), { if (text.isNotBlank()) submit() }, tint = Lav.colors.accent) },
    )
}

// ---- app rules

/**
 * App rules select a connection by process name. A phone has no such names for the rules to match: the core drops
 * the condition on Android, so none of these rules does anything here. They are listed in full, marked as having no
 * effect, and kept in the preset so that it works unchanged on the computer.
 */
internal fun LazyListScope.processSection(p: RoutingPreset, editable: Boolean, actions: EditorActions) {
    item(key = "proc", contentType = TYPE_PROC) {
        Column(Modifier.padding(top = 8.dp), verticalArrangement = Arrangement.spacedBy(4.dp)) {
            LavSectionLabel(stringResource(R.string.rt_proc_t))
            if (p.processRules.isNotEmpty()) LavBanner(StatusTone.Neutral, stringResource(R.string.ra_proc_note))
            if (p.processRules.isEmpty()) LavText(stringResource(R.string.rt_proc_empty), Modifier.padding(vertical = 8.dp), style = Lav.type.small, color = Lav.colors.textTertiary)
        }
    }
    items(p.processRules.size, key = { "p:$it" }, contentType = { TYPE_PROC }) { i ->
        val rule = p.processRules[i]
        ProcessRuleRow(rule, editable) { actions.edit(PresetPatch(removeProcessRule = rule)) }
    }
}

@Composable
private fun ProcessRuleRow(rule: ProcessRule, editable: Boolean, onDelete: () -> Unit) {
    val names = rule.processNames.joinToString(", ")
    Row(Modifier.fillMaxWidth().heightIn(min = 56.dp).padding(start = 12.dp), verticalAlignment = Alignment.CenterVertically) {
        Column(Modifier.weight(1f).padding(vertical = 6.dp), verticalArrangement = Arrangement.spacedBy(4.dp)) {
            LavText(names, style = Lav.type.mono, color = Lav.colors.textTertiary, maxLines = 3, overflow = TextOverflow.Ellipsis)
            FlowRow(horizontalArrangement = Arrangement.spacedBy(6.dp), verticalArrangement = Arrangement.spacedBy(4.dp)) {
                LavBadge(stringResource(PresetCatalog.outboundLabel(rule.outbound)))
                LavBadge(platformText(rule))
                LavBadge(stringResource(R.string.rt_plat_na), tone = StatusTone.Connecting)
            }
        }
        if (editable) LavIconButton(LavIcon.Close, "${stringResource(R.string.common_delete)} $names", onDelete)
    }
}

@Composable
private fun platformText(rule: ProcessRule): String = when (val pl = PresetCatalog.platformOf(rule)) {
    ProcessPlatform.AllSystems -> stringResource(R.string.rt_proc_allSystems)
    ProcessPlatform.WindowsOnly -> stringResource(R.string.rt_proc_winOnly)
    is ProcessPlatform.Listed -> pl.names
}
