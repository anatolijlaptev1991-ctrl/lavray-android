// SPDX-License-Identifier: GPL-3.0-only
package io.github.anatolijlaptev1991_ctrl.lavray.ui.screens.rules

import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.LazyListScope
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.semantics.heading
import androidx.compose.ui.semantics.semantics
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import io.github.anatolijlaptev1991_ctrl.lavray.R
import io.github.anatolijlaptev1991_ctrl.lavray.api.gen.Outbound
import io.github.anatolijlaptev1991_ctrl.lavray.api.gen.RoutingPreset
import io.github.anatolijlaptev1991_ctrl.lavray.ui.design.Lav
import io.github.anatolijlaptev1991_ctrl.lavray.ui.design.LavIcon
import io.github.anatolijlaptev1991_ctrl.lavray.ui.design.LavText
import io.github.anatolijlaptev1991_ctrl.lavray.ui.design.components.ButtonVariant
import io.github.anatolijlaptev1991_ctrl.lavray.ui.design.components.LavBanner
import io.github.anatolijlaptev1991_ctrl.lavray.ui.design.components.LavButton
import io.github.anatolijlaptev1991_ctrl.lavray.ui.design.components.LavChip
import io.github.anatolijlaptev1991_ctrl.lavray.ui.design.components.LavIconButton
import io.github.anatolijlaptev1991_ctrl.lavray.ui.design.components.StatusTone

/** Callbacks the editor needs from the screen. */
internal class EditorActions(
    val edit: (PresetPatch) -> Unit,
    val exists: () -> Unit,
    val toggle: (ListKind) -> Unit,
)

/**
 * Rules of one preset on a page of its own. A built-in preset is read only (its rules are all visible; "customize
 * a copy" makes an editable one); a custom preset takes edits one entry at a time, each saved on its own.
 * Lists are collapsed by default and drawn lazily, so a list of a thousand domains costs nothing until opened.
 */
@Composable
fun PresetEditor(p: RoutingPreset, state: PresetsState, model: PresetsModel, onBack: () -> Unit) {
    val copyName = stringResource(R.string.rt_copyName, presetTitle(p))
    var expanded by remember { mutableStateOf(emptySet<ListKind>()) }
    val editable = !p.builtin
    val actions = remember(p.id, model) {
        EditorActions(
            edit = { model.update(p.id, it) },
            exists = { model.notify(PresetsEvent.RuleExists) },
            toggle = { k -> expanded = if (k in expanded) expanded - k else expanded + k },
        )
    }
    Column(Modifier.fillMaxSize()) {
        EditorBar(p, onBack)
        RestartBanner()
        LazyColumn(Modifier.fillMaxSize(), contentPadding = PaddingValues(horizontal = 16.dp, vertical = 8.dp), verticalArrangement = Arrangement.spacedBy(4.dp)) {
            item(key = "head") {
                Head(p, state, enabled = !state.busy, onApply = { model.activate(p.id) }, onCustomize = {
                    model.copy(p.id, copyName, activateIt = true)
                    onBack()
                })
            }
            item(key = "default") { DefaultOutbound(p, editable) { actions.edit(PresetPatch(defaultOutbound = it)) } }
            if (PresetCatalog.ruleCount(p) == 0) item(key = "none") { Note(stringResource(R.string.rt_rulesEmpty)) }
            ListKind.entries.forEach { kind -> listSection(p, kind, kind in expanded, editable, actions) }
            processSection(p, editable, actions)
        }
    }
}

@Composable
private fun EditorBar(p: RoutingPreset, onBack: () -> Unit) {
    Row(Modifier.fillMaxWidth().padding(start = 4.dp, end = 16.dp), verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(4.dp)) {
        LavIconButton(LavIcon.ArrowLeft, stringResource(R.string.common_back), onBack, tint = Lav.colors.textPrimary)
        LavText(presetTitle(p), Modifier.weight(1f).semantics { heading() }, style = Lav.type.h1, maxLines = 1, overflow = TextOverflow.Ellipsis)
    }
}

@Composable
private fun Head(p: RoutingPreset, state: PresetsState, enabled: Boolean, onApply: () -> Unit, onCustomize: () -> Unit) {
    Column(verticalArrangement = Arrangement.spacedBy(8.dp), modifier = Modifier.padding(bottom = 8.dp)) {
        if (p.builtin) {
            LavBanner(
                StatusTone.Neutral, stringResource(R.string.rt_ro),
                actions = { LavButton(stringResource(R.string.rt_customizeCopy), onCustomize, enabled = enabled) },
            )
        }
        if (p.id == state.activeId) {
            LavText(stringResource(R.string.ra_active), style = Lav.type.small, color = Lav.colors.connectedText)
        } else {
            LavButton(stringResource(R.string.ra_apply), onApply, variant = ButtonVariant.Primary, enabled = enabled)
        }
    }
}

@Composable
private fun DefaultOutbound(p: RoutingPreset, editable: Boolean, onPick: (Outbound) -> Unit) {
    val current = PresetsReducer.defaultOutbound(p)
    Column(Modifier.padding(bottom = 4.dp)) {
        LavText(stringResource(R.string.rt_default), style = Lav.type.small, color = Lav.colors.textSecondary)
        Row(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
            OUTBOUND_CHOICES.forEach { o ->
                // A built-in preset shows its choice but does not take a change.
                LavChip(stringResource(PresetCatalog.outboundLabel(o)), current == o, { if (editable) onPick(o) })
            }
        }
    }
}

@Composable
private fun Note(text: String) {
    LavText(text, Modifier.padding(vertical = 8.dp), style = Lav.type.small, color = Lav.colors.textTertiary)
}

// ---- lists

private fun LazyListScope.listSection(p: RoutingPreset, kind: ListKind, open: Boolean, editable: Boolean, actions: EditorActions) {
    val entries = kind.of(p)
    if (entries.isEmpty() && !editable) return
    item(key = "h:${kind.name}", contentType = TYPE_HEADER) { SectionHeader(kind, entries.size, open) { actions.toggle(kind) } }
    if (!open) return
    items(entries.size, key = { "e:${kind.name}:$it" }, contentType = { TYPE_ENTRY }) { i ->
        EntryRow(entries[i], editable) { actions.edit(PresetPatch(listEdit = ListEdit.Remove(kind, entries[i]))) }
    }
    if (editable) {
        item(key = "a:${kind.name}", contentType = TYPE_ADD) {
            AddEntry(kind) { raw ->
                when (PresetDraft.addEntry(entries, raw)) {
                    is AddResult.Added -> actions.edit(PresetPatch(listEdit = ListEdit.Add(kind, raw)))
                    AddResult.Exists -> actions.exists()
                    AddResult.Empty -> Unit
                }
            }
        }
    }
}

private const val TYPE_HEADER = 0
private const val TYPE_ENTRY = 1
private const val TYPE_ADD = 2
