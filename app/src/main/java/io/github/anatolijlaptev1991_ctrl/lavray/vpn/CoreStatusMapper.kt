// SPDX-License-Identifier: GPL-3.0-only
package io.github.anatolijlaptev1991_ctrl.lavray.vpn

import io.github.anatolijlaptev1991_ctrl.lavray.api.ConnectCode
import io.github.anatolijlaptev1991_ctrl.lavray.api.gen.CoreState
import io.github.anatolijlaptev1991_ctrl.lavray.ipc.VpnState
import io.github.anatolijlaptev1991_ctrl.lavray.ipc.VpnStatus

object CoreStatusMapper {
    fun fromCore(state: CoreState, lastError: String?): VpnStatus = when (state) {
        CoreState.CORE_STATE_IDLE -> VpnStatus(VpnState.IDLE)
        CoreState.CORE_STATE_CONNECTING, CoreState.CORE_STATE_RECONNECTING -> VpnStatus(VpnState.CONNECTING)
        CoreState.CORE_STATE_CONNECTED -> VpnStatus(VpnState.CONNECTED)
        CoreState.CORE_STATE_DISCONNECTING -> VpnStatus(VpnState.DISCONNECTING)
        CoreState.CORE_STATE_ERROR -> VpnStatus(VpnState.ERROR, lastError)
    }

    /**
     * Failures a retry cannot fix. Mirrors the desktop core's connect-last rules
     * (no profile, bad settings, missing rights) plus a rejected token or a method the core lacks.
     */
    fun isPermanent(code: ConnectCode): Boolean = when (code) {
        ConnectCode.FAILED_PRECONDITION,
        ConnectCode.INVALID_ARGUMENT,
        ConnectCode.NOT_FOUND,
        ConnectCode.PERMISSION_DENIED,
        ConnectCode.UNAUTHENTICATED,
        ConnectCode.UNIMPLEMENTED -> true
        else -> false
    }
}
