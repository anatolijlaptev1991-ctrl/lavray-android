// SPDX-License-Identifier: GPL-3.0-only
package io.github.anatolijlaptev1991_ctrl.lavray.ui.screens.rules

import androidx.compose.runtime.Composable
import androidx.compose.ui.res.stringResource
import io.github.anatolijlaptev1991_ctrl.lavray.api.gen.RoutingPreset

/** Name of a preset for display: a brand as it is, a built-in by its dictionary name, a custom one as typed. */
@Composable
fun presetTitle(p: RoutingPreset): String {
    val text = PresetCatalog.textOf(p.id)
    return when {
        text?.brand != null -> text.brand
        text?.nameRes != null -> stringResource(text.nameRes)
        else -> PresetCatalog.fallbackName(p)
    }
}
