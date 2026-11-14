// SPDX-License-Identifier: GPL-3.0-only
package io.github.anatolijlaptev1991_ctrl.lavray.ui.screens.settings

import androidx.annotation.StringRes
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Modifier
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.unit.dp
import io.github.anatolijlaptev1991_ctrl.lavray.R
import io.github.anatolijlaptev1991_ctrl.lavray.ui.LocalEnv
import io.github.anatolijlaptev1991_ctrl.lavray.ui.design.Lav
import io.github.anatolijlaptev1991_ctrl.lavray.ui.design.LavIcon
import io.github.anatolijlaptev1991_ctrl.lavray.ui.design.LavIconView
import io.github.anatolijlaptev1991_ctrl.lavray.ui.design.LavText
import io.github.anatolijlaptev1991_ctrl.lavray.ui.design.components.LavCard
import io.github.anatolijlaptev1991_ctrl.lavray.ui.design.components.LavListRow
import io.github.anatolijlaptev1991_ctrl.lavray.ui.nav.LocalNavigator
import io.github.anatolijlaptev1991_ctrl.lavray.vpn.AppRoute
import io.github.anatolijlaptev1991_ctrl.lavray.vpn.AppSelection

/** How a list is named and where its picker is. */
internal object AppRouteUi {
    @StringRes
    fun title(route: AppRoute): Int = when (route) {
        AppRoute.ViaVpn -> R.string.apps_vpn_title
        AppRoute.Direct -> R.string.apps_direct_title
    }

    @StringRes
    fun description(route: AppRoute): Int = when (route) {
        AppRoute.ViaVpn -> R.string.apps_vpn_d
        AppRoute.Direct -> R.string.apps_direct_d
    }

    /** The subtitle of an app that is in [route], shown in every other picker. */
    @StringRes
    fun inList(route: AppRoute): Int = when (route) {
        AppRoute.ViaVpn -> R.string.apps_in_vpn
        AppRoute.Direct -> R.string.apps_in_direct
    }

    fun screen(route: AppRoute): String = when (route) {
        AppRoute.ViaVpn -> SCREEN_VPN
        AppRoute.Direct -> SCREEN_DIRECT
    }

    fun icon(route: AppRoute): LavIcon = when (route) {
        AppRoute.ViaVpn -> LavIcon.Shield
        AppRoute.Direct -> LavIcon.Globe
    }

    const val SCREEN_VPN = "apps_vpn"
    const val SCREEN_DIRECT = "apps_direct"
}

/** One row per list, opening its picker; the count is left out while the choice is not known. */
@Composable
internal fun AppRouteRows(selection: AppSelection?, modifier: Modifier = Modifier.padding(horizontal = 4.dp)) {
    val nav = LocalNavigator.current
    for (route in AppRoute.entries) {
        LavListRow(
            title = stringResource(AppRouteUi.title(route)),
            subtitle = selection?.let { stringResource(R.string.apps_count, it.listOf(route).size.toString()) },
            leading = { LavIconView(AppRouteUi.icon(route), Lav.colors.textSecondary, size = 20.dp) },
            trailing = { LavIconView(LavIcon.ChevronRight, Lav.colors.textTertiary, size = 18.dp) },
            onClick = { nav.push(AppRouteUi.screen(route)) },
            modifier = modifier,
        )
    }
}

/** The two lists on the routing screen, where a person looks for what goes where. */
@Composable
fun AppRoutesCard(modifier: Modifier = Modifier.fillMaxWidth()) {
    val env = LocalEnv.current
    var selection by remember { mutableStateOf<AppSelection?>(null) }
    LaunchedEffect(Unit) { selection = env.appSelection.current() }
    LavCard(modifier) {
        Column {
            AppRouteRows(selection)
            LavText(
                stringResource(R.string.rt_apps_hint), Modifier.padding(horizontal = 16.dp, vertical = 8.dp),
                style = Lav.type.caption, color = Lav.colors.textTertiary,
            )
        }
    }
}
