// SPDX-License-Identifier: GPL-3.0-only
package io.github.anatolijlaptev1991_ctrl.lavray.ipc

/** What the VPN layer reports to the UI. Codes are stable: they cross the process boundary as ints. */
enum class VpnState(val code: Int) {
    IDLE(0),
    CONNECTING(1),
    CONNECTED(2),
    DISCONNECTING(3),
    WAITING_NETWORK(4),
    NEEDS_PERMISSION(5),
    ERROR(6);

    companion object {
        fun fromCode(code: Int): VpnState = entries.firstOrNull { it.code == code } ?: IDLE
    }
}

/** [error] is a short machine-safe message, never a secret; shown only when [state] is ERROR. */
data class VpnStatus(val state: VpnState, val error: String? = null) {
    companion object {
        val IDLE = VpnStatus(VpnState.IDLE)
    }
}
