// SPDX-License-Identifier: GPL-3.0-only
package io.github.anatolijlaptev1991_ctrl.lavray.ui.screens.connection

import androidx.annotation.StringRes
import androidx.compose.runtime.Composable
import androidx.compose.runtime.Immutable
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

/**
 * Why a connection attempt failed, as far as the service says. [kind] is the service's failure code (null until the
 * service reports one); [raw] is its short text, which is untranslated and may name addresses. The screen words the
 * kind, and shows [raw] only as a sanitised secondary line ([ErrorDetail]).
 */
@Immutable
data class FailureInfo(val kind: String?, val raw: String?) {
    companion object {
        val None = FailureInfo(null, null)
    }
}

/** The line under the state title. Pure, so the wording of each state is tested. */
@StringRes
fun stateSubRes(phase: Phase, link: LinkHealth, failure: FailureInfo = FailureInfo.None): Int = when (phase) {
    Phase.Off, Phase.WaitingNetwork -> R.string.conn_state_off_sub
    Phase.Connecting, Phase.Disconnecting -> R.string.conn_state_connecting_sub
    Phase.On -> when (link) {
        LinkHealth.Verified -> R.string.conn_state_on_sub
        LinkHealth.Checking -> R.string.cx_health_checking
        is LinkHealth.NoTraffic -> R.string.cx_health_failed_sub
        // Not asked, or the core has not answered: nothing is claimed about the traffic.
        LinkHealth.NotChecked, LinkHealth.Unknown -> R.string.cx_health_unknown_sub
    }
    Phase.NeedsPermission, Phase.Error -> ErrorDetail.kindText(failure.kind) ?: R.string.conn_state_err_sub
}

/** The mark: a tunnel that is up but passes nothing is not drawn as healthy. */
fun markFor(phase: Phase, link: LinkHealth): MarkState =
    if (phase == Phase.On && link is LinkHealth.NoTraffic) MarkState.Error else phase.mark

/** Words for a failed attempt: known failure kinds have dictionary text, the raw detail is a cleaned secondary line. */
object ErrorDetail {
    const val MAX = 160

    /** Failure codes of the service (docs/ARCHITECTURE.md, "service status for the interface") and their dictionary text. */
    @StringRes
    fun kindText(kind: String?): Int? = when (kind) {
        "no_server" -> R.string.fail_no_server
        "core_start" -> R.string.fail_core_start
        "apps_none" -> R.string.fail_apps_none
        "permission" -> R.string.fail_permission
        "connect_failed" -> R.string.fail_connect_failed
        "restricted_start" -> R.string.fail_restricted_start
        "tunnel_lost" -> R.string.fail_tunnel_lost
        "protection_off" -> R.string.fail_protection_off
        else -> null
    }

    private val ipv6InBrackets = Regex("""\[[0-9A-Fa-f:.]+](:\d{1,5})?""")
    private val ipv6Bare = Regex("""(?<![\w:])(?:[0-9A-Fa-f]{0,4}:){2,7}[0-9A-Fa-f]{0,4}(?![\w:])""")
    private val ipv4 = Regex("""\b\d{1,3}(?:\.\d{1,3}){3}(?::\d{1,5})?\b""")
    private val host = Regex("""\b[A-Za-z0-9-]+(?:\.[A-Za-z0-9-]+)+(?::\d{1,5})?\b""")

    /**
     * One short line from the service's raw text: whitespace collapsed, control characters dropped, addresses, host names
     * and ports replaced by "…" (the text can name the server), cut to [MAX]. Null when nothing is left.
     */
    fun clean(error: String?): String? {
        val flat = error.orEmpty().replace(Regex("\\s+"), " ").filter { !it.isISOControl() }.trim()
        val masked = flat
            .replace(ipv6InBrackets, "…").replace(ipv4, "…").replace(ipv6Bare, "…").replace(host, "…")
            .trim()
        if (masked.isEmpty() || masked.all { it == '…' || it == ' ' }) return null
        return if (masked.length <= MAX) masked else masked.take(MAX - 1) + "…"
    }
}

/**
 * What the core says about traffic through the server, for as long as the tunnel is up. The check is the core's own
 * (connect_health.go): a failure arrives as an event, success as silence, so "verified" is a verdict after the window
 * in which a failure could still arrive, and only for a tunnel whose basis is known ([HealthRules.of]). One timer, set
 * once per tunnel, not a loop.
 */
@Composable
fun rememberLinkHealth(signal: HealthSignal, basis: HealthBasis?): LinkHealth {
    var tick by remember { mutableIntStateOf(0) }
    LaunchedEffect(signal, basis) {
        val wait = HealthRules.msUntilVerdict(signal, basis, System.currentTimeMillis())
        if (wait > 0) {
            delay(wait)
            tick++
        }
    }
    return remember(signal, basis, tick) { HealthRules.of(signal, basis, System.currentTimeMillis()) }
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
