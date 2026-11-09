// SPDX-License-Identifier: GPL-3.0-only
package io.github.anatolijlaptev1991_ctrl.lavray.ui.screens.connection

import android.Manifest
import android.app.Activity
import android.content.pm.PackageManager
import android.net.VpnService
import android.os.Build
import androidx.activity.compose.rememberLauncherForActivityResult
import androidx.activity.result.contract.ActivityResultContracts
import androidx.compose.runtime.Composable
import androidx.compose.runtime.remember
import androidx.compose.ui.platform.LocalContext
import io.github.anatolijlaptev1991_ctrl.lavray.ui.LocalEnv

/**
 * Returns "connect to this server" that first runs the two system prompts a VPN needs, one after the other:
 * the notification permission on API 33+ (the foreground notification is how the system shows the VPN is
 * on; connecting works without it, so a refusal does not stop the flow) and the VPN consent dialog.
 * Id <= 0 means the last used server.
 */
@Composable
fun rememberVpnConnect(): (Long) -> Unit {
    val env = LocalEnv.current
    val context = LocalContext.current
    val pending = remember { LongArray(1) }

    val consent = rememberLauncherForActivityResult(ActivityResultContracts.StartActivityForResult()) { result ->
        if (result.resultCode == Activity.RESULT_OK) env.vpn.connect(pending[0])
    }
    fun askConsent() {
        val intent = VpnService.prepare(context)
        if (intent == null) env.vpn.connect(pending[0]) else consent.launch(intent)
    }
    val notifications = rememberLauncherForActivityResult(ActivityResultContracts.RequestPermission()) { askConsent() }

    return remember(env, context) {
        { id: Long ->
            pending[0] = id
            val needsAsk = Build.VERSION.SDK_INT >= Build.VERSION_CODES.TIRAMISU &&
                context.checkSelfPermission(Manifest.permission.POST_NOTIFICATIONS) != PackageManager.PERMISSION_GRANTED
            if (needsAsk) notifications.launch(Manifest.permission.POST_NOTIFICATIONS) else askConsent()
        }
    }
}
