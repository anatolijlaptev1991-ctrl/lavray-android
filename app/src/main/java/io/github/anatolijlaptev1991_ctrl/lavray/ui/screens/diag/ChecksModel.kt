// SPDX-License-Identifier: GPL-3.0-only
package io.github.anatolijlaptev1991_ctrl.lavray.ui.screens.diag

import androidx.compose.runtime.Immutable
import io.github.anatolijlaptev1991_ctrl.lavray.api.gen.CheckRouteRequest
import io.github.anatolijlaptev1991_ctrl.lavray.api.gen.GetPublicIpRequest
import io.github.anatolijlaptev1991_ctrl.lavray.api.gen.RunSelfCheckRequest
import io.github.anatolijlaptev1991_ctrl.lavray.api.gen.diagnosticsCheckLeaks
import io.github.anatolijlaptev1991_ctrl.lavray.api.gen.diagnosticsCheckRoute
import io.github.anatolijlaptev1991_ctrl.lavray.api.gen.diagnosticsGetPublicIp
import io.github.anatolijlaptev1991_ctrl.lavray.api.gen.diagnosticsRunSelfCheck
import io.github.anatolijlaptev1991_ctrl.lavray.ui.core.CoreResult
import io.github.anatolijlaptev1991_ctrl.lavray.ui.core.CoreSession
import io.github.anatolijlaptev1991_ctrl.lavray.ui.core.ErrorKind
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.Job
import kotlinx.coroutines.async
import kotlinx.coroutines.coroutineScope
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.flow.update
import kotlinx.coroutines.launch

/** One probe of the public address: nothing yet, running, an address, or why there is none. */
sealed interface IpReading {
    data object Idle : IpReading
    data object Loading : IpReading
    data class Value(val ip: String, val ipv6: Boolean) : IpReading
    data class Failed(val kind: ErrorKind) : IpReading
}

@Immutable
data class ChecksState(
    val rows: List<CheckRow> = emptyList(),
    val running: Boolean = false,
    val summary: CheckSummary? = null,
    val error: ErrorKind? = null,
    val direct: IpReading = IpReading.Idle,
    val tunnel: IpReading = IpReading.Idle,
    val leak: LeakVerdict? = null,
    val leakRunning: Boolean = false,
    val leakError: ErrorKind? = null,
    val routing: Boolean = false,
    val route: RouteAnswer? = null,
    val routeError: ErrorKind? = null,
)

/**
 * The diagnostics calls of the Checks screen. Every call is a one-shot unary request on the IO dispatcher; nothing
 * here polls, and a run that is replaced by a newer one is cancelled, so a slow answer never overwrites a fresh one.
 */
class ChecksModel(private val session: CoreSession, private val scope: CoroutineScope) {
    private val mutable = MutableStateFlow(ChecksState())
    private var checkJob: Job? = null
    private var ipJob: Job? = null
    private var leakJob: Job? = null
    private var routeJob: Job? = null

    val state: StateFlow<ChecksState> = mutable.asStateFlow()

    fun runChecks() {
        checkJob?.cancel()
        mutable.update { it.copy(rows = ChecksMapper.pending(), running = true, summary = null, error = null) }
        checkJob = scope.launch(Dispatchers.IO) {
            when (val r = session.call { diagnosticsRunSelfCheck(RunSelfCheckRequest()) }) {
                is CoreResult.Ok -> {
                    val rows = ChecksMapper.rows(r.value.results)
                    mutable.update { it.copy(rows = rows, running = false, summary = ChecksMapper.summary(rows)) }
                }
                is CoreResult.Fail -> mutable.update { it.copy(rows = emptyList(), running = false, error = r.kind) }
            }
        }
    }

    /** Both addresses at once; the tunnel one is skipped by the caller while disconnected. */
    fun probeAddresses(tunnelUp: Boolean) {
        ipJob?.cancel()
        mutable.update { it.copy(direct = IpReading.Loading, tunnel = if (tunnelUp) IpReading.Loading else IpReading.Idle) }
        ipJob = scope.launch(Dispatchers.IO) {
            coroutineScope {
                val direct = async { probe(GetPublicIpRequest.Via.VIA_DIRECT) }
                val tunnel = if (tunnelUp) async { probe(GetPublicIpRequest.Via.VIA_THROUGH_TUNNEL) } else null
                val d = direct.await()
                mutable.update { it.copy(direct = d) }
                tunnel?.await()?.let { t -> mutable.update { it.copy(tunnel = t) } }
            }
        }
    }

    private suspend fun probe(via: GetPublicIpRequest.Via): IpReading =
        when (val r = session.call { diagnosticsGetPublicIp(GetPublicIpRequest(via)) }) {
            is CoreResult.Ok -> r.value.ip?.takeIf { it.isNotBlank() }?.let { IpReading.Value(it, r.value.isIpv6) }
                ?: IpReading.Failed(ErrorKind.Generic)
            is CoreResult.Fail -> IpReading.Failed(r.kind)
        }

    fun checkLeaks() {
        leakJob?.cancel()
        mutable.update { it.copy(leakRunning = true, leakError = null) }
        leakJob = scope.launch(Dispatchers.IO) {
            when (val r = session.call { diagnosticsCheckLeaks() }) {
                is CoreResult.Ok -> mutable.update { it.copy(leak = LeakVerdict.of(r.value), leakRunning = false) }
                is CoreResult.Fail -> mutable.update { it.copy(leakRunning = false, leakError = r.kind) }
            }
        }
    }

    fun checkRoute(input: String) {
        val address = RouteMapper.normalize(input) ?: return
        routeJob?.cancel()
        mutable.update { it.copy(routing = true, route = null, routeError = null) }
        routeJob = scope.launch(Dispatchers.IO) {
            when (val r = session.call { diagnosticsCheckRoute(CheckRouteRequest(address = address)) }) {
                is CoreResult.Ok -> {
                    val answer = RouteAnswer(address, RouteMapper.outboundName(r.value.outbound), r.value.matchedRule)
                    mutable.update { it.copy(routing = false, route = answer) }
                }
                is CoreResult.Fail -> mutable.update { it.copy(routing = false, routeError = r.kind) }
            }
        }
    }

    /** Leaving the screen drops what is still running; the finished answers stay for the way back. */
    fun cancelRunning() {
        checkJob?.cancel()
        ipJob?.cancel()
        leakJob?.cancel()
        routeJob?.cancel()
        val loading = IpReading.Loading
        mutable.update { s ->
            s.copy(
                rows = if (s.rows.any { it.state == RowState.Pending }) emptyList() else s.rows,
                running = false, routing = false, leakRunning = false,
                direct = if (s.direct == loading) IpReading.Idle else s.direct,
                tunnel = if (s.tunnel == loading) IpReading.Idle else s.tunnel,
            )
        }
    }
}
