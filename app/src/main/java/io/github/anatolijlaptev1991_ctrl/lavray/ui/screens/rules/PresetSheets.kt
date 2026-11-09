// SPDX-License-Identifier: GPL-3.0-only
package io.github.anatolijlaptev1991_ctrl.lavray.ui.screens.rules

import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.unit.dp
import io.github.anatolijlaptev1991_ctrl.lavray.R
import io.github.anatolijlaptev1991_ctrl.lavray.api.gen.RoutingPreset
import io.github.anatolijlaptev1991_ctrl.lavray.ui.design.Lav
import io.github.anatolijlaptev1991_ctrl.lavray.ui.design.LavIcon
import io.github.anatolijlaptev1991_ctrl.lavray.ui.design.LavIconView
import io.github.anatolijlaptev1991_ctrl.lavray.ui.design.components.ButtonVariant
import io.github.anatolijlaptev1991_ctrl.lavray.ui.design.components.LavButton
import io.github.anatolijlaptev1991_ctrl.lavray.ui.design.components.LavConfirmDialog
import io.github.anatolijlaptev1991_ctrl.lavray.ui.design.components.LavListRow
import io.github.anatolijlaptev1991_ctrl.lavray.ui.design.components.LavSheet
import io.github.anatolijlaptev1991_ctrl.lavray.ui.design.components.LavTextField

/** What a name sheet is for. */
enum class NameMode { SaveAs, Rename, Empty }

/** Which sheet or dialog the presets screen shows. One at a time; null means none. */
sealed interface PresetSheet {
    data object New : PresetSheet
    data class Menu(val preset: RoutingPreset) : PresetSheet
    data class Name(val mode: NameMode, val preset: RoutingPreset?) : PresetSheet
    data class ConfirmDelete(val preset: RoutingPreset) : PresetSheet
    data class ConfirmReset(val preset: RoutingPreset) : PresetSheet
}

/** What the sheets can do; the screen owns each action (file pickers need an activity result launcher). */
class PresetSheetActions(
    val model: PresetsModel,
    val active: RoutingPreset?,
    val show: (PresetSheet?) -> Unit,
    val onExport: (RoutingPreset) -> Unit,
    val onImport: () -> Unit,
)

@Composable
fun PresetSheets(sheet: PresetSheet?, actions: PresetSheetActions) {
    val close = { actions.show(null) }
    when (sheet) {
        null -> Unit
        PresetSheet.New -> NewSheet(actions, close)
        is PresetSheet.Menu -> CardMenuSheet(sheet.preset, actions, close)
        is PresetSheet.Name -> NameSheet(sheet, actions, close)
        is PresetSheet.ConfirmDelete -> ConfirmDeleteDialog(sheet.preset, actions, close)
        is PresetSheet.ConfirmReset -> ConfirmResetDialog(sheet.preset, actions, close)
    }
}

@Composable
private fun MenuRow(icon: LavIcon, text: String, onClick: () -> Unit, danger: Boolean = false) {
    val c = Lav.colors
    LavListRow(
        title = text, onClick = onClick,
        leading = { LavIconView(icon, if (danger) c.errorText else c.textSecondary, size = 20.dp) },
    )
}

/** "New preset": copy the active one, start an empty one, or import a file. */
@Composable
private fun NewSheet(actions: PresetSheetActions, close: () -> Unit) {
    LavSheet(stringResource(R.string.rt_new), close) {
        if (actions.active != null) {
            MenuRow(LavIcon.Copy, stringResource(R.string.rt_saveAs), { actions.show(PresetSheet.Name(NameMode.SaveAs, actions.active)) })
        }
        MenuRow(LavIcon.Plus, stringResource(R.string.rt_newEmpty), { actions.show(PresetSheet.Name(NameMode.Empty, null)) })
        MenuRow(LavIcon.Download, stringResource(R.string.rt_importPreset), { close(); actions.onImport() })
    }
}

/**
 * Menu of one preset. A built-in one can be duplicated, exported and the file import started from it; rename,
 * reset to the original and delete belong to the person's own presets (reset to copies of a built-in one).
 */
@Composable
private fun CardMenuSheet(p: RoutingPreset, actions: PresetSheetActions, close: () -> Unit) {
    val copyName = stringResource(R.string.rt_copyName, presetTitle(p))
    LavSheet(presetTitle(p), close) {
        if (!p.builtin) MenuRow(LavIcon.Pencil, stringResource(R.string.rt_rename), { actions.show(PresetSheet.Name(NameMode.Rename, p)) })
        MenuRow(LavIcon.Copy, stringResource(R.string.rt_duplicate), {
            close()
            actions.model.copy(p.id, copyName, activateIt = false)
        })
        if (!p.builtin && p.builtinSourceId.isNotEmpty()) {
            MenuRow(LavIcon.Refresh, stringResource(R.string.rt_resetSource), { actions.show(PresetSheet.ConfirmReset(p)) })
        }
        MenuRow(LavIcon.Share, stringResource(R.string.rt_exportPreset), { close(); actions.onExport(p) })
        MenuRow(LavIcon.Download, stringResource(R.string.rt_importPreset), { close(); actions.onImport() })
        if (!p.builtin) MenuRow(LavIcon.Trash, stringResource(R.string.rt_deletePreset), { actions.show(PresetSheet.ConfirmDelete(p)) }, danger = true)
    }
}

/** Name of a preset for "save as", "create empty" and "rename": a blank name is refused, the length is capped. */
@Composable
private fun NameSheet(sheet: PresetSheet.Name, actions: PresetSheetActions, close: () -> Unit) {
    val initial = when (sheet.mode) {
        NameMode.Rename -> sheet.preset?.displayName.orEmpty()
        NameMode.SaveAs -> sheet.preset?.let { stringResource(R.string.rt_copyName, presetTitle(it)) }.orEmpty()
        NameMode.Empty -> stringResource(R.string.rt_newPresetName)
    }
    var name by remember { mutableStateOf(initial) }
    var tried by remember { mutableStateOf(false) }
    val title = when (sheet.mode) {
        NameMode.Rename -> R.string.rt_rename
        NameMode.SaveAs -> R.string.rt_saveAs
        NameMode.Empty -> R.string.rt_newEmpty
    }
    val submit = {
        if (!PresetDraft.nameValid(name)) {
            tried = true
        } else {
            val clean = PresetDraft.cleanName(name)
            close()
            when {
                sheet.mode == NameMode.Rename && sheet.preset != null -> actions.model.update(sheet.preset.id, PresetPatch(displayName = clean))
                sheet.mode == NameMode.SaveAs && sheet.preset != null -> actions.model.copy(sheet.preset.id, clean, activateIt = true)
                else -> actions.model.createEmpty(clean)
            }
        }
    }
    LavSheet(stringResource(title), close) {
        LavTextField(
            name, { name = it.take(PresetDraft.MAX_NAME_LENGTH); tried = false },
            label = stringResource(R.string.rt_presetName),
            error = if (tried && !PresetDraft.nameValid(name)) stringResource(R.string.rt_nameEmpty) else null,
        )
        Row(Modifier.fillMaxWidth(), horizontalArrangement = Arrangement.spacedBy(8.dp, Alignment.End)) {
            LavButton(stringResource(R.string.common_cancel), close, variant = ButtonVariant.Ghost)
            val confirm = if (sheet.mode == NameMode.Empty) R.string.common_create else R.string.common_save
            LavButton(stringResource(confirm), submit, variant = ButtonVariant.Primary)
        }
    }
}

@Composable
private fun ConfirmDeleteDialog(p: RoutingPreset, actions: PresetSheetActions, close: () -> Unit) {
    LavConfirmDialog(
        title = stringResource(R.string.rt_deletePreset),
        text = stringResource(R.string.rt_deleteConfirm, presetTitle(p)),
        confirmLabel = stringResource(R.string.common_delete),
        dismissLabel = stringResource(R.string.common_cancel),
        onConfirm = { close(); actions.model.remove(p.id) },
        onDismiss = close,
        danger = true,
    )
}

@Composable
private fun ConfirmResetDialog(p: RoutingPreset, actions: PresetSheetActions, close: () -> Unit) {
    LavConfirmDialog(
        title = stringResource(R.string.rt_resetSource),
        text = stringResource(R.string.rt_resetConfirm, presetTitle(p)),
        confirmLabel = stringResource(R.string.rt_resetSource),
        dismissLabel = stringResource(R.string.common_cancel),
        onConfirm = { close(); actions.model.reset(p.id) },
        onDismiss = close,
    )
}
