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
import io.github.anatolijlaptev1991_ctrl.lavray.ipc.VpnStatus
import io.github.anatolijlaptev1991_ctrl.lavray.ui.core.CoreResult
import io.github.anatolijlaptev1991_ctrl.lavray.ui.core.CoreSession
import io.github.anatolijlaptev1991_ctrl.lavray.ui.core.ErrorKind
import io.github.anatolijlaptev1991_ctrl.lavray.ui.core.StreamFailure
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
import kotlinx.coroutines.flow.update
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
 * Loads the current server, active route preset and start time for the connection screen, and follows the service's
 * status for the tunnel's health ([health], [healthBasis]). [traffic] is a cold stream: it opens the stats socket when
 * collected and closes it when the collector goes away, so it only runs while the screen is visible and the tunnel is up.
 * The status itself is not a core stream: the service delivers it to the app anyway, following it costs nothing.
 */
class ConnectionModel(
    private val session: CoreSession,
    private val scope: CoroutineScope,
    /** The service's status; its `healthError` is the health source (see [HealthSignal]). */
    status: StateFlow<VpnStatus>,
    /** The "check the link after connecting" setting as it is now; null when it cannot be read. */
    private val healthCheckReader: suspend () -> Boolean? = { readHealthSetting(session) },
    /** When the core started the tunnel it runs now (epoch millis); null when it cannot be read. */
    private val startedAtReader: suspend () -> Long? = { readStartedAt(session) },
    private val clock: () -> Long = System::currentTimeMillis,
    /** What survives the Activity: closed notices and the basis of the tunnel that runs on. */
    private val memory: TunnelMemory = TunnelMemory(),
) {
    private val mutable = MutableStateFlow(ConnectionInfo())
    val info: StateFlow<ConnectionInfo> = mutable.asStateFlow()

    private val mutableHealth = MutableStateFlow(HealthSignal.None)

    /** The health the service reports for the tunnel it shows now; [HealthSignal.None] while no tunnel is up. */
    val health: StateFlow<HealthSignal> = mutableHealth.asStateFlow()

    private val mutableBasis = MutableStateFlow<HealthBasis?>(null)

    /** What is known about the health check of the tunnel that [health] describes; null until a tunnel was seen connected. */
    val healthBasis: StateFlow<HealthBasis?> = mutableBasis.asStateFlow()

    @Volatile private var sight = TunnelSight()

    /** True from the start until this model's first tunnel is identified: an earlier closing of the notes is taken to be about it. */
    @Volatile private var closingPresumed = false

    /** Notices the person closed; each is forgotten when its cause ends, so the next one is shown again. Kept in [memory]. */
    val dismissed: StateFlow<Dismissed> = memory.dismissed

    fun dismissStop() = memory.update { it.withStop() }

    fun dismissFailure(kind: String) = memory.update { it.withFailure(kind) }

    /** The core's notes are shown once per connection: this closes them for the tunnel that is up now. */
    fun dismissWarnings() {
        closingPresumed = false
        memory.closeWarningsFor(mutableBasis.value?.takeIf { it.tunnel == sight.tunnel }?.startedAtMs)
        memory.update { it.withWarnings(sight.tunnel) }
    }

    init {
        // This model numbers its tunnels from 1 again: a closing recorded under the numbers of an earlier screen means nothing
        // here. The notes of the SAME core run are found again by its start time when its basis is taken. That takes a call to
        // the core, so until it has answered, a closing that exists is taken to be about this model's first tunnel: the notes
        // the person closed do not flash on a recreated screen. If the tunnel turns out to be another core run, [onStatus]
        // takes the presumption back and its notes show.
        closingPresumed = memory.warningsClosedFor != null
        memory.update { it.withWarnings(if (closingPresumed) FIRST_TUNNEL else 0) }
        scope.launch { status.collect(::onStatus) }
    }

    /**
     * One status of the service. A tunnel coming up fixes its [healthBasis] right then (the core reads the setting when it
     * connects); the signal always follows the latest status.
     */
    private fun onStatus(status: VpnStatus) {
        val before = sight
        sight = before.after(status)
        mutableHealth.value = HealthSignal.of(status, sight.tunnel)
        memory.update { it.after(status) }
        if (sight.tunnel != before.tunnel) {
            val tunnel = sight.tunnel
            scope.launch {
                val basis = basisFor(tunnel)
                // A newer tunnel may have come up while the readings were under way: they describe the older one.
                if (sight.tunnel == tunnel) {
                    memory.remember(basis)
                    // The same core run as the one whose notes were closed (the Activity was recreated): they stay closed.
                    // Before the basis is published: whoever sees the basis of this tunnel must find its notes already
                    // closed, or the closed banner shows for a moment (and a test that waits for the basis fails by chance).
                    if (basis.startedAtMs != null && basis.startedAtMs == memory.warningsClosedFor) {
                        memory.update { it.withWarnings(tunnel) }
                    } else if (closingPresumed && tunnel == FIRST_TUNNEL) {
                        memory.update { if (it.warningsOfTunnel == FIRST_TUNNEL) it.withWarnings(0) else it }
                    }
                    closingPresumed = false
                    mutableBasis.value = basis
                }
            }
        }
    }

    private suspend fun basisFor(tunnel: Int): HealthBasis {
        val startedAt = startedAtReader()
        // The same core run seen again by a new Activity: what was found out about it stays true, and it is not "first seen
        // long after it started" just because this screen is new.
        memory.basisFor(startedAt, tunnel)?.let { return it }
        return HealthBasis.firstSight(tunnel, startedAt, healthCheckReader(), clock())
    }

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

        /** The number [TunnelSight] gives the first tunnel a model sees. */
        const val FIRST_TUNNEL = 1

        suspend fun readStartedAt(session: CoreSession): Long? =
            (session.call { coreGetStatus() } as? CoreResult.Ok)?.value?.startedAtMs?.takeIf { it > 0 }

        suspend fun readHealthSetting(session: CoreSession): Boolean? {
            val r = session.call { settingsGetSettings(GetSettingsRequest(listOf(SettingKeys.HEALTH_CHECK))) }
            val value = (r as? CoreResult.Ok)?.value?.values?.get(SettingKeys.HEALTH_CHECK) ?: return null
            return value.boolValue
        }
    }
}
