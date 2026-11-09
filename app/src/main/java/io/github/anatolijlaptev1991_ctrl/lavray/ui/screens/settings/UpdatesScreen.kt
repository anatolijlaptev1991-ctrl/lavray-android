// SPDX-License-Identifier: GPL-3.0-only
package io.github.anatolijlaptev1991_ctrl.lavray.ui.screens.settings

import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.semantics.semantics
import androidx.compose.ui.unit.dp
import io.github.anatolijlaptev1991_ctrl.lavray.R
import io.github.anatolijlaptev1991_ctrl.lavray.ui.LocalEnv
import io.github.anatolijlaptev1991_ctrl.lavray.ui.design.Lav
import io.github.anatolijlaptev1991_ctrl.lavray.ui.design.LavIcon
import io.github.anatolijlaptev1991_ctrl.lavray.ui.design.LavText
import io.github.anatolijlaptev1991_ctrl.lavray.ui.design.components.ButtonVariant
import io.github.anatolijlaptev1991_ctrl.lavray.ui.design.components.LavButton
import io.github.anatolijlaptev1991_ctrl.lavray.ui.design.components.LavCard
import io.github.anatolijlaptev1991_ctrl.lavray.ui.design.components.LavScreenColumn

/**
 * Updates. The desktop updater (signed manifests, in-app download and install, channels) is not ported: a phone
 * installs apps through the system. This screen shows what is installed and opens the releases page in the browser.
 * Nothing here contacts the network, and nothing checks for updates by itself.
 */
@Composable
fun UpdatesScreen() {
    val env = LocalEnv.current
    val context = LocalContext.current
    val core by rememberCoreVersion()
    val noBrowser = stringResource(R.string.upd_no_browser)
    LavScreenColumn {
        LavCard(Modifier.fillMaxWidth()) {
            Column(Modifier.padding(16.dp), verticalArrangement = Arrangement.spacedBy(12.dp)) {
                VersionLine(stringResource(R.string.set_appVersion), AppInfo.versionName(context))
                VersionLine(stringResource(R.string.set_coreVersion), core ?: "—")
            }
        }
        LavCard(Modifier.fillMaxWidth()) {
            Column(Modifier.padding(16.dp), verticalArrangement = Arrangement.spacedBy(12.dp)) {
                LavText(stringResource(R.string.upd_manual_t), style = Lav.type.h2)
                LavText(stringResource(R.string.upd_manual_d), style = Lav.type.small, color = Lav.colors.textSecondary)
                LavButton(stringResource(R.string.upd_open_releases), {
                    if (!openUrl(context, ProjectLinks.RELEASES)) env.toast.show(noBrowser)
                }, variant = ButtonVariant.Primary, icon = LavIcon.Link)
                LavText(ProjectLinks.RELEASES, style = Lav.type.mono, color = Lav.colors.textTertiary)
            }
        }
    }
}

@Composable
internal fun VersionLine(label: String, value: String) {
    Column(Modifier.semantics(mergeDescendants = true) {}) {
        LavText(label, style = Lav.type.caption, color = Lav.colors.textTertiary)
        LavText(value, style = Lav.type.mono)
    }
}
