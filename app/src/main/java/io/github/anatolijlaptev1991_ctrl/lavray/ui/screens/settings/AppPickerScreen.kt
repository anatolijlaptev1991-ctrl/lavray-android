// SPDX-License-Identifier: GPL-3.0-only
package io.github.anatolijlaptev1991_ctrl.lavray.ui.screens.settings

import androidx.compose.foundation.Image
import androidx.compose.foundation.background
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.runtime.Composable
import androidx.compose.runtime.DisposableEffect
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.ImageBitmap
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.unit.dp
import io.github.anatolijlaptev1991_ctrl.lavray.R
import io.github.anatolijlaptev1991_ctrl.lavray.ui.LocalEnv
import io.github.anatolijlaptev1991_ctrl.lavray.ui.design.Lav
import io.github.anatolijlaptev1991_ctrl.lavray.ui.design.LavIcon
import io.github.anatolijlaptev1991_ctrl.lavray.ui.design.LavIconView
import io.github.anatolijlaptev1991_ctrl.lavray.ui.design.LavText
import io.github.anatolijlaptev1991_ctrl.lavray.ui.design.components.LavEmptyState
import io.github.anatolijlaptev1991_ctrl.lavray.ui.design.components.LavListRow
import io.github.anatolijlaptev1991_ctrl.lavray.ui.design.components.LavSkeletonRows
import io.github.anatolijlaptev1991_ctrl.lavray.ui.design.components.LavSwitchRow
import io.github.anatolijlaptev1991_ctrl.lavray.ui.design.components.LavTextField
import io.github.anatolijlaptev1991_ctrl.lavray.vpn.AppMode
import io.github.anatolijlaptev1991_ctrl.lavray.vpn.AppSelection
import kotlinx.coroutines.launch

/**
 * The app picker of the per-app tunnel. Apps load off the main thread, icons load only for rows that scroll into
 * view, and a tap saves the choice at once (in order, newest wins, on the app's scope). Chosen packages that are not
 * installed are listed too, so they can be removed. The choice applies the next time the tunnel is built; while a
 * tunnel is up the shell offers the reconnect.
 */
@Composable
fun AppPickerScreen() {
    val env = LocalEnv.current
    val context = LocalContext.current
    var apps by remember { mutableStateOf<List<AppEntry>?>(null) }
    var selection by remember { mutableStateOf(AppSelection()) }
    var ready by remember { mutableStateOf(false) }
    var query by remember { mutableStateOf("") }
    var showSystem by remember { mutableStateOf(false) }

    // The icon cache is only worth its memory while the list is on screen.
    DisposableEffect(Unit) { onDispose { AppIcons.clear() } }
    LaunchedEffect(Unit) {
        // A failed read leaves the saved choice alone: nothing is written until it was read.
        env.appSelection.current()?.let { selection = it; ready = true }
        apps = AppCatalog.load(context.packageManager, context.packageName)
    }
    // A tap is saved at once, on the app's scope: leaving the picker right after it must not lose it. The saver writes
    // in order and the newest choice wins, so a burst of taps is not a burst of lost updates.
    val save = { next: AppSelection ->
        selection = next
        // submit() records the choice here, on the tapping thread, so the order of taps is the order that is saved.
        val saved = env.appSelection.submit(next, env.scope)
        env.scope.launch {
            val result = saved.await()
            if (result is AppSelectionSaver.Result.Failed) {
                // The saver has already re-read what the service has, under its own lock.
                result.stored?.let { selection = it }
                env.toast.show(env.appContext.getString(R.string.apps_saveFailed))
            }
        }
        Unit
    }
    val chosen = selection.packages
    val list = remember(apps, query, showSystem, chosen) {
        apps?.let { AppFilter.visible(AppFilter.withMissing(it, chosen), query, showSystem, chosen) }
    }

    Column(Modifier.fillMaxSize()) {
        Column(Modifier.padding(horizontal = 16.dp, vertical = 8.dp)) {
            LavText(
                stringResource(if (selection.mode == AppMode.Except) R.string.apps_except_d else R.string.apps_only_d),
                style = Lav.type.small, color = Lav.colors.textSecondary,
            )
            LavTextField(
                query, { query = it }, Modifier.padding(top = 8.dp), placeholder = stringResource(R.string.apps_search), leading = LavIcon.Search,
            )
            LavSwitchRow(stringResource(R.string.apps_system), showSystem, { showSystem = it })
            LavText(stringResource(R.string.apps_count, chosen.size.toString()), style = Lav.type.caption, color = Lav.colors.textTertiary)
            SkippedAppsNote(selection, Modifier)
        }
        val shown = list
        when {
            shown == null -> LavSkeletonRows(6, stringResource(R.string.lazy_loading), Modifier.padding(horizontal = 16.dp))
            shown.isEmpty() -> LavEmptyState(stringResource(R.string.apps_none), icon = LavIcon.Layers)
            else -> LazyColumn(Modifier.fillMaxSize().padding(horizontal = 8.dp)) {
                items(shown, key = { it.packageName }, contentType = { "app" }) { app ->
                    val on = app.packageName in chosen
                    LavListRow(
                        title = app.label,
                        subtitle = if (app.missing) stringResource(R.string.apps_missing) else app.packageName,
                        selected = on,
                        modifier = Modifier.fillMaxWidth(),
                        leading = { AppIcon(app.packageName) },
                        trailing = { if (on) LavIconView(LavIcon.Check, Lav.colors.accent, size = 22.dp) },
                        onClick = { if (ready) save(selection.toggled(app.packageName)) },
                    )
                }
            }
        }
    }
}

@Composable
private fun AppIcon(packageName: String) {
    val context = LocalContext.current
    var bitmap by remember(packageName) { mutableStateOf<ImageBitmap?>(AppIcons.cached(packageName)) }
    LaunchedEffect(packageName) {
        if (bitmap == null) bitmap = AppIcons.load(context.packageManager, packageName)
    }
    val shape = RoundedCornerShape(8.dp)
    val shown = bitmap
    if (shown != null) {
        Image(shown, contentDescription = null, modifier = Modifier.size(40.dp))
    } else {
        Box(Modifier.size(40.dp).background(Lav.colors.bgSurface2, shape))
    }
}

