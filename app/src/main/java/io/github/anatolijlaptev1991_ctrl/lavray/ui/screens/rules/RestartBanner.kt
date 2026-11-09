// SPDX-License-Identifier: GPL-3.0-only
package io.github.anatolijlaptev1991_ctrl.lavray.ui.screens.rules

import androidx.compose.foundation.layout.padding
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.ui.Modifier
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.unit.dp
import androidx.lifecycle.Lifecycle
import androidx.lifecycle.compose.LocalLifecycleOwner
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import androidx.lifecycle.repeatOnLifecycle
import io.github.anatolijlaptev1991_ctrl.lavray.R
import io.github.anatolijlaptev1991_ctrl.lavray.ui.LocalEnv
import io.github.anatolijlaptev1991_ctrl.lavray.ui.core.textRes
import io.github.anatolijlaptev1991_ctrl.lavray.ui.design.LavIcon
import io.github.anatolijlaptev1991_ctrl.lavray.ui.design.components.ButtonVariant
import io.github.anatolijlaptev1991_ctrl.lavray.ui.design.components.LavBanner
import io.github.anatolijlaptev1991_ctrl.lavray.ui.design.components.LavButton
import io.github.anatolijlaptev1991_ctrl.lavray.ui.design.components.StatusTone
import io.github.anatolijlaptev1991_ctrl.lavray.ui.screens.connection.Phase
import io.github.anatolijlaptev1991_ctrl.lavray.ui.screens.connection.phaseOf

/**
 * "The change applies after a core restart" with the restart and "later" buttons. Shown only while a tunnel
 * is up: with none, the next connect already uses the new settings, so the request is dropped. Toasts report
 * the outcome of the restart while the screen is on show.
 */
@Composable
fun RestartBanner(modifier: Modifier = Modifier) {
    val env = LocalEnv.current
    val hub = env.restart
    val state by hub.state.collectAsStateWithLifecycle()
    val vpn by env.vpn.status.collectAsStateWithLifecycle()
    val phase = phaseOf(vpn.state)
    val live = phase == Phase.On || phase == Phase.Connecting
    LaunchedEffect(live) { if (!live) hub.clear() }
    RestartToasts(hub)
    if (!state.needed || !live) return
    LavBanner(
        tone = StatusTone.Connecting,
        title = stringResource(R.string.dns_restart),
        modifier = modifier.padding(horizontal = 16.dp, vertical = 8.dp),
        actions = {
            LavButton(
                stringResource(R.string.dns_restartBtn), { hub.restart() },
                variant = ButtonVariant.Primary, icon = LavIcon.Refresh, loading = state.busy,
            )
            LavButton(stringResource(R.string.dns_restartLater), { hub.clear() }, variant = ButtonVariant.Ghost, enabled = !state.busy)
        },
    )
}

@Composable
private fun RestartToasts(hub: RestartHub) {
    val env = LocalEnv.current
    val owner = LocalLifecycleOwner.current
    LaunchedEffect(hub) {
        owner.lifecycle.repeatOnLifecycle(Lifecycle.State.STARTED) {
            hub.events.collect { e ->
                val ctx = env.appContext
                env.toast.show(
                    when (e) {
                        RestartEvent.Done -> ctx.getString(R.string.toast_coreRestarted)
                        is RestartEvent.Failed -> ctx.getString(e.kind.textRes())
                    },
                )
            }
        }
    }
}
