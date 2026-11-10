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

/**
 * "The saved change is not in the running tunnel yet", with the restart and "later" buttons. One for the whole app:
 * the shell shows it under the header on every screen, so a change made on Routes, DNS or Settings is offered the
 * restart wherever the person is. After "later" it stays folded, and the Connection screen shows it again with
 * [showWhenSnoozed] so the running config is never silently different from the saved one.
 */
@Composable
fun RestartBanner(modifier: Modifier = Modifier, showWhenSnoozed: Boolean = false) {
    val hub = LocalEnv.current.restart
    val state by hub.state.collectAsStateWithLifecycle()
    if (!state.needed || (state.snoozed && !showWhenSnoozed)) return
    val reconnect = state.needsReconnect
    val folded = state.snoozed
    LavBanner(
        tone = StatusTone.Connecting,
        title = stringResource(
            when {
                folded -> R.string.cx_pending_t
                reconnect -> R.string.set_reconnect
                else -> R.string.dns_restart
            },
        ),
        modifier = modifier.padding(horizontal = 16.dp, vertical = 8.dp),
        text = if (folded) stringResource(R.string.cx_pending_d) else null,
        actions = {
            LavButton(
                stringResource(if (reconnect) R.string.set_reconnect_now else R.string.dns_restartBtn), { hub.restart() },
                variant = ButtonVariant.Primary, icon = LavIcon.Refresh, loading = state.busy,
            )
            if (!folded) {
                LavButton(stringResource(R.string.dns_restartLater), { hub.postpone() }, variant = ButtonVariant.Ghost, enabled = !state.busy)
            }
        },
    )
}

/** Toasts for the outcome of a restart, shown while the app is in front. Composed once, by the shell. */
@Composable
fun RestartToasts() {
    val env = LocalEnv.current
    val owner = LocalLifecycleOwner.current
    LaunchedEffect(env.restart) {
        owner.lifecycle.repeatOnLifecycle(Lifecycle.State.STARTED) {
            env.restart.events.collect { e ->
                val ctx = env.appContext
                env.toast.show(
                    when (e) {
                        is RestartEvent.Done -> ctx.getString(if (e.tunnelRebuilt) R.string.toast_reconnected else R.string.toast_coreRestarted)
                        is RestartEvent.Failed -> ctx.getString(e.kind.textRes())
                    },
                )
            }
        }
    }
}
