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

object HealthRules {
    /**
     * The core tests the address twice at most (8 s each, 1 s apart); a verdict that has not come within this window
     * after the start is not coming, so a quiet core means the check passed. Success sends no event, only failure does.
     */
    const val CHECK_WINDOW_MS = 20_000L

    /** Reason codes of connect_health.go and their dictionary text; any other reason is shown as the core wrote it. */
    @StringRes
    fun reasonText(code: String): Int? = when (code) {
        "refused" -> R.string.conn_noTraffic_refused
        "reset" -> R.string.conn_noTraffic_reset
        "no_host" -> R.string.conn_noTraffic_no_host
        "timeout" -> R.string.conn_noTraffic_timeout
        else -> null
    }

    fun of(signal: HealthSignal, checkEnabled: Boolean, nowMs: Long): LinkHealth = when {
        !signal.connected -> LinkHealth.Unknown
        signal.healthError != null -> LinkHealth.NoTraffic(signal.healthError)
        !checkEnabled -> LinkHealth.NotChecked
        signal.startedAtMs > 0 && nowMs - signal.startedAtMs < CHECK_WINDOW_MS -> LinkHealth.Checking
        else -> LinkHealth.Verified
    }

    /** Milliseconds until a [LinkHealth.Checking] verdict turns into [LinkHealth.Verified]; 0 when it already has. */
    fun msUntilVerdict(signal: HealthSignal, nowMs: Long): Long =
        if (signal.startedAtMs > 0) (signal.startedAtMs + CHECK_WINDOW_MS - nowMs).coerceAtLeast(0L) else 0L
}
