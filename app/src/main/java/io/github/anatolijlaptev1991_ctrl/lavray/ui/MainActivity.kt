// SPDX-License-Identifier: GPL-3.0-only
package io.github.anatolijlaptev1991_ctrl.lavray.ui

import android.Manifest
import android.content.pm.PackageManager
import android.net.VpnService
import android.os.Build
import android.os.Bundle
import androidx.activity.ComponentActivity
import androidx.activity.compose.rememberLauncherForActivityResult
import androidx.activity.compose.setContent
import androidx.activity.result.contract.ActivityResultContracts
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.safeDrawingPadding
import androidx.compose.foundation.text.BasicText
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.collectAsState
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Modifier
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.unit.dp
import io.github.anatolijlaptev1991_ctrl.lavray.R
import io.github.anatolijlaptev1991_ctrl.lavray.ipc.VpnState

/**
 * Placeholder screen to exercise the VPN layer before the real UI exists: consent,
 * connect, disconnect, status, and the auto-connect switch. It binds to the :vpn
 * process in onStart (asynchronously) and unbinds in onStop, so closing the screen
 * lets an idle :vpn process exit.
 */
class MainActivity : ComponentActivity() {
    private lateinit var controller: VpnController

    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)
        controller = VpnController(this)
        setContent { MainScreen(controller, onPrepare = { VpnService.prepare(this) }) }
    }

    override fun onStart() {
        super.onStart()
        controller.bind()
    }

    override fun onStop() {
        controller.unbind()
        super.onStop()
    }

    override fun onDestroy() {
        controller.close()
        super.onDestroy()
    }
}

@Composable
private fun MainScreen(controller: VpnController, onPrepare: () -> android.content.Intent?) {
    val status by controller.status.collectAsState()
    var autoConnect by remember { mutableStateOf(false) }
    LaunchedEffect(Unit) { runCatching { autoConnect = controller.isAutoConnectEnabled() } }

    val consent = rememberLauncherForActivityResult(ActivityResultContracts.StartActivityForResult()) { result ->
        if (result.resultCode == android.app.Activity.RESULT_OK) controller.connect()
    }
    val notifications = rememberLauncherForActivityResult(ActivityResultContracts.RequestPermission()) { }

    fun requestConnect() {
        if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.TIRAMISU) notifications.launch(Manifest.permission.POST_NOTIFICATIONS)
        val intent = onPrepare()
        if (intent == null) controller.connect() else consent.launch(intent)
    }

    Column(
        modifier = Modifier.safeDrawingPadding().padding(24.dp),
        verticalArrangement = Arrangement.spacedBy(16.dp),
    ) {
        BasicText(stringResource(R.string.main_status, stateLabel(status.state)))
        if (status.error != null) BasicText(status.error.orEmpty())
        BasicText(stringResource(R.string.main_connect), Modifier.clickable { requestConnect() }.padding(8.dp))
        BasicText(stringResource(R.string.main_disconnect), Modifier.clickable { controller.disconnect() }.padding(8.dp))
        Row(Modifier.fillMaxWidth().clickable {
            autoConnect = !autoConnect
            controller.setAutoConnectEnabled(autoConnect)
        }.padding(8.dp)) {
            BasicText((if (autoConnect) "[x] " else "[ ] ") + stringResource(R.string.main_auto_connect))
        }
    }
}

@Composable
private fun stateLabel(state: VpnState): String = stringResource(
    when (state) {
        VpnState.IDLE -> R.string.state_idle
        VpnState.CONNECTING -> R.string.state_connecting
        VpnState.CONNECTED -> R.string.state_connected
        VpnState.DISCONNECTING -> R.string.state_disconnecting
        VpnState.WAITING_NETWORK -> R.string.state_waiting_network
        VpnState.NEEDS_PERMISSION -> R.string.state_needs_permission
        VpnState.ERROR -> R.string.state_error
    },
)
