// SPDX-License-Identifier: GPL-3.0-only
package io.github.anatolijlaptev1991_ctrl.lavray.ui.screens.connection

import androidx.annotation.StringRes
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableIntStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.res.stringResource
import io.github.anatolijlaptev1991_ctrl.lavray.R
import io.github.anatolijlaptev1991_ctrl.lavray.ui.design.MarkState
import io.github.anatolijlaptev1991_ctrl.lavray.ui.design.components.ButtonVariant
import io.github.anatolijlaptev1991_ctrl.lavray.ui.design.components.LavBanner
import io.github.anatolijlaptev1991_ctrl.lavray.ui.design.components.LavButton
import io.github.anatolijlaptev1991_ctrl.lavray.ui.design.components.StatusTone
import kotlinx.coroutines.delay

/** The line under the state title. Pure, so the wording of each state is tested. */
@StringRes
fun stateSubRes(phase: Phase, link: LinkHealth): Int = when (phase) {
    Phase.Off, Phase.WaitingNetwork -> R.string.conn_state_off_sub
    Phase.Connecting, Phase.Disconnecting -> R.string.conn_state_connecting_sub
    Phase.On -> when (link) {
        LinkHealth.Verified -> R.string.conn_state_on_sub
        LinkHealth.Checking -> R.string.cx_health_checking
        is LinkHealth.NoTraffic -> R.string.cx_health_failed_sub
        // Not asked, or the core has not answered: nothing is claimed about the traffic.
        LinkHealth.NotChecked, LinkHealth.Unknown -> R.string.cx_health_unknown_sub
    }
    Phase.NeedsPermission, Phase.Error -> R.string.conn_state_err_sub
}

/** The mark: a tunnel that is up but passes nothing is not drawn as healthy. */
fun markFor(phase: Phase, link: LinkHealth): MarkState =
    if (phase == Phase.On && link is LinkHealth.NoTraffic) MarkState.Error else phase.mark

/** A short plain reason for a failed attempt: one line, no control characters, never longer than [MAX]. */
object ErrorDetail {
    const val MAX = 160

    fun clean(error: String?): String? {
        val text = error.orEmpty().replace(Regex("\\s+"), " ").filter { !it.isISOControl() }.trim()
        if (text.isEmpty()) return null
        return if (text.length <= MAX) text else text.take(MAX - 1) + "…"
    }
}

/**
 * What the core says about traffic through the server, for as long as the tunnel is up. The check is the core's own
 * (connect_health.go): a failure arrives as an event, success as silence, so "verified" is a verdict after the window
 * in which a failure could still arrive. One timer, set once per connection, not a loop.
 */
@Composable
fun rememberLinkHealth(signal: HealthSignal, checkEnabled: Boolean): LinkHealth {
    var tick by remember { mutableIntStateOf(0) }
    LaunchedEffect(signal, checkEnabled) {
        val wait = HealthRules.msUntilVerdict(signal, System.currentTimeMillis())
        if (signal.connected && signal.healthError == null && checkEnabled && wait > 0) {
            delay(wait)
            tick++
        }
    }
    return remember(signal, checkEnabled, tick) { HealthRules.of(signal, checkEnabled, System.currentTimeMillis()) }
}

/** "Connected, but nothing gets through the server: <reason>", with the way out: another server. */
@Composable
fun NoTrafficBanner(reason: String, onChooseServer: () -> Unit) {
    val shown = HealthRules.reasonText(reason)?.let { stringResource(it) } ?: reason
    LavBanner(
        StatusTone.Connecting, stringResource(R.string.conn_noTraffic, shown),
        actions = { LavButton(stringResource(R.string.srv_change), onChooseServer, variant = ButtonVariant.Primary) },
    )
}
