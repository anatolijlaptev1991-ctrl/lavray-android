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
import kotlinx.coroutines.delay

/**
 * The app picker of the per-app tunnel. Apps load off the main thread, icons load only for rows that scroll into
 * view, and a tap saves the choice (several taps in a row become one save). The choice applies the next time the
 * tunnel is built.
 */
@Composable
fun AppPickerScreen() {
    val env = LocalEnv.current
    val context = LocalContext.current
    var apps by remember { mutableStateOf<List<AppEntry>?>(null) }
    var selection by remember { mutableStateOf(AppSelection()) }
    var ready by remember { mutableStateOf(false) }
    var dirty by remember { mutableStateOf(false) }
    var query by remember { mutableStateOf("") }
    var showSystem by remember { mutableStateOf(false) }

    LaunchedEffect(Unit) {
        // A failed read leaves the saved choice alone: nothing is written until it was read.
        runCatching { env.controller.appSelection() }.getOrNull()?.let { selection = it; ready = true }
        apps = AppCatalog.load(context.packageManager, context.packageName)
    }
    LaunchedEffect(selection, dirty) {
        if (!ready || !dirty) return@LaunchedEffect
        delay(SAVE_DEBOUNCE_MS)
        runCatching { env.controller.setAppSelection(selection) }
    }
    val chosen = selection.packages
    val list = remember(apps, query, showSystem, chosen) { apps?.let { AppFilter.visible(it, query, showSystem, chosen) } }

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
        }
        val shown = list
        when {
            shown == null -> LavSkeletonRows(6, stringResource(R.string.lazy_loading), Modifier.padding(horizontal = 16.dp))
            shown.isEmpty() -> LavEmptyState(stringResource(R.string.apps_none), icon = LavIcon.Layers)
            else -> LazyColumn(Modifier.fillMaxSize().padding(horizontal = 8.dp)) {
                items(shown, key = { it.packageName }, contentType = { "app" }) { app ->
                    val on = app.packageName in chosen
                    LavListRow(
                        title = app.label, subtitle = app.packageName, selected = on,
                        modifier = Modifier.fillMaxWidth(),
                        leading = { AppIcon(app.packageName) },
                        trailing = { if (on) LavIconView(LavIcon.Check, Lav.colors.accent, size = 22.dp) },
                        onClick = {
                            if (ready) {
                                selection = selection.toggled(app.packageName)
                                dirty = true
                            }
                        },
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

private const val SAVE_DEBOUNCE_MS = 250L
