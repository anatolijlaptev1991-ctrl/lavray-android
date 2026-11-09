// SPDX-License-Identifier: GPL-3.0-only
package io.github.anatolijlaptev1991_ctrl.lavray.ui

import io.github.anatolijlaptev1991_ctrl.lavray.api.gen.SetSettingsRequest
import io.github.anatolijlaptev1991_ctrl.lavray.api.gen.SettingValue
import io.github.anatolijlaptev1991_ctrl.lavray.api.gen.settingsSetSettings
import io.github.anatolijlaptev1991_ctrl.lavray.ipc.VpnState
import io.github.anatolijlaptev1991_ctrl.lavray.ipc.VpnStatus
import io.github.anatolijlaptev1991_ctrl.lavray.ui.core.CoreSession
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.first
import kotlinx.coroutines.launch
import kotlinx.coroutines.withTimeoutOrNull

/**
 * Connect, switch and disconnect as the person means them. The consent dialog is not here: it needs an
 * activity, so [io.github.anatolijlaptev1991_ctrl.lavray.ui.screens.connection.rememberVpnConnect] asks for it first and then calls [connect].
 */
class VpnActions(
    private val controller: VpnController,
    private val session: CoreSession,
    private val scope: CoroutineScope,
    private val onServerChosen: (Long) -> Unit,
) {
    val status: StateFlow<VpnStatus> get() = controller.status

    /**
     * Connects [profileId]. The service treats an already connected core as success, so switching to
     * another server first disconnects and waits for the tunnel to go down.
     */
    fun connect(profileId: Long) {
        onServerChosen(profileId)
        scope.launch {
            session.call { settingsSetSettings(SetSettingsRequest(mapOf(REMEMBER_ID to SettingValue(intValue = profileId)))) }
            if (controller.status.value.state.isLive) {
                controller.disconnect()
                withTimeoutOrNull(SWITCH_TIMEOUT_MS) { controller.status.first { !it.state.isLive } }
            }
            controller.connect(profileId)
        }
    }

    fun disconnect() = controller.disconnect()

    private val VpnState.isLive: Boolean
        get() = this == VpnState.CONNECTED || this == VpnState.CONNECTING || this == VpnState.WAITING_NETWORK

    private companion object {
        const val REMEMBER_ID = "remember.id"
        const val SWITCH_TIMEOUT_MS = 8_000L
    }
}
