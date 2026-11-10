// SPDX-License-Identifier: GPL-3.0-only
package io.github.anatolijlaptev1991_ctrl.lavray.ui.screens.diag

import androidx.compose.runtime.Immutable
import io.github.anatolijlaptev1991_ctrl.lavray.api.gen.GetTrafficSummaryResponse
import io.github.anatolijlaptev1991_ctrl.lavray.api.gen.GetTrafficTotalsRequest
import io.github.anatolijlaptev1991_ctrl.lavray.api.gen.Group
import io.github.anatolijlaptev1991_ctrl.lavray.api.gen.StatsSnapshot
import io.github.anatolijlaptev1991_ctrl.lavray.api.gen.SubscribeStatsRequest
import io.github.anatolijlaptev1991_ctrl.lavray.api.gen.TrafficCounters
import io.github.anatolijlaptev1991_ctrl.lavray.api.gen.profileListGroups
import io.github.anatolijlaptev1991_ctrl.lavray.api.gen.statsGetTrafficSummary
import io.github.anatolijlaptev1991_ctrl.lavray.api.gen.statsGetTrafficTotals
import io.github.anatolijlaptev1991_ctrl.lavray.api.gen.statsSubscribe
import io.github.anatolijlaptev1991_ctrl.lavray.ui.core.CoreResult
import io.github.anatolijlaptev1991_ctrl.lavray.ui.core.CoreSession
import io.github.anatolijlaptev1991_ctrl.lavray.ui.core.ErrorKind
import io.github.anatolijlaptev1991_ctrl.lavray.ui.screens.connection.TrafficReading
import io.github.anatolijlaptev1991_ctrl.lavray.ui.screens.connection.apply
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.delay
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.flow.update
import kotlinx.coroutines.withContext

/** Bytes both ways. */
@Immutable
data class TrafficPair(val upBytes: Long = 0L, val downBytes: Long = 0L) {
    val total: Long get() = upBytes + downBytes
}

/** One outbound of the running session ("proxy", "direct", ...) with its speed and totals. */
@Immutable
data class TagLine(val tag: String, val downBps: Double, val upBps: Double, val traffic: TrafficPair)

/** One group (a subscription or the manual list) in the summary the core keeps per session history. */
@Immutable
data class GroupLine(val groupId: Long, val name: String?, val traffic: TrafficPair, val sessions: Long, val lastSessionMs: Long?)

@Immutable
data class StatsState(
    val loaded: Boolean = false,
    val session: TrafficReading = TrafficReading.Empty,
    val tags: List<TagLine> = emptyList(),
    val total: TrafficPair = TrafficPair(),
    val groups: List<GroupLine> = emptyList(),
    val error: ErrorKind? = null,
    /** A live session has reported on this screen, so the stored totals are out of date once it ends. */
    val sessionSeen: Boolean = false,
)

/** Pure mapping from core messages to what the screen shows. */
object StatsMapper {
    fun total(totals: Map<Long, TrafficCounters>): TrafficPair =
        TrafficPair(totals.values.sumOf { it.uplinkBytes }, totals.values.sumOf { it.downlinkBytes })

    /** Groups with some traffic or sessions, the biggest first; a group the person deleted keeps its numbers, unnamed. */
    fun groups(summary: GetTrafficSummaryResponse, names: Map<Long, String>): List<GroupLine> =
        summary.groups
            .filter { it.uplinkBytes + it.downlinkBytes > 0 || it.sessionCount > 0 }
            .map { GroupLine(it.groupId, names[it.groupId], TrafficPair(it.uplinkBytes, it.downlinkBytes), it.sessionCount, it.lastSessionMs) }
            .sortedByDescending { it.traffic.total }

    fun tags(snapshot: StatsSnapshot): List<TagLine> =
        if (snapshot.final) emptyList() else snapshot.tags.map { TagLine(it.tag, it.downlinkBps, it.uplinkBps, TrafficPair(it.uplinkTotal, it.downlinkTotal)) }
            .sortedByDescending { it.traffic.total }

    fun names(groups: List<Group>): Map<Long, String> = groups.associate { it.id to it.name }

    fun afterFrame(s: StatsState, snap: StatsSnapshot): StatsState =
        s.copy(session = s.session.apply(snap), tags = tags(snap), error = null, sessionSeen = true)

    /** The live part goes when the tunnel leaves CONNECTED; [StatsState.sessionSeen] stays until the totals were read again. */
    fun afterEnd(s: StatsState): StatsState = s.copy(session = TrafficReading.Empty, tags = emptyList())

    /** The totals of the ended session were read (or there was no session): nothing is waiting any more. */
    fun afterReload(s: StatsState): StatsState = s.copy(sessionSeen = false)
}

/**
 * Traffic statistics: the live session from the stats stream (open only while the screen is visible and a tunnel is
 * up) and the totals and per-group summary from unary calls made when the screen opens and again when the session
 * that was shown ends.
 */
class StatisticsModel(private val session: CoreSession) {
    private val mutable = MutableStateFlow(StatsState())
    val state: StateFlow<StatsState> = mutable.asStateFlow()

    suspend fun load() = withContext(Dispatchers.IO) {
        val totals = session.call { statsGetTrafficTotals(GetTrafficTotalsRequest()) }
        val summary = session.call { statsGetTrafficSummary() }
        val groups = session.call { profileListGroups() }
        val failure = listOf(totals, summary).filterIsInstance<CoreResult.Fail>().firstOrNull()
        if (failure != null) {
            mutable.update { it.copy(loaded = true, error = failure.kind) }
            return@withContext
        }
        val names = (groups as? CoreResult.Ok)?.value?.groups?.let(StatsMapper::names).orEmpty()
        mutable.update {
            it.copy(
                loaded = true, error = null,
                total = StatsMapper.total((totals as CoreResult.Ok).value.totals),
                groups = StatsMapper.groups((summary as CoreResult.Ok).value, names),
            )
        }
    }

    /** Suspends while the stream lives; cancelling it closes the socket. Totals are refreshed when the session ends. */
    suspend fun watch() = withContext(Dispatchers.Default) {
        StreamBackoff.run(
            open = { session.stream { statsSubscribe(SubscribeStatsRequest(intervalMs = INTERVAL_MS)) } },
            onError = { kind -> mutable.update { it.copy(error = kind) } },
            onFrame = { snap -> mutable.update { StatsMapper.afterFrame(it, snap) } },
        )
    }

    /** The tunnel left CONNECTED: the live part is cleared. */
    fun endSession() = mutable.update(StatsMapper::afterEnd)

    /**
     * The tunnel is fully down. The core writes the end of a session after the stop completes, which is later than the
     * first frame that is not CONNECTED, so the totals are read when the tunnel is down, and once more shortly after, in
     * case the write was still under way. Only when a session was shown on this screen.
     */
    suspend fun reloadAfterSession() {
        if (!mutable.value.sessionSeen) return
        mutable.update(StatsMapper::afterReload)
        load()
        delay(RELOAD_RETRY_MS)
        load()
    }

    private companion object {
        const val INTERVAL_MS = 1000
        const val RELOAD_RETRY_MS = 1_500L
    }
}
