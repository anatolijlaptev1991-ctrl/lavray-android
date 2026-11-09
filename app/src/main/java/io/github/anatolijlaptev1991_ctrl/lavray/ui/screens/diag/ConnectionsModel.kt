// SPDX-License-Identifier: GPL-3.0-only
package io.github.anatolijlaptev1991_ctrl.lavray.ui.screens.diag

import io.github.anatolijlaptev1991_ctrl.lavray.api.gen.CloseConnectionRequest
import io.github.anatolijlaptev1991_ctrl.lavray.api.gen.WatchConnectionsRequest
import io.github.anatolijlaptev1991_ctrl.lavray.api.gen.connectionCloseConnection
import io.github.anatolijlaptev1991_ctrl.lavray.api.gen.connectionWatchConnections
import io.github.anatolijlaptev1991_ctrl.lavray.ui.core.CoreResult
import io.github.anatolijlaptev1991_ctrl.lavray.ui.core.CoreSession
import io.github.anatolijlaptev1991_ctrl.lavray.ui.core.ErrorKind
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.flow.map
import kotlinx.coroutines.flow.update
import kotlinx.coroutines.withContext

/**
 * Live connections. [watch] suspends for as long as the screen is visible and the tunnel is up: the caller runs it
 * inside a lifecycle-scoped coroutine, and cancelling that coroutine closes the socket to the core. Frames are
 * mapped and reduced on the IO dispatcher of the client, never on the main thread.
 */
class ConnectionsModel(private val session: CoreSession) {
    private val mutable = MutableStateFlow(ConnsState())
    private val mutableError = MutableStateFlow<ErrorKind?>(null)

    val state: StateFlow<ConnsState> = mutable.asStateFlow()
    val streamError: StateFlow<ErrorKind?> = mutableError.asStateFlow()

    suspend fun watch() = withContext(Dispatchers.Default) {
        StreamBackoff.run(
            open = {
                session.stream { connectionWatchConnections(WatchConnectionsRequest(intervalMs = INTERVAL_MS, activePageLimit = ACTIVE_LIMIT)) }
                    .map { snap -> ConnFrame(snap.active.map(ConnRow::from), snap.closedIds.toSet(), snap.truncated) }
            },
            onError = { mutableError.value = it },
            onFrame = { frame ->
                mutableError.value = null
                mutable.update { ConnsReducer.reduce(it, ConnsAction.Frame(frame, System.currentTimeMillis())) }
            },
        )
    }

    fun setPaused(paused: Boolean) = mutable.update { ConnsReducer.reduce(it, ConnsAction.Pause(paused)) }

    /** Tunnel down or screen gone: nothing to show, and the history of a finished session is not kept. */
    fun reset() {
        mutable.update { ConnsReducer.reduce(it, ConnsAction.Reset) }
        mutableError.value = null
    }

    /** Closes one connection; the next frame shows it gone. */
    suspend fun close(id: String): CoreResult<Unit> = when (val r = session.call { connectionCloseConnection(CloseConnectionRequest(id)) }) {
        is CoreResult.Ok -> CoreResult.Ok(Unit)
        is CoreResult.Fail -> r
    }

    companion object {
        const val INTERVAL_MS = 1000
        const val ACTIVE_LIMIT = 500
    }
}
