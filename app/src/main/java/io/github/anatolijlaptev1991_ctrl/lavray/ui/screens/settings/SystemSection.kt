// SPDX-License-Identifier: GPL-3.0-only
package io.github.anatolijlaptev1991_ctrl.lavray.ui.screens.settings

import android.content.ActivityNotFoundException
import android.content.Context
import android.content.Intent
import android.os.PowerManager
import android.provider.Settings
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.unit.dp
import androidx.lifecycle.compose.LifecycleResumeEffect
import io.github.anatolijlaptev1991_ctrl.lavray.R
import io.github.anatolijlaptev1991_ctrl.lavray.ui.design.Lav
import io.github.anatolijlaptev1991_ctrl.lavray.ui.design.LavIcon
import io.github.anatolijlaptev1991_ctrl.lavray.ui.design.LavText
import io.github.anatolijlaptev1991_ctrl.lavray.ui.design.components.LavButton
import io.github.anatolijlaptev1991_ctrl.lavray.ui.design.components.LavCard
import io.github.anatolijlaptev1991_ctrl.lavray.ui.design.components.LavSectionLabel

/**
 * Always-on VPN and battery optimization. Both are Android's own switches: LAVRay can open the right system page but
 * cannot read or change the first one, and the second one only in its standard form (brand-specific background
 * limits are invisible to apps). The texts say exactly that.
 */
@Composable
fun SystemSection() {
    val context = LocalContext.current
    LavSectionLabel(stringResource(R.string.sec_system))
    LavCard(Modifier.fillMaxWidth()) {
        Column(Modifier.padding(16.dp), verticalArrangement = Arrangement.spacedBy(16.dp)) {
            Explained(
                title = stringResource(R.string.set_alwayson_t),
                paragraphs = listOf(R.string.set_alwayson_d, R.string.set_lockdown_d, R.string.set_alwayson_note),
                button = stringResource(R.string.set_alwayson_open),
            ) { open(context, Settings.ACTION_VPN_SETTINGS) }
            BatteryBlock(context)
        }
    }
}

@Composable
private fun BatteryBlock(context: Context) {
    var exempt by remember { mutableStateOf(isExempt(context)) }
    LifecycleResumeEffect(Unit) {
        exempt = isExempt(context)
        onPauseOrDispose { }
    }
    Explained(
        title = stringResource(R.string.set_battery_t),
        paragraphs = listOf(if (exempt) R.string.set_battery_off else R.string.set_battery_on, R.string.set_battery_d),
        button = stringResource(R.string.set_battery_open),
    ) { open(context, Settings.ACTION_IGNORE_BATTERY_OPTIMIZATION_SETTINGS) }
}

@Composable
private fun Explained(title: String, paragraphs: List<Int>, button: String, onOpen: () -> Unit) {
    Column(verticalArrangement = Arrangement.spacedBy(6.dp)) {
        LavText(title, style = Lav.type.bodyStrong)
        paragraphs.forEach { LavText(stringResource(it), style = Lav.type.small, color = Lav.colors.textSecondary) }
        LavButton(button, onOpen, icon = LavIcon.Sliders)
    }
}

private fun isExempt(context: Context): Boolean =
    context.getSystemService(PowerManager::class.java)?.isIgnoringBatteryOptimizations(context.packageName) == true

/** Opens a system settings page; a device that lacks it falls back to the main settings list. */
private fun open(context: Context, action: String) {
    try {
        context.startActivity(Intent(action).addFlags(Intent.FLAG_ACTIVITY_NEW_TASK))
    } catch (_: ActivityNotFoundException) {
        try {
            context.startActivity(Intent(Settings.ACTION_SETTINGS).addFlags(Intent.FLAG_ACTIVITY_NEW_TASK))
        } catch (_: ActivityNotFoundException) {
            // No settings app at all: nothing more to offer.
        }
    }
}
