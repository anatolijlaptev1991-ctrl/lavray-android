// SPDX-License-Identifier: GPL-3.0-only
package io.github.anatolijlaptev1991_ctrl.lavray.ui.screens.connection

import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.BoxWithConstraints
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.FlowRow
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
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.mutableLongStateOf
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
import io.github.anatolijlaptev1991_ctrl.lavray.ui.design.LavIconView
import io.github.anatolijlaptev1991_ctrl.lavray.ui.design.LavText
import io.github.anatolijlaptev1991_ctrl.lavray.ui.design.LightField
import io.github.anatolijlaptev1991_ctrl.lavray.ui.design.ReflectionMark
import io.github.anatolijlaptev1991_ctrl.lavray.ui.design.components.ButtonVariant
import io.github.anatolijlaptev1991_ctrl.lavray.ui.design.components.LavBadge
import io.github.anatolijlaptev1991_ctrl.lavray.ui.design.components.LavBanner
import io.github.anatolijlaptev1991_ctrl.lavray.ui.design.components.LavButton
import io.github.anatolijlaptev1991_ctrl.lavray.ui.design.components.LavCard
import io.github.anatolijlaptev1991_ctrl.lavray.ui.design.components.LavEmptyState
import io.github.anatolijlaptev1991_ctrl.lavray.ui.design.components.StatusTone
import io.github.anatolijlaptev1991_ctrl.lavray.ui.design.components.lavClickable
import io.github.anatolijlaptev1991_ctrl.lavray.ui.i18n.UnitFormat
import io.github.anatolijlaptev1991_ctrl.lavray.ui.i18n.rememberUnitFormat
import io.github.anatolijlaptev1991_ctrl.lavray.ui.importer.ImportTab
import io.github.anatolijlaptev1991_ctrl.lavray.ui.nav.LocalNavigator
import io.github.anatolijlaptev1991_ctrl.lavray.ui.screens.rules.PendingReason
import io.github.anatolijlaptev1991_ctrl.lavray.ui.screens.rules.RestartBanner
import io.github.anatolijlaptev1991_ctrl.lavray.ui.screens.servers.label
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.ui.semantics.Role
import io.github.anatolijlaptev1991_ctrl.lavray.api.gen.TestLatencyRequest
import io.github.anatolijlaptev1991_ctrl.lavray.ui.screens.subscriptions.EventToasts
import kotlinx.coroutines.delay
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
        Shown(link, notices, ErrorDetail.clean(notices.failure.raw), noticeActions, onChooseServer = { nav.openSection("servers", "profiles") })
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
    val stateText = if (reading) stringResource(R.string.cx_state_reading) else stateLabel(phase)
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
private fun Details(
    phase: Phase,
    info: ConnectionInfo,
    traffic: TrafficReading,
    probe: ProbeUi,
    onChange: () -> Unit,
    onStats: () -> Unit,
) {
    val fmt = rememberUnitFormat()
    // What is saved but not in the running tunnel yet: the preset and DNS shown below are the saved ones.
    val pending by LocalEnv.current.restart.state.collectAsStateWithLifecycle()
    val running = phase.isLive
    if (info.server != null) ServerCard(info.server, onChange)
    if (running) ExitCard(probe.state, probe.key, info.server, fmt, probe.onRefresh, dnsPending = PendingReason.Dns in pending.reasons)
    TrafficCard(phase, info, traffic, fmt, onStats)
    if (running) LeakCard(probe.state, probe.key, probe.onRefresh)
    if (info.preset != null) PresetRow(info.preset, pending = running && PendingReason.Routing in pending.reasons)
}

/** The probe as the cards need it: its state, the connection it belongs to and the manual refresh. */
private class ProbeUi(val state: ProbeState, val key: String?, val onRefresh: () -> Unit)

private class QuickState(
    val testing: Boolean, val progress: String?, val updatingSubs: Boolean, val info: ConnectionInfo,
    val onTest: () -> Unit, val onAdd: () -> Unit, val onSubs: () -> Unit,
)

@Composable
private fun rememberQuickActions(info: ConnectionInfo, scope: kotlinx.coroutines.CoroutineScope): QuickState {
    val env = LocalEnv.current
    val servers by env.servers.state.collectAsStateWithLifecycle()
    var subsAsked by remember { mutableStateOf(false) }
    // The subscriptions model is built (and its events heard) only after the person asks for an update.
    var updating = false
    if (subsAsked) {
        EventToasts(env.subscriptions)
        updating = env.subscriptions.state.collectAsStateWithLifecycle().value.isBusy
    }
    val test = servers.test
    return QuickState(
        testing = test.running,
        progress = if (test.running) stringResource(R.string.test_progress, test.done.toString(), test.total.toString()) else null,
        updatingSubs = updating, info = info,
        onTest = { scope.launch { env.servers.startTest(env.connection.profileIds(), TestLatencyRequest.Mode.MODE_TCP_PING) } },
        onAdd = { env.importer.open(tab = ImportTab.Link) },
        onSubs = { subsAsked = true; env.subscriptions.updateAll() },
    )
}

@Composable
private fun QuickActionsRow(q: QuickState) {
    QuickActions(
        testing = q.testing, testProgress = q.progress, updatingSubs = q.updatingSubs,
        canTest = q.info.hasServers, canUpdateSubs = q.info.subscriptions > 0,
        onTest = q.onTest, onAdd = q.onAdd, onSubs = q.onSubs,
    )
}

@Composable
private fun ServerCard(server: ServerSummary, onChange: () -> Unit) {
    val c = Lav.colors
    val shape = RoundedCornerShape(14.dp)
    val change = stringResource(R.string.srv_change)
    LavCard(Modifier.fillMaxWidth()) {
        Row(
            Modifier.fillMaxWidth().lavClickable(onChange, shape, role = Role.Button, onClickLabel = change).padding(16.dp),
            verticalAlignment = Alignment.CenterVertically,
            horizontalArrangement = Arrangement.spacedBy(12.dp),
        ) {
            Column(Modifier.weight(1f), verticalArrangement = Arrangement.spacedBy(4.dp)) {
                LavText(stringResource(R.string.srv_current), style = Lav.type.caption, color = c.textTertiary)
                // A flow: with a large font the badge drops under the name instead of cutting the name to one letter per line.
                FlowRow(horizontalArrangement = Arrangement.spacedBy(8.dp), verticalArrangement = Arrangement.spacedBy(4.dp), itemVerticalAlignment = Alignment.CenterVertically) {
                    LavText(server.name, style = Lav.type.h2, maxLines = 2, overflow = androidx.compose.ui.text.style.TextOverflow.Ellipsis)
                    LavBadge(server.type.label())
                }
                LavText("${server.address}:${server.port}", style = Lav.type.mono, color = c.textTertiary, maxLines = 1, overflow = androidx.compose.ui.text.style.TextOverflow.Ellipsis)
            }
            LavText(change, style = Lav.type.small, color = c.accent)
            LavIconView(LavIcon.ChevronRight, c.accent, size = 18.dp)
        }
    }
}

@Composable
private fun PresetRow(preset: PresetSummary, pending: Boolean) {
    val c = Lav.colors
    LavCard(Modifier.fillMaxWidth()) {
        Row(Modifier.fillMaxWidth().padding(16.dp), horizontalArrangement = Arrangement.SpaceBetween, verticalAlignment = Alignment.CenterVertically) {
            LavText(stringResource(R.string.health_preset), style = Lav.type.small, color = c.textTertiary)
            Column(Modifier.weight(1f).padding(start = 12.dp), horizontalAlignment = Alignment.End) {
                LavText(presetTitle(preset), style = Lav.type.bodyStrong, textAlign = TextAlign.End)
                // The tunnel still routes by the previous preset until the restart.
                if (pending) LavText(stringResource(R.string.cx_pending_apply), style = Lav.type.caption, color = c.connectingText, textAlign = TextAlign.End)
            }
        }
    }
}

@Composable
private fun TrafficCard(phase: Phase, info: ConnectionInfo, traffic: TrafficReading, fmt: UnitFormat, onStats: () -> Unit) {
    val c = Lav.colors
    val shape = RoundedCornerShape(14.dp)
    val elapsed = rememberSessionSeconds(phase.isLive, info.startedAtMs)
    LavCard(Modifier.fillMaxWidth()) {
        Column(
            Modifier.fillMaxWidth().lavClickable(onStats, shape, role = Role.Button).padding(16.dp),
            verticalArrangement = Arrangement.spacedBy(12.dp),
        ) {
            Row(Modifier.fillMaxWidth(), horizontalArrangement = Arrangement.spacedBy(12.dp)) {
                SpeedCell(LavIcon.ArrowDown, stringResource(R.string.conn_down), fmt.speed(traffic.downBps), Modifier.weight(1f))
                SpeedCell(LavIcon.ArrowUp, stringResource(R.string.conn_up), fmt.speed(traffic.upBps), Modifier.weight(1f))
            }
            Row(Modifier.fillMaxWidth(), horizontalArrangement = Arrangement.spacedBy(12.dp)) {
                Cell(stringResource(R.string.conn_session), fmt.clock(elapsed), Modifier.weight(1f))
                Cell(stringResource(R.string.conn_traffic), fmt.bytes(traffic.downTotal + traffic.upTotal), Modifier.weight(1f))
            }
        }
    }
}

@Composable
private fun SpeedCell(icon: LavIcon, label: String, value: String, modifier: Modifier) {
    val c = Lav.colors
    Column(modifier.semantics(mergeDescendants = true) {}, verticalArrangement = Arrangement.spacedBy(2.dp)) {
        Row(horizontalArrangement = Arrangement.spacedBy(4.dp), verticalAlignment = Alignment.CenterVertically) {
            LavIconView(icon, c.textTertiary, size = 14.dp)
            LavText(label, style = Lav.type.caption, color = c.textTertiary)
        }
        LavText(value, style = Lav.type.mono.copy(fontSize = androidx.compose.ui.unit.TextUnit(16f, androidx.compose.ui.unit.TextUnitType.Sp)), maxLines = 1)
    }
}

@Composable
private fun Cell(label: String, value: String, modifier: Modifier) {
    val c = Lav.colors
    Column(modifier.semantics(mergeDescendants = true) {}, verticalArrangement = Arrangement.spacedBy(2.dp)) {
        LavText(label, style = Lav.type.caption, color = c.textTertiary)
        LavText(value, style = Lav.type.mono, maxLines = 1)
    }
}

/** Seconds since the tunnel came up; ticks once a second, only while the screen is started and the tunnel is on. */
@Composable
private fun rememberSessionSeconds(live: Boolean, startedAtMs: Long): Long {
    var seconds by remember { mutableLongStateOf(0L) }
    val owner = LocalLifecycleOwner.current
    val fallbackStart = remember(live) { System.currentTimeMillis() }
    LaunchedEffect(live, startedAtMs) {
        if (!live) {
            seconds = 0L
            return@LaunchedEffect
        }
        val start = if (startedAtMs > 0) startedAtMs else fallbackStart
        owner.lifecycle.repeatOnLifecycle(Lifecycle.State.STARTED) {
            while (true) {
                seconds = ((System.currentTimeMillis() - start) / 1000).coerceAtLeast(0)
                delay(1000)
            }
        }
    }
    return seconds
}

@Composable
private fun stateLabel(phase: Phase): String = stringResource(
    when (phase) {
        Phase.Off -> R.string.conn_state_off
        Phase.Connecting -> R.string.conn_state_connecting
        Phase.WaitingNetwork -> R.string.state_waiting_network
        Phase.On -> R.string.conn_state_on
        Phase.Disconnecting -> R.string.state_disconnecting
        Phase.NeedsPermission -> R.string.state_needs_permission
        Phase.Error -> R.string.conn_state_err
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
private fun presetTitle(p: PresetSummary): String = when (p.id) {
    "lav-ru" -> "LAV Preset RU"
    "lav-cn" -> "LAV Preset CN"
    "global" -> stringResource(R.string.preset_global_t)
    "all-proxy" -> stringResource(R.string.preset_allproxy_t)
    "all-direct" -> stringResource(R.string.preset_alldirect_t)
    else -> if (p.displayName.startsWith("preset.")) p.id else p.displayName
}
