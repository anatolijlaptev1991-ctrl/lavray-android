// SPDX-License-Identifier: GPL-3.0-only
package io.github.anatolijlaptev1991_ctrl.lavray.ui.screens.connection

import androidx.annotation.StringRes
import androidx.compose.runtime.Immutable
import io.github.anatolijlaptev1991_ctrl.lavray.R
import io.github.anatolijlaptev1991_ctrl.lavray.api.gen.CoreState
import io.github.anatolijlaptev1991_ctrl.lavray.api.gen.CoreStatus
import io.github.anatolijlaptev1991_ctrl.lavray.api.gen.SubscribeEventsRequest
import io.github.anatolijlaptev1991_ctrl.lavray.api.gen.coreSubscribeEvents
import io.github.anatolijlaptev1991_ctrl.lavray.ui.core.CoreSession
import kotlinx.coroutines.flow.Flow
import kotlinx.coroutines.flow.mapNotNull

/**
 * What the core reported about the tunnel in one status frame. [healthError] is the core's reason code when the
 * check after connecting found that nothing passes through the server (desktop CoreStatus.healthError), else null.
 */
@Immutable
data class HealthSignal(val connected: Boolean, val healthError: String?, val startedAtMs: Long) {
    companion object {
        val None = HealthSignal(connected = false, healthError = null, startedAtMs = 0L)

        /** One core status: connected only in CONNECTED, a blank reason is no reason. */
        fun of(status: CoreStatus) = HealthSignal(
            connected = status.state == CoreState.CORE_STATE_CONNECTED,
            healthError = status.healthError?.trim()?.takeIf { it.isNotEmpty() },
            startedAtMs = status.startedAtMs ?: 0L,
        )
    }
}

/**
 * Where the health signal comes from. Today it is the core's event stream; once the service carries
 * `health_error` in its own status, another implementation can replace [CoreHealthSource] without touching the screen.
 */
fun interface HealthSource {
    /** Frames while collected; the first one is the full current status. Cancelling the collector closes the stream. */
    fun frames(): Flow<HealthSignal>
}

/** The core's SubscribeEvents stream, reduced to the state frames (the only ones that carry the status). */
class CoreHealthSource(private val session: CoreSession) : HealthSource {
    override fun frames(): Flow<HealthSignal> =
        session.stream { coreSubscribeEvents(SubscribeEventsRequest(eventKinds = listOf(STATE_CHANGED))) }
            .mapNotNull { event -> event.stateChanged?.let(HealthSignal::of) }

    private companion object {
        const val STATE_CHANGED = "state_changed"
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
 * was on then (the core reads it when it connects), and when. [trusted] is false when the tunnel was first seen long
 * after it started, so the setting may have changed since and nothing can be said about it.
 */
@Immutable
data class HealthBasis(val startedAtMs: Long, val checkEnabled: Boolean, val readyAtMs: Long, val trusted: Boolean) {
    companion object {
        /** [signal] is the first frame of a connected tunnel; [checkEnabled] is the setting read right then. */
        fun firstSight(signal: HealthSignal, checkEnabled: Boolean, nowMs: Long) = HealthBasis(
            startedAtMs = signal.startedAtMs,
            checkEnabled = checkEnabled,
            readyAtMs = nowMs,
            trusted = signal.startedAtMs > 0 && nowMs - signal.startedAtMs in 0..HealthRules.TRUST_MS,
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
        basis == null || basis.startedAtMs != signal.startedAtMs || !basis.trusted || !basis.checkEnabled -> LinkHealth.NotChecked
        nowMs - basis.readyAtMs < CHECK_WINDOW_MS -> LinkHealth.Checking
        else -> LinkHealth.Verified
    }

    /** Milliseconds until a [LinkHealth.Checking] verdict turns into [LinkHealth.Verified]; 0 when nothing is waiting. */
    fun msUntilVerdict(signal: HealthSignal, basis: HealthBasis?, nowMs: Long): Long =
        if (of(signal, basis, nowMs) == LinkHealth.Checking && basis != null) (basis.readyAtMs + CHECK_WINDOW_MS - nowMs).coerceAtLeast(0L) else 0L
}
