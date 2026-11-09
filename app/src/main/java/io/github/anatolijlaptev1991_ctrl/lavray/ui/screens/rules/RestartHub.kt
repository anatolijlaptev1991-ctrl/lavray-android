// SPDX-License-Identifier: GPL-3.0-only
package io.github.anatolijlaptev1991_ctrl.lavray.ui.screens.rules

import androidx.compose.runtime.Immutable
import io.github.anatolijlaptev1991_ctrl.lavray.api.gen.coreRestartProfile
import io.github.anatolijlaptev1991_ctrl.lavray.ui.core.CoreResult
import io.github.anatolijlaptev1991_ctrl.lavray.ui.core.CoreSession
import io.github.anatolijlaptev1991_ctrl.lavray.ui.core.ErrorKind
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.flow.MutableSharedFlow
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.SharedFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asSharedFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.launch

@Immutable
data class RestartState(val needed: Boolean = false, val busy: Boolean = false)

sealed interface RestartEvent {
    data object Done : RestartEvent
    data class Failed(val kind: ErrorKind) : RestartEvent
}

/**
 * "The change applies after a core restart" for routes and DNS together: the core says when a change needs
 * it (needs_profile_restart), the banner offers the restart, and RestartProfile does it. Lives as long as the
 * app, so the banner is still there after a switch between the Routing and DNS tabs.
 */
class RestartHub(private val session: CoreSession, private val scope: CoroutineScope) {
    private val mutable = MutableStateFlow(RestartState())
    private val mutableEvents = MutableSharedFlow<RestartEvent>(extraBufferCapacity = 4)

    val state: StateFlow<RestartState> = mutable.asStateFlow()
    val events: SharedFlow<RestartEvent> = mutableEvents.asSharedFlow()

    fun need() {
        mutable.value = mutable.value.copy(needed = true)
    }

    /** "Later", or the tunnel went down: the next connect starts with the new settings anyway. */
    fun clear() {
        if (mutable.value.needed && !mutable.value.busy) mutable.value = mutable.value.copy(needed = false)
    }

    /** Restarts the running profile. The banner goes away only when the core accepted it. */
    fun restart() {
        if (mutable.value.busy) return
        mutable.value = mutable.value.copy(busy = true)
        scope.launch(Dispatchers.IO) {
            val r = session.call { coreRestartProfile() }
            mutable.value = RestartState(needed = r is CoreResult.Fail, busy = false)
            mutableEvents.tryEmit(if (r is CoreResult.Fail) RestartEvent.Failed(r.kind) else RestartEvent.Done)
        }
    }
}
