// SPDX-License-Identifier: GPL-3.0-only
package io.github.anatolijlaptev1991_ctrl.lavray.ui.screens.connection

import io.github.anatolijlaptev1991_ctrl.lavray.api.gen.DnsSettings
import io.github.anatolijlaptev1991_ctrl.lavray.api.gen.LeakReport
import io.github.anatolijlaptev1991_ctrl.lavray.api.gen.TestLatencyRequest
import io.github.anatolijlaptev1991_ctrl.lavray.api.gen.diagnosticsCheckLeaks
import io.github.anatolijlaptev1991_ctrl.lavray.api.gen.dnsGetDns
import io.github.anatolijlaptev1991_ctrl.lavray.api.gen.profileTestLatency
import io.github.anatolijlaptev1991_ctrl.lavray.ui.core.CoreResult
import io.github.anatolijlaptev1991_ctrl.lavray.ui.core.CoreSession
import io.github.anatolijlaptev1991_ctrl.lavray.ui.core.ErrorKind
import io.github.anatolijlaptev1991_ctrl.lavray.ui.core.StreamFailure
import io.github.anatolijlaptev1991_ctrl.lavray.ui.screens.servers.LatencyCell
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.async
import kotlinx.coroutines.coroutineScope
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.sync.Mutex

/** The three one-shot reads of a pass. A fake stands in for them in tests. */
interface ProbeBackend {
    suspend fun leaks(): CoreResult<LeakReport>
    suspend fun dns(): CoreResult<DnsSettings>

    /** Round trip to the server of [profileId]; null when the test could not run at all. */
    suspend fun latency(profileId: Long): LatencyCell?
}

class CoreProbeBackend(private val session: CoreSession) : ProbeBackend {
    override suspend fun leaks(): CoreResult<LeakReport> = session.call { diagnosticsCheckLeaks() }

    override suspend fun dns(): CoreResult<DnsSettings> = session.call { dnsGetDns() }

    override suspend fun latency(profileId: Long): LatencyCell? {
        var cell: LatencyCell? = null
        try {
            session.stream { profileTestLatency(TestLatencyRequest(profileIds = listOf(profileId), mode = TestLatencyRequest.Mode.MODE_TCP_PING)) }
                .collect { r -> cell = LatencyCell.from(r.result?.latencyMs, r.result?.lastError) }
        } catch (_: StreamFailure) {
            return null
        }
        return cell
    }
}

/** Where a pass stands. Every state carries the connection it belongs to, so another connection never shows it. */
sealed interface ProbeState {
    val key: String?

    data object Idle : ProbeState {
        override val key: String? = null
    }

    /** A pass is running; [previous] is the finished report of the same connection that a manual refresh replaces. */
    data class Running(override val key: String, val previous: ExitReport?) : ProbeState

    data class Ready(val report: ExitReport) : ProbeState {
        override val key: String get() = report.key
    }

    /** The pass could not be completed. This is a quiet "could not check", not an alarm: the tunnel itself is unaffected. */
    data class Failed(override val key: String, val kind: ErrorKind) : ProbeState
}

/** A connection is told apart by its server and its start time: a reconnect makes a new key and so a new pass. */
fun probeKey(profileId: Long, startedAtMs: Long): String? =
    if (profileId > 0 && startedAtMs > 0) "$profileId:$startedAtMs" else null

/**
 * Exit address, leak check, delay and DNS of the running tunnel, asked once when the tunnel has come up and the
 * screen is visible, and kept for that connection. There is no timer: a refresh happens only when the person asks, or
 * when the screen is shown again after a pass that failed or was cut short. A pass that is cancelled (the screen
 * stopped, the tunnel went down) puts the state back as it was, so a half answer is never shown.
 */
class ExitProbe(
    private val backend: ProbeBackend,
    /**
     * False while another latency batch of the core is running (the Servers screen's "test all"). The core runs one batch at
     * a time per server and a newer batch cuts the older one short, so the probe SKIPS its delay measurement then (the report
     * shows no delay) instead of cutting the person's test; it does not queue behind a test that can take minutes.
     */
    private val canMeasureLatency: () -> Boolean = { true },
    private val clock: () -> Long = System::currentTimeMillis,
) {
    private val mutable = MutableStateFlow<ProbeState>(ProbeState.Idle)
    private val gate = Mutex()

    val state: StateFlow<ProbeState> = mutable.asStateFlow()

    /** Runs a pass unless this connection already has a finished one. Call it only while the screen is visible. */
    suspend fun ensure(key: String, profileId: Long) {
        val current = mutable.value
        if (current is ProbeState.Ready && current.key == key) return
        run(key, profileId)
    }

    /** The manual refresh. A pass that is already running is not doubled. */
    suspend fun refresh(key: String, profileId: Long) = run(key, profileId)

    /** The tunnel is down: nothing of the finished connection is worth keeping. */
    fun reset() {
        if (!gate.isLocked) mutable.value = ProbeState.Idle
    }

    private suspend fun run(key: String, profileId: Long) {
        if (!gate.tryLock()) return
        val before = mutable.value
        val keep = (before as? ProbeState.Ready)?.report?.takeIf { it.key == key }
        try {
            mutable.value = ProbeState.Running(key, keep)
            mutable.value = pass(key, profileId)
        } catch (e: CancellationException) {
            mutable.value = if (keep != null) ProbeState.Ready(keep) else ProbeState.Idle
            throw e
        } finally {
            gate.unlock()
        }
    }

    private suspend fun pass(key: String, profileId: Long): ProbeState = coroutineScope {
        val leaks = async { backend.leaks() }
        val dns = async { backend.dns() }
        val latency = async { if (canMeasureLatency()) backend.latency(profileId) else null }
        when (val l = leaks.await()) {
            is CoreResult.Fail -> {
                dns.cancel()
                latency.cancel()
                ProbeState.Failed(key, l.kind)
            }
            is CoreResult.Ok -> {
                val settings = (dns.await() as? CoreResult.Ok)?.value
                ProbeState.Ready(ExitReport.of(key, clock(), l.value, settings, latency.await()))
            }
        }
    }
}
