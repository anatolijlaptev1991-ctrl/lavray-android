// SPDX-License-Identifier: GPL-3.0-only
package io.github.anatolijlaptev1991_ctrl.lavray.ui.screens.settings

import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Modifier
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.unit.dp
import io.github.anatolijlaptev1991_ctrl.lavray.R
import io.github.anatolijlaptev1991_ctrl.lavray.ui.design.components.ButtonVariant
import io.github.anatolijlaptev1991_ctrl.lavray.ui.design.components.LavBanner
import io.github.anatolijlaptev1991_ctrl.lavray.ui.design.components.LavButton
import io.github.anatolijlaptev1991_ctrl.lavray.ui.design.components.LavCard
import io.github.anatolijlaptev1991_ctrl.lavray.ui.design.components.LavChip
import io.github.anatolijlaptev1991_ctrl.lavray.ui.design.components.LavConfirmDialog
import io.github.anatolijlaptev1991_ctrl.lavray.ui.design.components.LavSectionLabel
import io.github.anatolijlaptev1991_ctrl.lavray.ui.design.components.LavSwitchRow
import io.github.anatolijlaptev1991_ctrl.lavray.ui.design.components.StatusTone

/** Connection checks, multiplexing and certificate verification; the desktop's core section without what a phone cannot use. */
@Composable
fun AdvancedSection(settings: SettingsState, model: SettingsModel) {
    LavSectionLabel(stringResource(R.string.sec_core))
    LavCard(Modifier.fillMaxWidth()) {
        Column {
            if (settings.has(SettingKeys.HEALTH_CHECK)) {
                LavSwitchRow(
                    stringResource(R.string.set_healthCheck), settings.bool(SettingKeys.HEALTH_CHECK, true),
                    { model.setBool(SettingKeys.HEALTH_CHECK, it) }, Modifier.padding(horizontal = 16.dp),
                    hint = stringResource(R.string.set_healthCheck_d),
                )
            }
            if (settings.has(SettingKeys.MUX_PROTOCOL)) MuxBlock(settings, model)
            if (settings.has(SettingKeys.INSECURE_TLS)) InsecureTlsRow(settings, model)
            ResetRow(model)
        }
    }
}

@Composable
private fun MuxBlock(settings: SettingsState, model: SettingsModel) {
    val protocol = settings.string(SettingKeys.MUX_PROTOCOL, MUX_PROTOCOLS.first())
    ChoiceBlock(stringResource(R.string.mux_protocol)) {
        MUX_PROTOCOLS.forEach { p -> LavChip(p, protocol == p, { model.setString(SettingKeys.MUX_PROTOCOL, p) }) }
    }
    if (settings.has(SettingKeys.MUX_CONCURRENCY)) {
        NumberSettingRow(
            stringResource(R.string.mux_concurrency), stringResource(R.string.mux_concurrency_d), settings.int(SettingKeys.MUX_CONCURRENCY, -1),
            SettingsMapper::parseConcurrency, stringResource(R.string.mux_concurrencyErr),
        ) { model.setInt(SettingKeys.MUX_CONCURRENCY, it) }
    }
    if (settings.has(SettingKeys.MUX_DEFAULT_ON)) {
        LavSwitchRow(
            stringResource(R.string.mux_default_on), settings.bool(SettingKeys.MUX_DEFAULT_ON), { model.setBool(SettingKeys.MUX_DEFAULT_ON, it) },
            Modifier.padding(horizontal = 16.dp),
        )
    }
}

/** Turning certificate checks off asks first, with the desktop's wording; turning them back on does not. */
@Composable
private fun InsecureTlsRow(settings: SettingsState, model: SettingsModel) {
    val on = settings.bool(SettingKeys.INSECURE_TLS)
    var confirm by remember { mutableStateOf(false) }
    LavSwitchRow(
        stringResource(R.string.set_insecure), on, { wanted -> if (wanted) confirm = true else model.setBool(SettingKeys.INSECURE_TLS, false) },
        Modifier.padding(horizontal = 16.dp), hint = stringResource(R.string.set_insecure_d),
    )
    if (on) LavBanner(StatusTone.Error, stringResource(R.string.set_insecure_on), Modifier.padding(horizontal = 16.dp, vertical = 8.dp))
    if (confirm) {
        LavConfirmDialog(
            title = stringResource(R.string.set_insecure_confirm_t), text = stringResource(R.string.set_insecure_confirm_d),
            confirmLabel = stringResource(R.string.set_insecure_confirm_ok), dismissLabel = stringResource(R.string.common_cancel),
            onConfirm = { confirm = false; model.setBool(SettingKeys.INSECURE_TLS, true) }, onDismiss = { confirm = false }, danger = true,
        )
    }
}

@Composable
private fun ResetRow(model: SettingsModel) {
    var ask by remember { mutableStateOf(false) }
    Column(Modifier.padding(16.dp)) {
        LavButton(stringResource(R.string.set_reset), { ask = true }, variant = ButtonVariant.Ghost)
    }
    if (ask) {
        LavConfirmDialog(
            title = stringResource(R.string.set_reset_t), text = stringResource(R.string.set_reset_d),
            confirmLabel = stringResource(R.string.set_reset), dismissLabel = stringResource(R.string.common_cancel),
            onConfirm = { ask = false; model.resetAll() }, onDismiss = { ask = false },
        )
    }
}
