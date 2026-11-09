// SPDX-License-Identifier: GPL-3.0-only
package io.github.anatolijlaptev1991_ctrl.lavray.ui.nav

import androidx.annotation.StringRes
import androidx.compose.runtime.Composable
import androidx.compose.runtime.Immutable
import io.github.anatolijlaptev1991_ctrl.lavray.R
import io.github.anatolijlaptev1991_ctrl.lavray.ui.design.LavIcon
import io.github.anatolijlaptev1991_ctrl.lavray.ui.importer.ScanScreen
import io.github.anatolijlaptev1991_ctrl.lavray.ui.screens.backup.BackupScreen
import io.github.anatolijlaptev1991_ctrl.lavray.ui.screens.connection.ConnectionScreen
import io.github.anatolijlaptev1991_ctrl.lavray.ui.screens.diag.ChecksScreen
import io.github.anatolijlaptev1991_ctrl.lavray.ui.screens.diag.ConnectionsScreen
import io.github.anatolijlaptev1991_ctrl.lavray.ui.screens.diag.LogScreen
import io.github.anatolijlaptev1991_ctrl.lavray.ui.screens.diag.StatisticsScreen
import io.github.anatolijlaptev1991_ctrl.lavray.ui.screens.rules.DnsScreen
import io.github.anatolijlaptev1991_ctrl.lavray.ui.screens.rules.RoutingScreen
import io.github.anatolijlaptev1991_ctrl.lavray.ui.screens.servers.ServersScreen
import io.github.anatolijlaptev1991_ctrl.lavray.ui.screens.settings.AboutScreen
import io.github.anatolijlaptev1991_ctrl.lavray.ui.screens.settings.AppPickerScreen
import io.github.anatolijlaptev1991_ctrl.lavray.ui.screens.settings.LicensesScreen
import io.github.anatolijlaptev1991_ctrl.lavray.ui.screens.settings.SettingsScreen
import io.github.anatolijlaptev1991_ctrl.lavray.ui.screens.settings.UpdatesScreen
import io.github.anatolijlaptev1991_ctrl.lavray.ui.screens.subscriptions.SubscriptionsScreen

/** One destination: a tab of a section or a pushed full-screen page. [content] fills the area under the header. */
@Immutable
class ScreenSpec(
    val id: String,
    @StringRes val title: Int,
    @StringRes val subtitle: Int?,
    val content: @Composable () -> Unit,
)

/** A bottom-navigation section with its tabs (docs/DESIGN-SYSTEM.md section 7, ui/src/app/sections.ts). */
@Immutable
class SectionSpec(
    val id: String,
    @StringRes val label: Int,
    val icon: LavIcon,
    val tabs: List<ScreenSpec>,
) {
    val first: ScreenSpec get() = tabs.first()
    fun tab(id: String?): ScreenSpec = tabs.firstOrNull { it.id == id } ?: first
}

/**
 * THE registry. To add a screen: write its composable in one file, then add one [ScreenSpec] line here
 * (a tab inside a section's list, or in [pushed]). A tab that exists on the desktop but is still a stub
 * already has its own file under ui/screens/<area>/; replace that file's body and nothing else changes.
 */
object AppRegistry {
    val sections: List<SectionSpec> = listOf(
        SectionSpec(
            "connection", R.string.nav_connection, LavIcon.Zap,
            listOf(ScreenSpec("connection", R.string.nav_connection, R.string.scr_connection_sub) { ConnectionScreen() }),
        ),
        SectionSpec(
            "servers", R.string.nav_servers, LavIcon.Layers,
            listOf(
                ScreenSpec("profiles", R.string.tab_servers, R.string.scr_profiles_sub) { ServersScreen() },
                ScreenSpec("subscriptions", R.string.nav_subscriptions, R.string.scr_subs_sub) { SubscriptionsScreen() },
            ),
        ),
        SectionSpec(
            "rules", R.string.nav_rules, LavIcon.Route,
            listOf(
                ScreenSpec("routing", R.string.nav_routing, R.string.scr_routing_sub) { RoutingScreen() },
                ScreenSpec("dns", R.string.tab_dns, R.string.scr_dns_sub) { DnsScreen() },
            ),
        ),
        SectionSpec(
            "diag", R.string.nav_diag, LavIcon.Activity,
            listOf(
                ScreenSpec("checks", R.string.tab_checks, R.string.scr_checks_sub) { ChecksScreen() },
                ScreenSpec("conns", R.string.tab_conns, R.string.scr_conns_sub) { ConnectionsScreen() },
                ScreenSpec("log", R.string.nav_log, R.string.scr_log_sub) { LogScreen() },
            ),
        ),
        SectionSpec(
            "settings", R.string.nav_settings, LavIcon.Sliders,
            listOf(
                ScreenSpec("settings", R.string.tab_params, R.string.scr_settings_sub) { SettingsScreen() },
                ScreenSpec("updates", R.string.nav_updates, R.string.scr_updates_sub) { UpdatesScreen() },
            ),
        ),
    )

    /** Full-screen destinations opened with Navigator.push(id); Back pops them. */
    val pushed: List<ScreenSpec> = listOf(
        ScreenSpec("stats", R.string.stats_title, null) { StatisticsScreen() },
        ScreenSpec("backup", R.string.bk_title, null) { BackupScreen() },
        ScreenSpec("about", R.string.sec_about, null) { AboutScreen() },
        ScreenSpec("licenses", R.string.lic_title, null) { LicensesScreen() },
        ScreenSpec("apps", R.string.apps_title, null) { AppPickerScreen() },
        ScreenSpec("scan", R.string.imp_tab_qr, null) { ScanScreen() },
    )

    fun section(id: String): SectionSpec = sections.firstOrNull { it.id == id } ?: sections.first()
    fun pushedScreen(id: String): ScreenSpec? = pushed.firstOrNull { it.id == id }
}
