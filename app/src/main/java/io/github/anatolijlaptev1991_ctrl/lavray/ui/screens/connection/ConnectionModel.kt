// SPDX-License-Identifier: GPL-3.0-only
package io.github.anatolijlaptev1991_ctrl.lavray.ui.screens.connection

import androidx.compose.runtime.Immutable
import io.github.anatolijlaptev1991_ctrl.lavray.api.gen.CoreState
import io.github.anatolijlaptev1991_ctrl.lavray.api.gen.GetProfileRequest
import io.github.anatolijlaptev1991_ctrl.lavray.api.gen.GetSettingsRequest
import io.github.anatolijlaptev1991_ctrl.lavray.api.gen.ListProfilesRequest
import io.github.anatolijlaptev1991_ctrl.lavray.api.gen.Page
import io.github.anatolijlaptev1991_ctrl.lavray.api.gen.Profile
import io.github.anatolijlaptev1991_ctrl.lavray.api.gen.ProfileType
import io.github.anatolijlaptev1991_ctrl.lavray.api.gen.StatsSnapshot
import io.github.anatolijlaptev1991_ctrl.lavray.api.gen.SubscribeStatsRequest
import io.github.anatolijlaptev1991_ctrl.lavray.api.gen.coreGetStatus
import io.github.anatolijlaptev1991_ctrl.lavray.api.gen.profileGetProfile
import io.github.anatolijlaptev1991_ctrl.lavray.api.gen.profileListProfiles
import io.github.anatolijlaptev1991_ctrl.lavray.api.gen.routingGetActivePreset
import io.github.anatolijlaptev1991_ctrl.lavray.api.gen.settingsGetSettings
import io.github.anatolijlaptev1991_ctrl.lavray.api.gen.statsSubscribe
import io.github.anatolijlaptev1991_ctrl.lavray.api.gen.subscriptionListSubscriptions
import io.github.anatolijlaptev1991_ctrl.lavray.ui.core.CoreResult
import io.github.anatolijlaptev1991_ctrl.lavray.ui.core.CoreSession
import io.github.anatolijlaptev1991_ctrl.lavray.ui.core.ErrorKind
import io.github.anatolijlaptev1991_ctrl.lavray.ui.core.StreamFailure
import io.github.anatolijlaptev1991_ctrl.lavray.ui.screens.diag.StreamBackoff
import io.github.anatolijlaptev1991_ctrl.lavray.ui.screens.settings.SettingKeys
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.Job
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
data class ServerSummary(val id: Long, val name: String, val address: String, val port: Int, val type: ProfileType, val geo: String = "") {
    companion object {
        fun of(p: Profile) = ServerSummary(p.id, p.name, p.address, p.port, p.type, p.geo)
    }
}

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
    val subscriptions: Int = 0,
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
class ConnectionModel(
    private val session: CoreSession,
    private val scope: CoroutineScope,
    private val healthSource: HealthSource = CoreHealthSource(session),
    /** The "check the link after connecting" setting as it is now; null when it cannot be read. */
    private val healthCheckReader: suspend () -> Boolean? = { readHealthSetting(session) },
    private val clock: () -> Long = System::currentTimeMillis,
) {
    private val mutable = MutableStateFlow(ConnectionInfo())
    val info: StateFlow<ConnectionInfo> = mutable.asStateFlow()

    private val mutableHealth = MutableStateFlow(HealthSignal.None)

    /** The last status frame of the core's event stream; [HealthSignal.None] while the stream is closed or broken. */
    val health: StateFlow<HealthSignal> = mutableHealth.asStateFlow()

    private val mutableBasis = MutableStateFlow<HealthBasis?>(null)

    /** What is known about the health check of the tunnel that [health] describes; null until a tunnel was seen connected. */
    val healthBasis: StateFlow<HealthBasis?> = mutableBasis.asStateFlow()

    private var refreshJob: Job? = null

    /**
     * Reads the core. [selectedId] is the server the person last chose; a running tunnel overrides it. A newer call
     * replaces the one still running: otherwise an older pass, finishing last, would write the state of a moment ago.
     */
    @Synchronized
    fun refresh(selectedId: Long) {
        refreshJob?.cancel()
        refreshJob = scope.launch(Dispatchers.IO) {
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
            val server = loadServer(wanted) ?: (first as? CoreResult.Ok)?.value?.profiles?.firstOrNull()?.let(ServerSummary::of)
            val subscriptions = (session.call { subscriptionListSubscriptions() } as? CoreResult.Ok)?.value?.groups?.size ?: 0
            val preset = (session.call { routingGetActivePreset() } as? CoreResult.Ok)?.value?.let { PresetSummary(it.id, it.displayName) }
            mutable.value = ConnectionInfo(
                loaded = true, server = server, preset = preset,
                startedAtMs = if (live) st.startedAtMs ?: 0L else 0L, hasServers = hasServers, subscriptions = subscriptions,
            )
        }
    }

    /**
     * Follows the core's status frames and keeps the latest in [health]. It suspends for as long as it is collected
     * (the screen runs it only while visible and the tunnel is up) and reopens the stream after a break. The frame
     * that first shows a tunnel connected also fixes its [healthBasis]: the setting is read right then, because the core
     * reads it when it connects. The last frame is kept when the screen is left (the next visit starts from it and gets
     * a fresh one at once); [resetHealth] clears it when the tunnel is gone.
     */
    suspend fun watchHealth() {
        StreamBackoff.run(
            open = { healthSource.frames() },
            onError = { mutableHealth.value = HealthSignal.None },
            onFrame = { frame ->
                mutableHealth.value = frame
                if (frame.connected && frame.startedAtMs > 0 && mutableBasis.value?.startedAtMs != frame.startedAtMs) {
                    mutableBasis.value = basisFor(frame)
                }
            },
        )
    }

    private suspend fun basisFor(frame: HealthSignal): HealthBasis {
        val enabled = healthCheckReader()
        val seen = HealthBasis.firstSight(frame, enabled ?: false, clock())
        // A setting that could not be read is no reading: nothing is claimed about the check.
        return if (enabled == null) seen.copy(trusted = false) else seen
    }

    /** The tunnel is gone: what the last frame said no longer describes anything. */
    fun resetHealth() {
        mutableHealth.value = HealthSignal.None
    }

    private suspend fun loadServer(id: Long): ServerSummary? {
        if (id <= 0) return null
        val r = session.call { profileGetProfile(GetProfileRequest(id)) }
        return (r as? CoreResult.Ok)?.value?.let(ServerSummary::of)
    }

    /** Every server id, for "check servers"; empty when the core cannot be asked. */
    suspend fun profileIds(): List<Long> =
        (session.call { profileListProfiles(ListProfilesRequest()) } as? CoreResult.Ok)?.value?.profiles?.map { it.id }.orEmpty()

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

        suspend fun readHealthSetting(session: CoreSession): Boolean? {
            val r = session.call { settingsGetSettings(GetSettingsRequest(listOf(SettingKeys.HEALTH_CHECK))) }
            val value = (r as? CoreResult.Ok)?.value?.values?.get(SettingKeys.HEALTH_CHECK) ?: return null
            return value.boolValue
        }
    }
}
