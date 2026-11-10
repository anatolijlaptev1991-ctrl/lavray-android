// SPDX-License-Identifier: GPL-3.0-only
package io.github.anatolijlaptev1991_ctrl.lavray.ipc

import kotlinx.serialization.Serializable
import kotlinx.serialization.json.Json

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

/** Stable machine codes of [VpnStatus.failureKind]; the UI picks its own text by them. */
object FailureKind {
    const val NO_SERVER = "no_server"
    const val CORE_START = "core_start"
    const val APPS_NONE = "apps_none"
    const val PERMISSION = "permission"
    const val CONNECT_FAILED = "connect_failed"
    const val RESTRICTED_START = "restricted_start"

    /** The tunnel was up and the core let go of it for good (no retry, or retries ran out of road). */
    const val TUNNEL_LOST = "tunnel_lost"

    /** A deliberate traffic block during a transition ran into its hard cap; apps now go direct. */
    const val PROTECTION_OFF = "protection_off"
}

/**
 * What the VPN layer reports to the UI (the contract is written down in docs/ARCHITECTURE.md,
 * "Статус службы для интерфейса"). Every field after [error] is additive and has a safe default.
 *
 * [error] is a short machine-safe message, never a secret; shown only when [state] is ERROR.
 */
data class VpnStatus(
    val state: VpnState,
    val error: String? = null,
    /** CoreStatus.health_error: a code from the core, set while [state] is CONNECTED but the server passes nothing. */
    val healthError: String? = null,
    /** The tunnel was wanted and the process or the link died; cleared on the next successful connect or user disconnect. */
    val unexpectedStop: Boolean = false,
    /** Why the process died (ApplicationExitInfo, for example "low_memory") or a [FailureKind]; null when unknown. */
    val stopReason: String? = null,
    /** VPN consent is missing; persisted, so it is true after a process restart too. */
    val needsPermission: Boolean = false,
    /** Persisted reason of the last start failure; cleared by the next successful connect or user connect. */
    val lastFailure: String? = null,
    /** [FailureKind] of [lastFailure]. */
    val failureKind: String? = null,
    /** Selected packages that are not installed and were skipped when the tunnel was built. */
    val skippedPackages: List<String> = emptyList(),
    /** Epoch millis until which traffic is deliberately blocked during a transition; 0 when nothing is blocked. */
    val blackholeUntilMs: Long = 0L,
) {
    /** True while the interface is held with nobody reading it: apps have no network on purpose. */
    val trafficBlocked: Boolean get() = blackholeUntilMs > 0L

    companion object {
        val IDLE = VpnStatus(VpnState.IDLE)
    }
}

/** The part of [VpnStatus] that rides as JSON in the third argument of IVpnObserver.onStatus. Unknown keys are ignored. */
@Serializable
internal data class StatusDetail(
    val healthError: String? = null,
    val unexpectedStop: Boolean = false,
    val stopReason: String? = null,
    val needsPermission: Boolean = false,
    val lastFailure: String? = null,
    val failureKind: String? = null,
    val skippedPackages: List<String> = emptyList(),
    val blackholeUntilMs: Long = 0L,
)

object VpnStatusWire {
    private val json = Json { ignoreUnknownKeys = true; encodeDefaults = false }

    fun detailOf(status: VpnStatus): String = json.encodeToString(
        StatusDetail.serializer(),
        StatusDetail(
            status.healthError, status.unexpectedStop, status.stopReason, status.needsPermission,
            status.lastFailure, status.failureKind, status.skippedPackages, status.blackholeUntilMs,
        ),
    )

    /** Unreadable or empty detail leaves the defaults: a status never fails to arrive because of an extra. */
    fun decode(state: Int, error: String?, detail: String?): VpnStatus {
        val d = try {
            if (detail.isNullOrBlank()) StatusDetail() else json.decodeFromString(StatusDetail.serializer(), detail)
        } catch (_: IllegalArgumentException) {
            StatusDetail()
        }
        return VpnStatus(
            VpnState.fromCode(state), error, d.healthError, d.unexpectedStop, d.stopReason, d.needsPermission,
            d.lastFailure, d.failureKind, d.skippedPackages, d.blackholeUntilMs,
        )
    }
}
