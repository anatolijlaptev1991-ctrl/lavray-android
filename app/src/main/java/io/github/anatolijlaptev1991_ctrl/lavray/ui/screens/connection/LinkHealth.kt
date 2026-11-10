// SPDX-License-Identifier: GPL-3.0-only
package io.github.anatolijlaptev1991_ctrl.lavray.ui.screens.connection

import androidx.annotation.StringRes
import androidx.compose.runtime.Immutable
import io.github.anatolijlaptev1991_ctrl.lavray.R
import io.github.anatolijlaptev1991_ctrl.lavray.ipc.VpnState
import io.github.anatolijlaptev1991_ctrl.lavray.ipc.VpnStatus

/**
 * What the service reports about the tunnel right now. [healthError] is the core's reason code when its check after
 * connecting found that nothing passes through the server (CoreStatus.health_error, carried by the service in
 * [VpnStatus.healthError]), else null. [tunnel] numbers the tunnels this process has seen come up ([TunnelSight]): it
 * is what ties a verdict to the tunnel it describes.
 *
 * The service is the source on purpose: it already holds the core's event stream for its own decisions and puts the
 * reason into every status it publishes, and it drops the reason whenever it does not show the tunnel as connected (a
 * rebuild, a grace period), so a reason of the previous tunnel cannot outlive it. A second SubscribeEvents stream from
 * the screen would carry the same frames at the cost of one more socket and a reopening loop.
 */
@Immutable
data class HealthSignal(val connected: Boolean, val healthError: String?, val tunnel: Int) {
    companion object {
        val None = HealthSignal(connected = false, healthError = null, tunnel = 0)

        /** Connected only in CONNECTED, a blank reason is no reason. */
        fun of(status: VpnStatus, tunnel: Int) = HealthSignal(
            connected = status.state == VpnState.CONNECTED,
            healthError = status.healthError?.trim()?.takeIf { it.isNotEmpty() && status.state == VpnState.CONNECTED },
            tunnel = tunnel,
        )
    }
}

/** What the Connection screen may say about traffic through the server. */
@Immutable
sealed interface LinkHealth {
    /** No tunnel, or no word from the core yet: nothing is claimed. */
    data object Unknown : LinkHealth

    /** The core's check after connecting has not finished. */
    data object Checking : LinkHealth

    /** The check ran and found no problem. */
    data object Verified : LinkHealth

    /** The person switched the check off in Settings: the tunnel is up, and nothing is known about the traffic. */
    data object NotChecked : LinkHealth

    /** The check failed: [reason] is a code ([HealthRules.reasonText]) or the core's own short text. */
    data class NoTraffic(val reason: String) : LinkHealth
}

/**
 * What is known about THIS tunnel's health check, taken when the tunnel was first seen connected: whether the setting
 * was on then (the core reads it when it connects), and when. [trusted] is false when the tunnel had already been running
 * long when it was first seen, or the start time or the setting could not be read: the setting may have changed since and
 * nothing can be said about it.
 */
@Immutable
data class HealthBasis(
    val tunnel: Int,
    val checkEnabled: Boolean,
    val readyAtMs: Long,
    val trusted: Boolean,
    /** The core's start time of the tunnel this was taken for: what lets a recreated screen recognise the same tunnel. */
    val startedAtMs: Long? = null,
) {
    companion object {
        /** [startedAtMs] is the core's start time of the tunnel (null when unreadable); [checkEnabled] the setting read right then (null when unreadable). */
        fun firstSight(tunnel: Int, startedAtMs: Long?, checkEnabled: Boolean?, nowMs: Long) = HealthBasis(
            tunnel = tunnel,
            checkEnabled = checkEnabled ?: false,
            readyAtMs = nowMs,
            startedAtMs = startedAtMs,
            trusted = checkEnabled != null && startedAtMs != null && startedAtMs > 0 && nowMs - startedAtMs in 0..HealthRules.TRUST_MS,
        )
    }
}

object HealthRules {
    /**
     * How long after the tunnel became ready a failure can still arrive: the core tests the address twice at most
     * (8 s each, 1 s apart), so 17 s, plus a margin. Success sends no event, only failure does, so "verified" is a verdict
     * after this window. It is counted from when the tunnel was first seen ready, never from the start of the run (the
     * core starts the check only after it is ready, which can be much later than the start).
     */
    const val CHECK_WINDOW_MS = 25_000L

    /** A tunnel first seen connected within this time of its start was watched from near its beginning. */
    const val TRUST_MS = 45_000L

    /** Reason codes of connect_health.go and their dictionary text; any other reason is shown as the core wrote it. */
    @StringRes
    fun reasonText(code: String): Int? = when (code) {
        "refused" -> R.string.conn_noTraffic_refused
        "reset" -> R.string.conn_noTraffic_reset
        "no_host" -> R.string.conn_noTraffic_no_host
        "timeout" -> R.string.conn_noTraffic_timeout
        else -> null
    }

    /**
     * "Verified" needs a basis for this very tunnel (same start), a setting that was on when it connected, trust in that
     * reading, and the whole window without a failure. In every doubtful case the answer is [LinkHealth.NotChecked]: the
     * core's own failure reason is shown whatever else is unknown, a verification never is.
     */
    fun of(signal: HealthSignal, basis: HealthBasis?, nowMs: Long): LinkHealth = when {
        !signal.connected -> LinkHealth.Unknown
        signal.healthError != null -> LinkHealth.NoTraffic(signal.healthError)
        basis == null || basis.tunnel != signal.tunnel || !basis.trusted || !basis.checkEnabled -> LinkHealth.NotChecked
        nowMs - basis.readyAtMs < CHECK_WINDOW_MS -> LinkHealth.Checking
        else -> LinkHealth.Verified
    }

    /** Milliseconds until a [LinkHealth.Checking] verdict turns into [LinkHealth.Verified]; 0 when nothing is waiting. */
    fun msUntilVerdict(signal: HealthSignal, basis: HealthBasis?, nowMs: Long): Long =
        if (of(signal, basis, nowMs) == LinkHealth.Checking && basis != null) (basis.readyAtMs + CHECK_WINDOW_MS - nowMs).coerceAtLeast(0L) else 0L

    /**
     * Sleeps until the verdict is due and returns when it is no longer [LinkHealth.Checking]. The sleep runs on the uptime
     * clock and the verdict reads the wall clock: after the wall clock was stepped back the verdict can still be "checking"
     * when the sleep ends, so the wait is asked again instead of trusting one sleep.
     */
    suspend fun awaitVerdict(signal: HealthSignal, basis: HealthBasis?, now: () -> Long, sleep: suspend (Long) -> Unit) {
        while (true) {
            val wait = msUntilVerdict(signal, basis, now())
            if (wait <= 0L) return
            sleep(wait)
        }
    }
}
