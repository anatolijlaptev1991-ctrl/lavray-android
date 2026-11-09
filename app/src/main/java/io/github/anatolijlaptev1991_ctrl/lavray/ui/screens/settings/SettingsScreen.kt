// SPDX-License-Identifier: GPL-3.0-only
package io.github.anatolijlaptev1991_ctrl.lavray.ui.screens.settings

import android.app.Activity
import android.content.Context
import android.content.ContextWrapper
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.FlowRow
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.unit.dp
import io.github.anatolijlaptev1991_ctrl.lavray.R
import io.github.anatolijlaptev1991_ctrl.lavray.ui.LocalEnv
import io.github.anatolijlaptev1991_ctrl.lavray.ui.design.AccentChoice
import io.github.anatolijlaptev1991_ctrl.lavray.ui.design.Lav
import io.github.anatolijlaptev1991_ctrl.lavray.ui.design.LavIcon
import io.github.anatolijlaptev1991_ctrl.lavray.ui.design.LavIconView
import io.github.anatolijlaptev1991_ctrl.lavray.ui.design.LavText
import io.github.anatolijlaptev1991_ctrl.lavray.ui.design.ThemeMode
import io.github.anatolijlaptev1991_ctrl.lavray.ui.design.components.LavCard
import io.github.anatolijlaptev1991_ctrl.lavray.ui.design.components.LavChip
import io.github.anatolijlaptev1991_ctrl.lavray.ui.design.components.LavListRow
import io.github.anatolijlaptev1991_ctrl.lavray.ui.design.components.LavScreenColumn
import io.github.anatolijlaptev1991_ctrl.lavray.ui.design.components.LavSectionLabel
import io.github.anatolijlaptev1991_ctrl.lavray.ui.design.components.LavSwitchRow
import io.github.anatolijlaptev1991_ctrl.lavray.ui.i18n.AppLanguage
import io.github.anatolijlaptev1991_ctrl.lavray.ui.i18n.AppLocale
import io.github.anatolijlaptev1991_ctrl.lavray.ui.nav.LocalNavigator

/**
 * Settings. Owned by another agent from here on: this file starts with the parts the app shell needs to be usable
 * (look, language, start with the phone). Add further sections as `LavSectionLabel` + `LavCard` blocks below
 * [AppearanceSection]; keep each section in its own file next to this one.
 */
@Composable
fun SettingsScreen() {
    val nav = LocalNavigator.current
    LavScreenColumn {
        LavSectionLabel(stringResource(R.string.sec_appearance))
        LavCard(Modifier.fillMaxWidth()) { AppearanceSection() }
        LavSectionLabel(stringResource(R.string.sec_startup))
        LavCard(Modifier.fillMaxWidth()) { StartupSection() }
        LavCard(Modifier.fillMaxWidth()) {
            LavListRow(
                title = stringResource(R.string.bk_title),
                leading = { LavIconView(LavIcon.Download, Lav.colors.textSecondary, size = 20.dp) },
                trailing = { LavIconView(LavIcon.ChevronRight, Lav.colors.textTertiary, size = 18.dp) },
                onClick = { nav.push("backup") },
            )
        }
    }
}

/** Theme, accent and language, applied at once and remembered. */
@Composable
fun AppearanceSection() {
    val env = LocalEnv.current
    val context = LocalContext.current
    val s = env.settings
    LavListBlock(R.string.set_theme) {
        LavChip(stringResource(R.string.theme_system), s.theme == ThemeMode.SYSTEM, { env.update { it.copy(theme = ThemeMode.SYSTEM) } })
        LavChip(stringResource(R.string.theme_dark), s.theme == ThemeMode.DARK, { env.update { it.copy(theme = ThemeMode.DARK) } })
        LavChip(stringResource(R.string.theme_light), s.theme == ThemeMode.LIGHT, { env.update { it.copy(theme = ThemeMode.LIGHT) } })
    }
    LavListBlock(R.string.set_accent) {
        LavChip(stringResource(R.string.accent_auto), s.accent == AccentChoice.AUTO, { env.update { it.copy(accent = AccentChoice.AUTO) } })
        LavChip(stringResource(R.string.accent_amber), s.accent == AccentChoice.AMBER, { env.update { it.copy(accent = AccentChoice.AMBER) } })
        LavChip(stringResource(R.string.accent_sage), s.accent == AccentChoice.SAGE, { env.update { it.copy(accent = AccentChoice.SAGE) } })
        LavChip(stringResource(R.string.accent_graphite), s.accent == AccentChoice.GRAPHITE, { env.update { it.copy(accent = AccentChoice.GRAPHITE) } })
    }
    val shown = remember(s.language) { AppLocale.systemChoice(context) ?: s.language }
    LavListBlock(R.string.aria_lang) {
        AppLanguage.entries.forEach { lang ->
            val label = if (lang == AppLanguage.System) stringResource(R.string.theme_system) else lang.endonym
            LavChip(label, shown == lang, {
                env.update { it.copy(language = lang) }
                context.findActivity()?.let { activity -> AppLocale.apply(activity, lang) }
            })
        }
    }
}

@Composable
private fun LavListBlock(title: Int, chips: @Composable () -> Unit) {
    androidx.compose.foundation.layout.Column(Modifier.padding(16.dp), verticalArrangement = Arrangement.spacedBy(4.dp)) {
        LavText(stringResource(title), style = Lav.type.small, color = Lav.colors.textSecondary)
        FlowRow(horizontalArrangement = Arrangement.spacedBy(8.dp)) { chips() }
    }
}

/** "Connect when the phone starts": the switch lives in the :vpn process, so it is read and written through the controller. */
@Composable
fun StartupSection() {
    val env = LocalEnv.current
    var enabled by remember { mutableStateOf(false) }
    LaunchedEffect(Unit) { runCatching { enabled = env.controller.isAutoConnectEnabled() } }
    LavSwitchRow(
        label = stringResource(R.string.main_auto_connect), checked = enabled,
        onCheckedChange = { on -> enabled = on; env.controller.setAutoConnectEnabled(on) },
        modifier = Modifier.padding(horizontal = 16.dp),
    )
}

internal tailrec fun Context.findActivity(): Activity? = when (this) {
    is Activity -> this
    is ContextWrapper -> baseContext.findActivity()
    else -> null
}
