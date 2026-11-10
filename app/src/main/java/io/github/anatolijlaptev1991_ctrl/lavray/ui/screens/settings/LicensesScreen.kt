// SPDX-License-Identifier: GPL-3.0-only
package io.github.anatolijlaptev1991_ctrl.lavray.ui.screens.settings

import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.runtime.Composable
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.unit.dp
import io.github.anatolijlaptev1991_ctrl.lavray.R
import io.github.anatolijlaptev1991_ctrl.lavray.ui.LocalEnv
import io.github.anatolijlaptev1991_ctrl.lavray.ui.design.Lav
import io.github.anatolijlaptev1991_ctrl.lavray.ui.design.LavIcon
import io.github.anatolijlaptev1991_ctrl.lavray.ui.design.LavText
import io.github.anatolijlaptev1991_ctrl.lavray.ui.design.components.LavButton
import io.github.anatolijlaptev1991_ctrl.lavray.ui.design.components.LavCard
import io.github.anatolijlaptev1991_ctrl.lavray.ui.design.components.LavScreenColumn

/**
 * Licenses and components. The one screen where the engine's origin is named (the desktop key lic.core): the licence
 * of the core requires it, and every other screen speaks only of "the core".
 */
@Composable
fun LicensesScreen() {
    val env = LocalEnv.current
    val context = LocalContext.current
    val noBrowser = stringResource(R.string.upd_no_browser)
    LavScreenColumn {
        LavCard(Modifier.fillMaxWidth()) {
            Column(Modifier.padding(16.dp), verticalArrangement = Arrangement.spacedBy(14.dp)) {
                Component(R.string.lic_app)
                Component(R.string.lic_core)
                Component(R.string.lic_android_ui)
                Component(R.string.lic_zxing)
                Component(R.string.lic_go)
                Component(R.string.lic_geo, R.string.lic_geoData)
            }
        }
        LavText(stringResource(R.string.lic_source_d), style = Lav.type.small, color = Lav.colors.textSecondary)
        LavButton(stringResource(R.string.about_gpl_text), {
            if (!openUrl(context, ProjectLinks.GPL_TEXT)) env.toast.show(noBrowser)
        }, icon = LavIcon.File)
    }
}

@Composable
private fun Component(title: Int, note: Int? = null) {
    Column(verticalArrangement = Arrangement.spacedBy(2.dp)) {
        LavText(stringResource(title), style = Lav.type.body)
        if (note != null) LavText(stringResource(note), style = Lav.type.caption, color = Lav.colors.textTertiary)
    }
}
