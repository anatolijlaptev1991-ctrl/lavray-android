// SPDX-License-Identifier: GPL-3.0-only
package io.github.anatolijlaptev1991_ctrl.lavray.ui.screens.settings

import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.setValue
import androidx.compose.ui.Modifier
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.unit.dp
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import io.github.anatolijlaptev1991_ctrl.lavray.R
import io.github.anatolijlaptev1991_ctrl.lavray.ui.LocalEnv
import io.github.anatolijlaptev1991_ctrl.lavray.ui.design.Lav
import io.github.anatolijlaptev1991_ctrl.lavray.ui.design.LavIcon
import io.github.anatolijlaptev1991_ctrl.lavray.ui.design.LavIconView
import io.github.anatolijlaptev1991_ctrl.lavray.ui.design.LavText
import io.github.anatolijlaptev1991_ctrl.lavray.ui.design.components.ButtonVariant
import io.github.anatolijlaptev1991_ctrl.lavray.ui.design.components.LavBanner
import io.github.anatolijlaptev1991_ctrl.lavray.ui.design.components.LavButton
import io.github.anatolijlaptev1991_ctrl.lavray.ui.design.components.LavCard
import io.github.anatolijlaptev1991_ctrl.lavray.ui.design.components.LavChip
import io.github.anatolijlaptev1991_ctrl.lavray.ui.design.components.LavListRow
import io.github.anatolijlaptev1991_ctrl.lavray.ui.design.components.LavSectionLabel
import io.github.anatolijlaptev1991_ctrl.lavray.ui.design.components.LavSwitchRow
import io.github.anatolijlaptev1991_ctrl.lavray.ui.design.components.StatusTone
import io.github.anatolijlaptev1991_ctrl.lavray.ui.nav.LocalNavigator
import io.github.anatolijlaptev1991_ctrl.lavray.ui.screens.connection.isLive
import io.github.anatolijlaptev1991_ctrl.lavray.ui.screens.connection.phaseOf
import io.github.anatolijlaptev1991_ctrl.lavray.vpn.AppMode
import io.github.anatolijlaptev1991_ctrl.lavray.vpn.AppSelection
import kotlinx.coroutines.launch

/** The tunnel: which apps use it, the MTU, IPv6, FakeIP and the addresses kept out of it. The stack is fixed to gVisor. */
@Composable
fun TunnelSection(settings: SettingsState, model: SettingsModel) {
    LavSectionLabel(stringResource(R.string.sec_tunnel))
    LavCard(Modifier.fillMaxWidth()) {
        Column {
            PerAppBlock()
            if (settings.has(SettingKeys.TUN_MTU)) {
                NumberSettingRow(
                    stringResource(R.string.tun_mtu), stringResource(R.string.tun_mtu_d), settings.int(SettingKeys.TUN_MTU, 1500),
                    SettingsMapper::parseMtu, stringResource(R.string.tun_mtuErr),
                ) { model.setInt(SettingKeys.TUN_MTU, it) }
            }
            if (settings.has(SettingKeys.TUN_IPV6)) {
                LavSwitchRow(
                    stringResource(R.string.tun_ipv6), settings.bool(SettingKeys.TUN_IPV6), { model.setBool(SettingKeys.TUN_IPV6, it) },
                    Modifier.padding(horizontal = 16.dp), hint = stringResource(R.string.tun_ipv6_d),
                )
            }
            if (settings.has(SettingKeys.TUN_FAKE_DNS)) {
                LavSwitchRow(
                    stringResource(R.string.dns_fakeip), settings.bool(SettingKeys.TUN_FAKE_DNS), { model.setBool(SettingKeys.TUN_FAKE_DNS, it) },
                    Modifier.padding(horizontal = 16.dp), hint = stringResource(R.string.dns_fakeip_d),
                )
            }
            if (settings.has(SettingKeys.TUN_EXCLUDE_ADDRESSES)) ExcludeAddresses(settings, model)
        }
    }
    if (settings.reconnectNeeded) ReconnectHint(onDismiss = model::dismissReconnect)
}

@Composable
private fun ExcludeAddresses(settings: SettingsState, model: SettingsModel) {
    val invalid = stringResource(R.string.tun_exclAddrErr)
    LinesSettingRow(
        stringResource(R.string.tun_exclAddr), stringResource(R.string.tun_exclHint), settings.string(SettingKeys.TUN_EXCLUDE_ADDRESSES),
        validate = { text -> SettingsMapper.firstInvalidAddress(text)?.let { "$invalid $it" } },
    ) { model.setString(SettingKeys.TUN_EXCLUDE_ADDRESSES, it) }
}

/** All apps / only chosen / all except chosen, with the picker one tap away. The choice lives in the :vpn process. */
@Composable
private fun PerAppBlock() {
    val env = LocalEnv.current
    val nav = LocalNavigator.current
    val scope = rememberCoroutineScope()
    var selection by remember { mutableStateOf<AppSelection?>(null) }
    var changed by remember { mutableStateOf(false) }
    val vpn by env.vpn.status.collectAsStateWithLifecycle()
    LaunchedEffect(Unit) { selection = runCatching { env.controller.appSelection() }.getOrNull() }
    val current = selection ?: AppSelection()
    val choose = { mode: AppMode ->
        // Until the saved choice was read nothing is written: a failed read must not replace it with the default.
        val next = current.with(mode)
        if (selection != null) {
            selection = next
            changed = true
            scope.launch { runCatching { env.controller.setAppSelection(next) } }
        }
        Unit
    }
    ChoiceBlock(stringResource(R.string.apps_title), stringResource(R.string.apps_hint)) {
        LavChip(stringResource(R.string.apps_all), current.mode == AppMode.All, { choose(AppMode.All) })
        LavChip(stringResource(R.string.apps_only), current.mode == AppMode.Only, { choose(AppMode.Only) })
        LavChip(stringResource(R.string.apps_except), current.mode == AppMode.Except, { choose(AppMode.Except) })
    }
    LavText(
        stringResource(
            when (current.mode) {
                AppMode.All -> R.string.apps_all_d
                AppMode.Only -> R.string.apps_only_d
                AppMode.Except -> R.string.apps_except_d
            },
        ),
        Modifier.padding(horizontal = 16.dp), style = Lav.type.caption, color = Lav.colors.textTertiary,
    )
    if (current.mode != AppMode.All) {
        LavListRow(
            title = stringResource(R.string.apps_choose),
            subtitle = stringResource(R.string.apps_count, current.packages.size.toString()),
            leading = { LavIconView(LavIcon.Layers, Lav.colors.textSecondary, size = 20.dp) },
            trailing = { LavIconView(LavIcon.ChevronRight, Lav.colors.textTertiary, size = 18.dp) },
            onClick = { nav.push("apps") },
            modifier = Modifier.padding(horizontal = 4.dp),
        )
    }
    if (current.mode == AppMode.Only && current.packages.isEmpty()) {
        LavBanner(StatusTone.Connecting, stringResource(R.string.apps_only_empty), Modifier.padding(horizontal = 16.dp, vertical = 8.dp))
    }
    if (changed && phaseOf(vpn.state).isLive) ReconnectHint(onDismiss = { changed = false })
}

/** Shown after a change that only the next tunnel picks up, with the button that rebuilds it now. */
@Composable
internal fun ReconnectHint(onDismiss: () -> Unit) {
    val env = LocalEnv.current
    val scope = rememberCoroutineScope()
    val live = phaseOf(env.vpn.status.collectAsStateWithLifecycle().value.state).isLive
    if (!live) return
    LavBanner(
        StatusTone.Neutral, stringResource(R.string.set_reconnect),
        Modifier.padding(horizontal = 16.dp, vertical = 8.dp),
        actions = {
            LavButton(stringResource(R.string.set_reconnect_now), {
                onDismiss()
                scope.launch { Reconnect.now(env) }
            }, variant = ButtonVariant.Primary)
        },
    )
}
