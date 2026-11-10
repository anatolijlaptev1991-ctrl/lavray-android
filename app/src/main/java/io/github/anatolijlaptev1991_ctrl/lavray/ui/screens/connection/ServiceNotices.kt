// SPDX-License-Identifier: GPL-3.0-only
package io.github.anatolijlaptev1991_ctrl.lavray.ui.screens.connection

import androidx.annotation.StringRes
import androidx.compose.runtime.Immutable
import io.github.anatolijlaptev1991_ctrl.lavray.R
import io.github.anatolijlaptev1991_ctrl.lavray.ipc.FailureKind
import io.github.anatolijlaptev1991_ctrl.lavray.ipc.VpnState
import io.github.anatolijlaptev1991_ctrl.lavray.ipc.VpnStatus

/** The tunnel ended without the person asking; [reason] is the sentence, [tip] says whether the battery hint fits it. */
@Immutable
data class StopNotice(@StringRes val reason: Int, val tip: Boolean, val notificationsOff: Boolean)

/** A failure the state alone does not show (the state is not ERROR): protection switched off, a core that would not stop. */
@Immutable
data class SilentFailure(val kind: String, @StringRes val title: Int, @StringRes val body: Int)

/** One note of the core about this connection. [code] is set only for a code the dictionary has no sentence for. */
@Immutable
data class WarningNote(@StringRes val text: Int, val code: String? = null)

/** What the person already closed, so a notice does not come back with the next status. */
@Immutable
data class Dismissed(val stop: Boolean = false, val failureKind: String? = null, val warningsOfTunnel: Int = 0) {
    /**
     * Forgets a dismissal whose cause has ended, so the next unexpected stop is shown again. The proof that it ended is a
     * connected tunnel (the service clears both on a successful connect) or another failure kind. A default IDLE status,
     * which is all a freshly bound controller knows for a moment, proves nothing and must not bring a closed notice back.
     */
    fun after(status: VpnStatus): Dismissed = when {
        status.state == VpnState.CONNECTED -> copy(stop = false, failureKind = null)
        status.failureKind != null && status.failureKind != failureKind -> copy(failureKind = null)
        else -> this
    }

    fun withStop() = copy(stop = true)
    fun withFailure(kind: String) = copy(failureKind = kind)
    fun withWarnings(tunnel: Int) = copy(warningsOfTunnel = tunnel)
}

/**
 * Everything the service says besides the state, reduced to what the Connection screen shows. Pure, so the wording rules
 * are tested. The service skips its own notification while the app is open, so these are then the only signal.
 */
@Immutable
data class ServiceNotices(
    val failure: FailureInfo = FailureInfo.None,
    val needsPermission: Boolean = false,
    val stop: StopNotice? = null,
    val silentFailure: SilentFailure? = null,
    /** Traffic is held on purpose during a rebuild: the state line must not read as a slow connection. */
    val held: Boolean = false,
    val warnings: List<WarningNote> = emptyList(),
) {
    companion object {
        /**
         * [tunnel] is the number of the tunnel now up ([TunnelSight]): the core's notes are shown once per connection.
         * [notificationsOn] is whether the system lets the app post notifications.
         */
        fun of(status: VpnStatus, phase: Phase, tunnel: Int, dismissed: Dismissed, notificationsOn: Boolean): ServiceNotices = ServiceNotices(
            failure = FailureInfo(status.failureKind, status.error ?: status.lastFailure, status.failureReason),
            needsPermission = status.needsPermission || phase == Phase.NeedsPermission,
            stop = stopNotice(status, phase, dismissed, notificationsOn),
            silentFailure = silentFailure(status, phase, dismissed),
            held = status.trafficBlocked,
            warnings = if (phase == Phase.On && dismissed.warningsOfTunnel != tunnel) warningNotes(status.warnings) else emptyList(),
        )

        private fun stopNotice(status: VpnStatus, phase: Phase, dismissed: Dismissed, notificationsOn: Boolean): StopNotice? {
            // While the service is already bringing the tunnel back, "stopped" would be a stale claim.
            val down = phase == Phase.Off || phase == Phase.Error || phase == Phase.NeedsPermission
            if (!status.unexpectedStop || dismissed.stop || !down) return null
            val (reason, tip) = stopReasonText(status.stopReason)
            return StopNotice(reason, tip, notificationsOff = !notificationsOn)
        }

        private fun silentFailure(status: VpnStatus, phase: Phase, dismissed: Dismissed): SilentFailure? {
            val kind = status.failureKind ?: return null
            if (kind == dismissed.failureKind || phase == Phase.Error || status.trafficBlocked) return null
            return when (kind) {
                // Whatever the state is (the core can still be connecting), apps now use the network directly.
                FailureKind.PROTECTION_OFF -> SilentFailure(kind, R.string.fail_protection_off, R.string.nt_protection_off_d)
                // Only once the tunnel is down: a person who connected again no longer needs the old news.
                FailureKind.CORE_STOP_TIMEOUT ->
                    if (phase == Phase.Off) SilentFailure(kind, R.string.fail_core_stop_timeout, R.string.nt_core_stop_d) else null
                else -> null
            }
        }
    }
}

/** The sentence for an exit reason of the service (ApplicationExitInfo code, `process_died`, `tunnel_lost`) and whether the battery hint fits. */
internal fun stopReasonText(reason: String?): Pair<Int, Boolean> = when (reason) {
    "low_memory" -> R.string.nt_stop_memory to true
    "freezer", "excessive_resource_usage", "signaled", "dependency_died" -> R.string.nt_stop_other to true
    "crash", "crash_native", "anr", "initialization_failure" -> R.string.nt_stop_crash to false
    "user_stopped", "user_requested" -> R.string.nt_stop_user to false
    "package_updated", "package_state_change" -> R.string.nt_stop_update to false
    // stopReason can also carry a failure kind of the service: those are not memory kills, so no battery hint.
    "tunnel_lost", "connect_failed" -> R.string.nt_stop_lost to false
    "core_stop_timeout" -> R.string.nt_stop_other to false
    "repeated_crash" -> R.string.nt_stop_crash to false
    else -> R.string.nt_stop_other to true
}

/** The core's notes arrive as "code: sentence" (in the core's language); the code decides the text, the sentence is not shown. */
internal fun warningNotes(raw: List<String>): List<WarningNote> =
    raw.map { codeOf(it) }
        .filter { it.isNotEmpty() }
        .distinct()
        .map { code ->
            when (code) {
                "custom_unconfirmed" -> WarningNote(R.string.nt_warn_custom)
                "tun_mixed_enabled" -> WarningNote(R.string.nt_warn_mixed)
                else -> WarningNote(R.string.nt_warn_other, code)
            }
        }

private val codeShape = Regex("[a-z][a-z0-9_]{0,39}")

/** The machine code before the colon; a note without one (or with a sentence there) is just "unknown". Blank notes are nothing. */
private fun codeOf(note: String): String {
    if (note.isBlank()) return ""
    val head = note.substringBefore(':').trim()
    return if (codeShape.matches(head)) head else "unknown"
}
