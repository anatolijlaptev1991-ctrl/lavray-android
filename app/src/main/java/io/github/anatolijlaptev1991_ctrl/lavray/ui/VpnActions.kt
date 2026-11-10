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
import kotlinx.coroutines.launch

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
     * Connects [profileId]. While a tunnel is up, coming up, waiting for a network or tearing down, the switch goes
     * through the service's `reconnect`: it rebuilds the tunnel without a direct-traffic gap and runs after a
     * disconnect that is still in progress, so a connect is never issued into a tunnel that is going down.
     */
    fun connect(profileId: Long) {
        onServerChosen(profileId)
        scope.launch {
            session.call { settingsSetSettings(SetSettingsRequest(mapOf(REMEMBER_ID to SettingValue(intValue = profileId)))) }
            when (SwitchPlan.decide(controller.status.value.state)) {
                SwitchPlan.Action.CONNECT -> controller.connect(profileId)
                SwitchPlan.Action.RECONNECT -> controller.reconnect(profileId)
            }
        }
    }

    fun disconnect() = controller.disconnect()

    private companion object {
        const val REMEMBER_ID = "remember.id"
    }
}

/** What choosing a server means in each state of the tunnel. */
object SwitchPlan {
    enum class Action { CONNECT, RECONNECT }

    /** Only a tunnel that is definitely down is connected afresh; anything else is rebuilt by the service. */
    fun decide(state: VpnState): Action = when (state) {
        VpnState.IDLE, VpnState.ERROR, VpnState.NEEDS_PERMISSION -> Action.CONNECT
        VpnState.CONNECTING, VpnState.CONNECTED, VpnState.DISCONNECTING, VpnState.WAITING_NETWORK -> Action.RECONNECT
    }
}
