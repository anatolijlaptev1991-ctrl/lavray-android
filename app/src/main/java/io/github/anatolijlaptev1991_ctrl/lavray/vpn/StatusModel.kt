// SPDX-License-Identifier: GPL-3.0-only
package io.github.anatolijlaptev1991_ctrl.lavray.vpn

import io.github.anatolijlaptev1991_ctrl.lavray.api.gen.CoreState
import io.github.anatolijlaptev1991_ctrl.lavray.ipc.VpnState
import io.github.anatolijlaptev1991_ctrl.lavray.ipc.VpnStatus

/** Everything the status shown to the UI is derived from; plain values so it runs on the JVM. */
internal data class StatusInputs(
    val coreState: CoreState? = null,
    val coreError: String? = null,
    val healthError: String? = null,
    val phase: AutoPhase = AutoPhase.IDLE,
    val needsPermission: Boolean = false,
    val lastFailure: String? = null,
    val failureKind: String? = null,
    val failureReason: String? = null,
    val warnings: List<String> = emptyList(),
    val coreEpoch: Int = 0,
    /** The core let go of an established tunnel and the grace period has not ended yet. */
    val inGrace: Boolean = false,
    /** A deliberate disconnect is waiting for the core. */
    val disconnecting: Boolean = false,
    val unexpectedStop: Boolean = false,
    val stopReason: String? = null,
    val skippedPackages: List<String> = emptyList(),
    val blackholeUntilMs: Long = 0L,
)

internal object StatusModel {
    fun compute(i: StatusInputs): VpnStatus {
        val core = i.coreState?.let { CoreStatusMapper.fromCore(it, i.coreError, i.healthError) } ?: VpnStatus.IDLE
        val shown = when {
            i.needsPermission -> VpnStatus(VpnState.NEEDS_PERMISSION)
            i.disconnecting -> VpnStatus(VpnState.DISCONNECTING)
            i.phase == AutoPhase.WAITING_NETWORK || i.phase == AutoPhase.BACKING_OFF -> VpnStatus(VpnState.WAITING_NETWORK)
            i.phase == AutoPhase.FAILED -> VpnStatus(VpnState.ERROR, i.lastFailure)
            i.inGrace -> VpnStatus(VpnState.CONNECTING)
            // An attempt is running: whatever the core still says from the previous run is not news.
            i.phase == AutoPhase.ATTEMPTING && (core.state == VpnState.IDLE || core.state == VpnState.ERROR) ->
                VpnStatus(VpnState.CONNECTING)
            // A deliberate disconnect never ends in a displayed error: the core reports its aborted start as ERROR.
            i.phase == AutoPhase.STOPPED && core.state == VpnState.ERROR -> VpnStatus(VpnState.IDLE)
            else -> core
        }
        return shown.copy(
            // The health error means something only while the tunnel is shown as up.
            healthError = if (shown.state == VpnState.CONNECTED) core.healthError else null,
            unexpectedStop = i.unexpectedStop,
            stopReason = i.stopReason.takeIf { i.unexpectedStop },
            needsPermission = i.needsPermission,
            lastFailure = i.lastFailure,
            failureKind = i.failureKind,
            failureReason = i.failureReason,
            warnings = if (shown.state == VpnState.CONNECTED) i.warnings else emptyList(),
            coreEpoch = i.coreEpoch,
            skippedPackages = i.skippedPackages,
            blackholeUntilMs = i.blackholeUntilMs,
        )
    }
}
