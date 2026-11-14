// SPDX-License-Identifier: GPL-3.0-only
package io.github.anatolijlaptev1991_ctrl.lavray.ui.screens.connection

import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.BoxWithConstraints
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.verticalScroll
import androidx.compose.runtime.Composable
import androidx.compose.runtime.Immutable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.semantics.LiveRegionMode
import androidx.compose.ui.semantics.liveRegion
import androidx.compose.ui.semantics.semantics
import androidx.compose.ui.text.style.TextAlign
import androidx.compose.ui.unit.dp
import androidx.lifecycle.Lifecycle
import androidx.lifecycle.compose.LocalLifecycleOwner
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import androidx.lifecycle.repeatOnLifecycle
import io.github.anatolijlaptev1991_ctrl.lavray.R
import io.github.anatolijlaptev1991_ctrl.lavray.ui.LocalEnv
import io.github.anatolijlaptev1991_ctrl.lavray.ui.core.textRes
import io.github.anatolijlaptev1991_ctrl.lavray.ui.design.Lav
import io.github.anatolijlaptev1991_ctrl.lavray.ui.design.LavIcon
import io.github.anatolijlaptev1991_ctrl.lavray.ui.design.LavText
import io.github.anatolijlaptev1991_ctrl.lavray.ui.design.LightField
import io.github.anatolijlaptev1991_ctrl.lavray.ui.design.ReflectionMark
import io.github.anatolijlaptev1991_ctrl.lavray.ui.design.components.ButtonVariant
import io.github.anatolijlaptev1991_ctrl.lavray.ui.design.components.LavBanner
import io.github.anatolijlaptev1991_ctrl.lavray.ui.design.components.LavButton
import io.github.anatolijlaptev1991_ctrl.lavray.ui.design.components.LavCard
import io.github.anatolijlaptev1991_ctrl.lavray.ui.design.components.LavEmptyState
import io.github.anatolijlaptev1991_ctrl.lavray.ui.design.components.StatusTone
import io.github.anatolijlaptev1991_ctrl.lavray.ui.importer.ImportTab
import io.github.anatolijlaptev1991_ctrl.lavray.ui.nav.LocalNavigator
import io.github.anatolijlaptev1991_ctrl.lavray.ui.screens.rules.RestartBanner
import kotlinx.coroutines.launch

/**
 * Connection: the mark in its state, connect or disconnect, the current server, the active route preset and
 * live speed with session traffic. The VPN state arrives from the service; the stats stream is subscribed
 * only while this screen is started and the tunnel is up, so a hidden screen holds no socket and no timer.
 */
@Composable
fun ConnectionScreen() {
    val env = LocalEnv.current
    val nav = LocalNavigator.current
    val model = env.connection
    val status by env.vpn.status.collectAsStateWithLifecycle()
    val info by model.info.collectAsStateWithLifecycle()
    val phase = phaseOf(status.state)
    val connect = rememberVpnConnect()

    val dataVersion by env.data.version.collectAsStateWithLifecycle()
    LaunchedEffect(phase, env.settings.selectedProfileId, dataVersion) { model.refresh(env.settings.selectedProfileId) }

    val traffic by remember(phase.isLive) {
        if (phase.isLive) model.traffic() else kotlinx.coroutines.flow.flowOf(TrafficReading.Empty)
    }.collectAsStateWithLifecycle(TrafficReading.Empty)

    val onConnect = { connect(info.server?.id ?: 0L) }
    val owner = LocalLifecycleOwner.current
    val scope = rememberCoroutineScope()
    val probe = env.exit
    val probeState by probe.state.collectAsStateWithLifecycle()
    val serverId = info.server?.id ?: 0L
    val key = if (phase.isLive) probeKey(serverId, info.startedAtMs) else null
    // One pass per connection, only while this screen is visible: leaving the screen or dropping the tunnel cancels it.
    LaunchedEffect(key) {
        if (key == null) {
            probe.reset()
            return@LaunchedEffect
        }
        owner.lifecycle.repeatOnLifecycle(Lifecycle.State.STARTED) { probe.ensure(key, serverId) }
    }
    val onRefresh: () -> Unit = { if (key != null) scope.launch { probe.refresh(key, serverId) } }
    val quick = rememberQuickActions(info, scope)

    // The core's own word on traffic through the server, as the service reports it in its status (no stream of ours).
    val healthSignal by model.health.collectAsStateWithLifecycle()
    val healthBasis by model.healthBasis.collectAsStateWithLifecycle()
    val link = rememberLinkHealth(healthSignal, healthBasis)
    // What else the service says: failure words, a stop nobody asked for, held traffic, the core's notes.
    val dismissed by model.dismissed.collectAsStateWithLifecycle()
    val notificationsOn = rememberNotificationsOn()
    val notices = remember(status, phase, healthSignal.tunnel, dismissed, notificationsOn) {
        ServiceNotices.of(status, phase, healthSignal.tunnel, dismissed, notificationsOn)
    }
    val noticeActions = remember(model, connect, info.server?.id) {
        NoticeActions(
            onConnect = { connect(info.server?.id ?: 0L) },
            onDismissStop = model::dismissStop,
            onDismissFailure = model::dismissFailure,
            onDismissWarnings = model::dismissWarnings,
        )
    }
    // Remembered: a new object on every pass would make the state card recompose with each tick of the traffic card.
    val shown = remember(link, notices, noticeActions, nav) {
        Shown(link, notices, ErrorDetail.detail(notices.failure.kind, notices.failure.raw), noticeActions, onChooseServer = { nav.openSection("servers", "profiles") })
    }

    LightField(markFor(phase, link), Modifier.fillMaxSize()) {
        BoxWithConstraints(Modifier.fillMaxSize()) {
            val wide = maxWidth >= 600.dp
            if (wide) {
                Row(Modifier.fillMaxSize().padding(16.dp), horizontalArrangement = Arrangement.spacedBy(16.dp)) {
                    Column(Modifier.weight(1f).verticalScroll(rememberScrollState()), verticalArrangement = Arrangement.spacedBy(12.dp)) {
                        StateCard(phase, onConnect, info, shown)
                        QuickActionsRow(quick)
                    }
                    Column(Modifier.weight(1f).verticalScroll(rememberScrollState()), verticalArrangement = Arrangement.spacedBy(12.dp)) {
                        Details(phase, info, traffic, ProbeUi(probeState, key, onRefresh), onChange = { nav.openSection("servers", "profiles") }, onStats = { nav.push("stats") })
                    }
                }
            } else {
                Column(
                    Modifier.fillMaxSize().verticalScroll(rememberScrollState()).padding(16.dp),
                    verticalArrangement = Arrangement.spacedBy(12.dp),
                ) {
                    StateCard(phase, onConnect, info, shown)
                    Details(phase, info, traffic, ProbeUi(probeState, key, onRefresh), onChange = { nav.openSection("servers", "profiles") }, onStats = { nav.push("stats") })
                    QuickActionsRow(quick)
                }
            }
        }
    }
}

@Composable
private fun StateCard(phase: Phase, onConnect: () -> Unit, info: ConnectionInfo, shown: Shown) {
    val env = LocalEnv.current
    val c = Lav.colors
    // Right after the app starts the service state has not arrived: "Disconnected" would be a guess, and Connect pressed on
    // a tunnel that is already up does nothing visible. Until the core was asked once, the card says it is reading.
    val reading = !info.loaded && phase == Phase.Off
    val stateText = if (reading) stringResource(R.string.cx_state_reading) else stateLabel(phase, shown.notices.failure.kind)
    val noServer = info.loaded && info.server == null
    Column(verticalArrangement = Arrangement.spacedBy(12.dp)) {
        // A saved change the running tunnel does not have yet: here it is shown whether or not "later" was tapped.
        RestartBanner(showWhenSnoozed = true, padded = false)
        ServiceNoticeBanners(shown.notices, shown.actions)
        if (phase == Phase.On && shown.link is LinkHealth.NoTraffic) NoTrafficBanner(shown.link.reason, shown.onChooseServer)
        if (info.error != null) {
            LavBanner(
                StatusTone.Error, stringResource(R.string.core_err_t), text = stringResource(info.error.textRes()),
                actions = { LavButton(stringResource(R.string.err_retry), { env.connection.refresh(env.settings.selectedProfileId) }) },
            )
        }
        LavCard(Modifier.fillMaxWidth(), strong = phase == Phase.On) {
            Column(
                Modifier.fillMaxWidth().padding(20.dp),
                horizontalAlignment = Alignment.CenterHorizontally,
                verticalArrangement = Arrangement.spacedBy(8.dp),
            ) {
                ReflectionMark(markFor(phase, shown.link), size = 120.dp)
                // The single live region of the screen: TalkBack announces the state when it changes.
                LavText(
                    stateText, Modifier.semantics { liveRegion = LiveRegionMode.Polite },
                    style = Lav.type.h1, textAlign = TextAlign.Center,
                )
                LavText(stringResource(stateSubRes(phase, shown.link, shown.notices.failure, shown.notices.held)), style = Lav.type.small, color = c.textSecondary, textAlign = TextAlign.Center)
                // The service's own text, cleaned (no addresses), only as a secondary line under the worded reason.
                shown.errorDetail?.takeIf { phase == Phase.Error }?.let { LavText(it, style = Lav.type.mono, color = c.textTertiary, textAlign = TextAlign.Center) }
                if (phase == Phase.Error && ErrorDetail.suggestsNetworkCheck(shown.notices.failure.kind)) {
                    LavText(stringResource(R.string.conn_try_again), style = Lav.type.small, color = c.textTertiary, textAlign = TextAlign.Center)
                }
                if (noServer) {
                    NoServers()
                } else {
                    ConnectButton(phase, onConnect, reading)
                }
            }
        }
    }
}

@Composable
private fun NoServers() {
    val env = LocalEnv.current
    LavEmptyState(
        title = stringResource(R.string.cx_empty_t),
        text = stringResource(R.string.cx_empty_d),
        icon = LavIcon.Layers,
        actions = {
            LavButton(
                stringResource(R.string.cx_add), { env.importer.open(tab = ImportTab.Link) },
                variant = ButtonVariant.Primary, icon = LavIcon.Plus,
            )
        },
    )
}

@Composable
private fun ConnectButton(phase: Phase, onConnect: () -> Unit, reading: Boolean) {
    val env = LocalEnv.current
    val busy = phase == Phase.Connecting || phase == Phase.Disconnecting || phase == Phase.WaitingNetwork
    val connected = phase == Phase.On
    LavButton(
        text = if (connected || phase.canDisconnect) stringResource(R.string.conn_disconnect) else stringResource(R.string.conn_connect),
        onClick = { if (phase.canDisconnect) env.vpn.disconnect() else onConnect() },
        modifier = Modifier.fillMaxWidth(),
        variant = if (phase.canDisconnect) ButtonVariant.Danger else ButtonVariant.Primary,
        icon = LavIcon.Plug,
        loading = phase == Phase.Disconnecting || reading,
        enabled = (phase != Phase.Disconnecting || busy) && !reading,
    )
}

@Composable
private fun stateLabel(phase: Phase, failureKind: String?): String = stringResource(
    when (phase) {
        Phase.Off -> R.string.conn_state_off
        Phase.Connecting -> R.string.conn_state_connecting
        Phase.WaitingNetwork -> R.string.state_waiting_network
        Phase.On -> R.string.conn_state_on
        Phase.Disconnecting -> R.string.state_disconnecting
        Phase.NeedsPermission -> R.string.state_needs_permission
        Phase.Error -> ErrorDetail.titleRes(failureKind)
    },
)

/** What the state card needs beyond the phase: the core's word on traffic, what the service says, its cleaned raw text, the ways out. */
@Immutable
private class Shown(
    val link: LinkHealth,
    val notices: ServiceNotices,
    val errorDetail: String?,
    val actions: NoticeActions,
    val onChooseServer: () -> Unit,
)

/** Route preset name as on the desktop: brand names are not translated, built-in generic ones come from the dictionary. */
@Composable
internal fun presetTitle(p: PresetSummary): String = when (p.id) {
    "lav-ru-linux" -> "LAV Preset RU Linux"
    "lav-ru" -> "LAV Preset RU"
    "lav-cn" -> "LAV Preset CN"
    "global" -> stringResource(R.string.preset_global_t)
    "all-proxy" -> stringResource(R.string.preset_allproxy_t)
    "all-direct" -> stringResource(R.string.preset_alldirect_t)
    else -> if (p.displayName.startsWith("preset.")) p.id else p.displayName
}
