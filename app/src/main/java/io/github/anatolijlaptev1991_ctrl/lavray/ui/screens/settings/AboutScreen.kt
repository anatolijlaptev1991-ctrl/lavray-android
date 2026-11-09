// SPDX-License-Identifier: GPL-3.0-only
package io.github.anatolijlaptev1991_ctrl.lavray.ui.screens.settings

import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.unit.dp
import io.github.anatolijlaptev1991_ctrl.lavray.R
import io.github.anatolijlaptev1991_ctrl.lavray.ui.LocalEnv
import io.github.anatolijlaptev1991_ctrl.lavray.ui.design.Lav
import io.github.anatolijlaptev1991_ctrl.lavray.ui.design.LavIcon
import io.github.anatolijlaptev1991_ctrl.lavray.ui.design.LavIconView
import io.github.anatolijlaptev1991_ctrl.lavray.ui.design.LavText
import io.github.anatolijlaptev1991_ctrl.lavray.ui.design.MarkState
import io.github.anatolijlaptev1991_ctrl.lavray.ui.design.ReflectionMark
import io.github.anatolijlaptev1991_ctrl.lavray.ui.design.components.LavCard
import io.github.anatolijlaptev1991_ctrl.lavray.ui.design.components.LavListRow
import io.github.anatolijlaptev1991_ctrl.lavray.ui.design.components.LavScreenColumn
import io.github.anatolijlaptev1991_ctrl.lavray.ui.design.components.LavSectionLabel
import io.github.anatolijlaptev1991_ctrl.lavray.ui.nav.LocalNavigator

/** About: the mark, versions, the GPL-3.0 notice and links to the source. Links open in the browser on a tap. */
@Composable
fun AboutScreen() {
    val env = LocalEnv.current
    val nav = LocalNavigator.current
    val context = LocalContext.current
    val core by rememberCoreVersion()
    val noBrowser = stringResource(R.string.upd_no_browser)
    val open = { url: String -> if (!openUrl(context, url)) env.toast.show(noBrowser) }
    LavScreenColumn {
        Column(Modifier.fillMaxWidth(), horizontalAlignment = Alignment.CenterHorizontally, verticalArrangement = Arrangement.spacedBy(8.dp)) {
            ReflectionMark(MarkState.On, size = 72.dp)
            LavText(stringResource(R.string.app_name), style = Lav.type.display)
            LavText(stringResource(R.string.about_desc), style = Lav.type.body, color = Lav.colors.textSecondary)
        }
        LavCard(Modifier.fillMaxWidth()) {
            Column(Modifier.padding(16.dp), verticalArrangement = Arrangement.spacedBy(12.dp)) {
                VersionLine(stringResource(R.string.set_appVersion), AppInfo.versionName(context))
                VersionLine(stringResource(R.string.set_coreVersion), core ?: "—")
            }
        }
        LavSectionLabel(stringResource(R.string.about_license))
        LavCard(Modifier.fillMaxWidth()) {
            Column(Modifier.padding(16.dp), verticalArrangement = Arrangement.spacedBy(8.dp)) {
                LavText(stringResource(R.string.lic_app), style = Lav.type.bodyStrong)
                LavText(stringResource(R.string.about_gpl_d), style = Lav.type.small, color = Lav.colors.textSecondary)
            }
        }
        LavCard(Modifier.fillMaxWidth()) {
            Column {
                Link(R.string.about_repo, LavIcon.Link) { open(ProjectLinks.APP_REPO) }
                Link(R.string.about_core_repo, LavIcon.Link) { open(ProjectLinks.CORE_REPO) }
                Link(R.string.about_bug, LavIcon.Alert) { open(ProjectLinks.APP_ISSUES) }
                Link(R.string.about_gpl_text, LavIcon.File) { open(ProjectLinks.GPL_TEXT) }
                Link(R.string.lic_title, LavIcon.Layers, chevron = true) { nav.push("licenses") }
            }
        }
    }
}

@Composable
private fun Link(title: Int, icon: LavIcon, chevron: Boolean = false, onClick: () -> Unit) {
    LavListRow(
        title = stringResource(title),
        leading = { LavIconView(icon, Lav.colors.textSecondary, size = 20.dp) },
        trailing = { LavIconView(if (chevron) LavIcon.ChevronRight else LavIcon.Share, Lav.colors.textTertiary, size = 18.dp) },
        onClick = onClick,
    )
}
