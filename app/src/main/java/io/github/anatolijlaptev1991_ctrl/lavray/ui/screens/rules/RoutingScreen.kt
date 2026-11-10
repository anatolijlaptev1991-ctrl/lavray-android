// SPDX-License-Identifier: GPL-3.0-only
package io.github.anatolijlaptev1991_ctrl.lavray.ui.screens.rules

import android.content.Context
import android.net.Uri
import androidx.activity.compose.BackHandler
import androidx.activity.compose.rememberLauncherForActivityResult
import androidx.activity.result.contract.ActivityResultContracts
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.unit.dp
import androidx.lifecycle.Lifecycle
import androidx.lifecycle.compose.LifecycleEventEffect
import androidx.lifecycle.compose.LocalLifecycleOwner
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import androidx.lifecycle.repeatOnLifecycle
import io.github.anatolijlaptev1991_ctrl.lavray.R
import io.github.anatolijlaptev1991_ctrl.lavray.api.gen.RoutingPreset
import io.github.anatolijlaptev1991_ctrl.lavray.ui.LocalEnv
import io.github.anatolijlaptev1991_ctrl.lavray.ui.core.ErrorKind
import io.github.anatolijlaptev1991_ctrl.lavray.ui.core.textRes
import io.github.anatolijlaptev1991_ctrl.lavray.ui.design.LavIcon
import io.github.anatolijlaptev1991_ctrl.lavray.ui.design.components.ButtonVariant
import io.github.anatolijlaptev1991_ctrl.lavray.ui.design.components.LavButton
import io.github.anatolijlaptev1991_ctrl.lavray.ui.design.components.LavEmptyState
import io.github.anatolijlaptev1991_ctrl.lavray.ui.design.components.LavErrorState
import io.github.anatolijlaptev1991_ctrl.lavray.ui.design.components.LavSkeletonRows
import kotlinx.coroutines.launch

/**
 * Routing: the list of route presets (built-in ones marked, LAV Preset RU first and active by default), apply,
 * create, duplicate, reset, delete, export and import through the system file picker, and an editor page for the
 * rules of one preset. Edits to the active preset or switching it show the restart banner when a tunnel is up.
 */
@Composable
fun RoutingScreen() {
    val env = LocalEnv.current
    val scope = rememberCoroutineScope()
    val model = remember(env) { PresetsModel(env.session, scope, env.restart) }
    val state by model.state.collectAsStateWithLifecycle()
    var sheet by remember { mutableStateOf<PresetSheet?>(null) }

    LifecycleEventEffect(Lifecycle.Event.ON_START) { model.refresh() }
    PresetToasts(model)
    BackHandler(enabled = state.open != null) { model.dispatch(PresetsAction.Open(null)) }

    val files = rememberPresetFiles(model)
    val actions = PresetSheetActions(model, state.active, { sheet = it }, files.export, files.import)
    val open = state.open
    if (open != null) {
        PresetEditor(open, state, model, onBack = { model.dispatch(PresetsAction.Open(null)) })
    } else {
        PresetList(state, model, onNew = { sheet = PresetSheet.New }, onMenu = { sheet = PresetSheet.Menu(it) })
    }
    PresetSheets(sheet, actions)
}

@Composable
private fun PresetList(state: PresetsState, model: PresetsModel, onNew: () -> Unit, onMenu: (RoutingPreset) -> Unit) {
    val cardActions = remember(model) {
        PresetCardActions(
            onOpen = { model.dispatch(PresetsAction.Open(it.id)) },
            onActivate = { model.activate(it.id) },
            onMenu = onMenu,
        )
    }
    Column(Modifier.fillMaxSize()) {
        Row(Modifier.fillMaxWidth().padding(horizontal = 16.dp, vertical = 8.dp), verticalAlignment = Alignment.CenterVertically) {
            LavButton(stringResource(R.string.rt_new), onNew, variant = ButtonVariant.Primary, icon = LavIcon.Plus)
        }
        ListBody(state, cardActions, model)
    }
}

@Composable
private fun ListBody(state: PresetsState, actions: PresetCardActions, model: PresetsModel) {
    when {
        state.loading -> LavSkeletonRows(5, stringResource(R.string.lazy_loading), Modifier.padding(16.dp))
        state.error != null && state.presets.isEmpty() -> LavErrorState(
            stringResource(R.string.core_err_t), stringResource(state.error.textRes()),
            actions = { LavButton(stringResource(R.string.err_retry), { model.refresh() }, variant = ButtonVariant.Primary) },
        )
        state.presets.isEmpty() -> LavEmptyState(stringResource(R.string.rt_empty_t), text = stringResource(R.string.rt_empty_d), icon = LavIcon.Route)
        else -> LazyColumn(
            Modifier.fillMaxSize(),
            contentPadding = PaddingValues(horizontal = 16.dp, vertical = 4.dp),
            verticalArrangement = Arrangement.spacedBy(12.dp),
        ) {
            items(state.presets, key = { it.id }, contentType = { if (it.builtin) 0 else 1 }) { p ->
                PresetCard(p, active = p.id == state.activeId, enabled = !state.busy, actions = actions)
            }
            item(key = "tester", contentType = 2) { RouteTesterCard(model) }
        }
    }
}

/** Export and import launchers: the system picker returns a content URI, no storage permission is involved. */
private class PresetFileActions(val export: (RoutingPreset) -> Unit, val import: () -> Unit)

@Composable
private fun rememberPresetFiles(model: PresetsModel): PresetFileActions {
    val context = LocalContext.current
    val scope = rememberCoroutineScope()
    var pending by remember { mutableStateOf<String?>(null) }
    val saver = rememberLauncherForActivityResult(ActivityResultContracts.CreateDocument(PresetFiles.MIME_JSON)) { uri ->
        val text = pending
        pending = null
        if (uri != null && text != null) scope.launch { model.notify(writeResult(context, uri, text)) }
    }
    val opener = rememberLauncherForActivityResult(ActivityResultContracts.OpenDocument()) { uri ->
        if (uri != null) scope.launch { importFrom(context, uri, model) }
    }
    return remember(model) {
        PresetFileActions(
            export = { p ->
                scope.launch {
                    val res = model.exportOf(p.id) ?: return@launch
                    pending = res.json
                    saver.launch(PresetFiles.exportFileName(res.suggestedName))
                }
            },
            import = { opener.launch(PresetFiles.OPEN_TYPES) },
        )
    }
}

private suspend fun writeResult(context: Context, uri: Uri, text: String): PresetsEvent =
    if (PresetFiles.write(context.contentResolver, uri, text)) PresetsEvent.Exported else PresetsEvent.Failed(ErrorKind.Generic)

private suspend fun importFrom(context: Context, uri: Uri, model: PresetsModel) {
    val text = PresetFiles.read(context.contentResolver, uri)
    if (text == null) model.notify(PresetsEvent.Unreadable) else model.importText(text)
}

@Composable
private fun PresetToasts(model: PresetsModel) {
    val env = LocalEnv.current
    val owner = LocalLifecycleOwner.current
    LaunchedEffect(model) {
        owner.lifecycle.repeatOnLifecycle(Lifecycle.State.STARTED) {
            // The hub is told before the event is sent, so the state read here already includes this change.
            model.events.collect { e -> env.toast.show(presetEventText(env.appContext, e, env.restart.state.value.needed)) }
        }
    }
}

/**
 * [pending]: the core said the running tunnel does not have the change yet. The toast then says so instead of
 * "applied" (the tunnel still runs the old preset until the restart).
 */
private fun presetEventText(ctx: Context, e: PresetsEvent, pending: Boolean): String = when (e) {
    PresetsEvent.Activated -> ctx.getString(savedOrPending(pending, R.string.toast_presetActivated))
    PresetsEvent.Saved -> ctx.getString(savedOrPending(pending, R.string.toast_presetSaved))
    PresetsEvent.Copied -> ctx.getString(savedOrPending(pending, R.string.toast_presetCopied))
    PresetsEvent.Deleted -> ctx.getString(savedOrPending(pending, R.string.toast_presetDeleted))
    PresetsEvent.Reset -> ctx.getString(savedOrPending(pending, R.string.toast_presetReset))
    PresetsEvent.Exported -> ctx.getString(R.string.toast_presetExported)
    is PresetsEvent.Imported ->
        if (e.warnings > 0) ctx.getString(R.string.toast_presetImportedWarn, e.warnings.toString()) else ctx.getString(R.string.toast_presetImported)
    PresetsEvent.Unreadable -> ctx.getString(R.string.rt_importFailed)
    PresetsEvent.Conflict -> ctx.getString(R.string.toast_presetConflict)
    PresetsEvent.RuleExists -> ctx.getString(R.string.rt_ruleExists)
    PresetsEvent.EditRejected -> ctx.getString(R.string.rt_editRejected)
    is PresetsEvent.Failed -> ctx.getString(e.kind.textRes())
}
