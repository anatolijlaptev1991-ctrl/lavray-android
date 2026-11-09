// SPDX-License-Identifier: GPL-3.0-only
package io.github.anatolijlaptev1991_ctrl.lavray.ui.screens.connection

import androidx.compose.runtime.Immutable
import io.github.anatolijlaptev1991_ctrl.lavray.api.gen.CoreState
import io.github.anatolijlaptev1991_ctrl.lavray.api.gen.GetProfileRequest
import io.github.anatolijlaptev1991_ctrl.lavray.api.gen.ListProfilesRequest
import io.github.anatolijlaptev1991_ctrl.lavray.api.gen.Page
import io.github.anatolijlaptev1991_ctrl.lavray.api.gen.ProfileType
import io.github.anatolijlaptev1991_ctrl.lavray.api.gen.StatsSnapshot
import io.github.anatolijlaptev1991_ctrl.lavray.api.gen.SubscribeStatsRequest
import io.github.anatolijlaptev1991_ctrl.lavray.api.gen.coreGetStatus
import io.github.anatolijlaptev1991_ctrl.lavray.api.gen.profileGetProfile
import io.github.anatolijlaptev1991_ctrl.lavray.api.gen.profileListProfiles
import io.github.anatolijlaptev1991_ctrl.lavray.api.gen.routingGetActivePreset
import io.github.anatolijlaptev1991_ctrl.lavray.api.gen.statsSubscribe
import io.github.anatolijlaptev1991_ctrl.lavray.ui.core.CoreResult
import io.github.anatolijlaptev1991_ctrl.lavray.ui.core.CoreSession
import io.github.anatolijlaptev1991_ctrl.lavray.ui.core.ErrorKind
import io.github.anatolijlaptev1991_ctrl.lavray.ui.core.StreamFailure
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.flow.Flow
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.flow.catch
import kotlinx.coroutines.flow.emitAll
import kotlinx.coroutines.flow.flow
import kotlinx.coroutines.flow.scan
import kotlinx.coroutines.launch

@Immutable
data class ServerSummary(val id: Long, val name: String, val address: String, val port: Int, val type: ProfileType)

@Immutable
data class PresetSummary(val id: String, val displayName: String)

/** What the connection screen shows that does not come from the VPN service itself. */
@Immutable
data class ConnectionInfo(
    val loaded: Boolean = false,
    val server: ServerSummary? = null,
    val preset: PresetSummary? = null,
    val startedAtMs: Long = 0L,
    val hasServers: Boolean = false,
    val error: ErrorKind? = null,
)

/** Speed and session totals from the stats stream (desktop lib/traffic.ts), bytes and bytes per second. */
@Immutable
data class TrafficReading(val downBps: Double = 0.0, val upBps: Double = 0.0, val downTotal: Long = 0L, val upTotal: Long = 0L) {
    companion object {
        val Empty = TrafficReading()
    }
}

/** Applies one frame: the sum over all tags is the speed, a final frame zeroes it, totals persist. */
fun TrafficReading.apply(snapshot: StatsSnapshot): TrafficReading {
    val session = snapshot.session
    val down = if (snapshot.final) 0.0 else snapshot.tags.sumOf { it.downlinkBps }
    val up = if (snapshot.final) 0.0 else snapshot.tags.sumOf { it.uplinkBps }
    return TrafficReading(
        downBps = down,
        upBps = up,
        downTotal = session?.downlinkTotal ?: downTotal,
        upTotal = session?.uplinkTotal ?: upTotal,
    )
}

/**
 * Loads the current server, active route preset and start time for the connection screen. [traffic] is a cold
 * stream: it opens the stats socket when collected and closes it when the collector goes away, so it only
 * runs while the screen is visible and the tunnel is up.
 */
class ConnectionModel(private val session: CoreSession, private val scope: CoroutineScope) {
    private val mutable = MutableStateFlow(ConnectionInfo())
    val info: StateFlow<ConnectionInfo> = mutable.asStateFlow()

    /** Reads the core. [selectedId] is the server the person last chose; a running tunnel overrides it. */
    fun refresh(selectedId: Long) {
        scope.launch(Dispatchers.IO) {
            val status = session.call { coreGetStatus() }
            if (status is CoreResult.Fail) {
                mutable.value = mutable.value.copy(loaded = true, error = status.kind)
                return@launch
            }
            val st = (status as CoreResult.Ok).value
            val live = st.state == CoreState.CORE_STATE_CONNECTED || st.state == CoreState.CORE_STATE_CONNECTING
            val first = session.call { profileListProfiles(ListProfilesRequest(page = Page(0L, 1))) }
            val hasServers = first is CoreResult.Ok && first.value.total > 0
            val wanted = if (live) st.activeProfileId ?: selectedId else selectedId
            val server = loadServer(wanted) ?: (first as? CoreResult.Ok)?.value?.profiles?.firstOrNull()?.let {
                ServerSummary(it.id, it.name, it.address, it.port, it.type)
            }
            val preset = (session.call { routingGetActivePreset() } as? CoreResult.Ok)?.value?.let { PresetSummary(it.id, it.displayName) }
            mutable.value = ConnectionInfo(
                loaded = true, server = server, preset = preset,
                startedAtMs = if (live) st.startedAtMs ?: 0L else 0L, hasServers = hasServers,
            )
        }
    }

    private suspend fun loadServer(id: Long): ServerSummary? {
        if (id <= 0) return null
        val r = session.call { profileGetProfile(GetProfileRequest(id)) }
        return (r as? CoreResult.Ok)?.value?.let { ServerSummary(it.id, it.name, it.address, it.port, it.type) }
    }

    /** Live speed and totals; completes with an empty reading when the core cannot be reached. */
    fun traffic(): Flow<TrafficReading> = flow {
        emit(TrafficReading.Empty)
        emitAll(
            session.stream { statsSubscribe(SubscribeStatsRequest(intervalMs = STATS_INTERVAL_MS)) }
                .scan(TrafficReading.Empty) { acc, snap -> acc.apply(snap) }
                .catch { e -> if (e !is StreamFailure) throw e },
        )
    }

    private companion object {
        const val STATS_INTERVAL_MS = 1000
    }
}
