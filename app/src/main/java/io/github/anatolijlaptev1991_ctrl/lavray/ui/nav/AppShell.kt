// SPDX-License-Identifier: GPL-3.0-only
package io.github.anatolijlaptev1991_ctrl.lavray.ui.nav

import androidx.activity.compose.PredictiveBackHandler
import androidx.compose.animation.core.Animatable
import androidx.compose.foundation.background
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.BoxWithConstraints
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.WindowInsets
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.heightIn
import androidx.compose.foundation.layout.ime
import androidx.compose.foundation.layout.navigationBars
import androidx.compose.foundation.layout.only
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.safeDrawing
import androidx.compose.foundation.layout.union
import androidx.compose.foundation.layout.windowInsetsPadding
import androidx.compose.foundation.layout.WindowInsetsSides
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.SideEffect
import androidx.compose.runtime.derivedStateOf
import androidx.compose.runtime.getValue
import androidx.compose.runtime.key
import androidx.compose.runtime.mutableFloatStateOf
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.graphicsLayer
import androidx.compose.ui.platform.LocalDensity
import androidx.compose.ui.platform.LocalResources
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.semantics.contentDescription
import androidx.compose.ui.semantics.heading
import androidx.compose.ui.semantics.semantics
import androidx.compose.ui.unit.dp
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import io.github.anatolijlaptev1991_ctrl.lavray.ui.screens.connection.phaseOf
import io.github.anatolijlaptev1991_ctrl.lavray.R
import io.github.anatolijlaptev1991_ctrl.lavray.ui.LocalEnv
import io.github.anatolijlaptev1991_ctrl.lavray.ui.design.Lav
import io.github.anatolijlaptev1991_ctrl.lavray.ui.design.LavIcon
import io.github.anatolijlaptev1991_ctrl.lavray.ui.design.LavText
import io.github.anatolijlaptev1991_ctrl.lavray.ui.design.LocalMotionPrefs
import io.github.anatolijlaptev1991_ctrl.lavray.ui.design.Motion
import io.github.anatolijlaptev1991_ctrl.lavray.ui.design.ReflectionMark
import io.github.anatolijlaptev1991_ctrl.lavray.ui.design.components.LavBottomNav
import io.github.anatolijlaptev1991_ctrl.lavray.ui.design.components.LavIconButton
import io.github.anatolijlaptev1991_ctrl.lavray.ui.design.components.LavNavRail
import io.github.anatolijlaptev1991_ctrl.lavray.ui.design.components.LavTabs
import io.github.anatolijlaptev1991_ctrl.lavray.ui.design.components.NavItem
import io.github.anatolijlaptev1991_ctrl.lavray.ui.design.components.ToastHost
import io.github.anatolijlaptev1991_ctrl.lavray.ui.design.components.lavClickable
import io.github.anatolijlaptev1991_ctrl.lavray.ui.importer.ImportHost
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.ui.semantics.Role
import kotlinx.coroutines.CancellationException

/** Width from which the bottom bar becomes a side rail (tablets and phones in landscape). */
private val WIDE = 600.dp

/**
 * The shell: header with the mark, section title and tabs; the destination; bottom navigation (a rail on wide
 * windows); toasts and the import sheet. Edge to edge: bars draw under the system bars and take their insets.
 * Back follows [NavState.pop] and previews the result with the system predictive-back gesture.
 */
@Composable
fun AppShell() {
    val env = LocalEnv.current
    val navigator = LocalNavigator.current
    val state = navigator.state
    val sections = AppRegistry.sections
    // Built once per language: a new list on every pass would recompose the bottom bar and the rail each time.
    val resources = LocalResources.current
    val items = remember(resources) { sections.map { NavItem(it.id, resources.getString(it.label), it.icon) } }
    val backProgress = remember { mutableFloatStateOf(0f) }

    PredictiveBackHandler(enabled = state.canPop) { events ->
        try {
            events.collect { backProgress.floatValue = it.progress }
            navigator.back()
        } catch (_: CancellationException) {
            // The gesture was cancelled: stay where we are.
        } finally {
            backProgress.floatValue = 0f
        }
    }

    BoxWithConstraints(Modifier.fillMaxSize().background(Lav.colors.bgBase)) {
        val wide = maxWidth >= WIDE
        val pushedOpen = state.pushed.isNotEmpty()
        // Derived: reading the inset height here would recompose the whole shell on every frame of the keyboard animation.
        val density = LocalDensity.current
        val ime = WindowInsets.ime
        val imeVisible by remember(ime, density) { derivedStateOf { ime.getBottom(density) > 0 } }
        val showNav = !pushedOpen && !imeVisible
        Row(Modifier.fillMaxSize()) {
            if (wide && showNav) {
                LavNavRail(
                    items, state.section, { navigator.openSection(it) },
                    Modifier.windowInsetsPadding(WindowInsets.safeDrawing.only(WindowInsetsSides.Start + WindowInsetsSides.Vertical)),
                )
            }
            Column(
                Modifier.weight(1f).fillMaxSize().windowInsetsPadding(
                    WindowInsets.safeDrawing.only(
                        (if (wide && showNav) WindowInsetsSides.End else WindowInsetsSides.Horizontal) + WindowInsetsSides.Top,
                    ),
                ),
            ) {
                Header(state, navigator)
                Box(
                    Modifier.weight(1f).fillMaxWidth()
                        // Without the bottom bar (pushed screens, the rail, the keyboard) the content must clear the system gesture bar itself.
                        .windowInsetsPadding(if (!wide && showNav) WindowInsets.ime else WindowInsets.ime.union(WindowInsets.navigationBars))
                        .graphicsLayer {
                            val p = backProgress.floatValue
                            val s = 1f - 0.08f * p
                            scaleX = s
                            scaleY = s
                            alpha = 1f - 0.3f * p
                        },
                ) {
                    Destination(state, navigator)
                    ToastHost(env.toast, Modifier.align(Alignment.BottomCenter).padding(bottom = 8.dp))
                }
                if (!wide && showNav) {
                    LavBottomNav(items, state.section, { navigator.openSection(it) }, Modifier.windowInsetsPadding(WindowInsets.navigationBars))
                }
            }
        }
        ImportHost()
    }
}

@Composable
private fun Header(state: NavState, navigator: Navigator) {
    val pushed = state.pushed.lastOrNull()?.let { AppRegistry.pushedScreen(it) }
    if (pushed != null) {
        Row(Modifier.fillMaxWidth().heightIn(min = 56.dp), verticalAlignment = Alignment.CenterVertically) {
            LavIconButton(LavIcon.ArrowLeft, stringResource(R.string.common_back), { navigator.back() }, Modifier.padding(start = 4.dp), tint = Lav.colors.textPrimary)
            LavText(stringResource(pushed.title), Modifier.semantics { heading() }, style = Lav.type.h1)
        }
        return
    }
    val section = AppRegistry.section(state.section)
    val tab = section.tab(state.tabs[section.id])
    Column {
        Row(
            Modifier.fillMaxWidth().heightIn(min = 56.dp).padding(horizontal = 16.dp),
            verticalAlignment = Alignment.CenterVertically,
            horizontalArrangement = Arrangement.spacedBy(12.dp),
        ) {
            val markLabel = stringResource(R.string.nav_connection)
            Box(
                Modifier.lavClickable({ navigator.openSection(NavState.START_SECTION) }, RoundedCornerShape(10.dp), role = Role.Button).semantics { contentDescription = markLabel }.padding(8.dp),
            ) { MarkInHeader() }
            Column(Modifier.weight(1f)) {
                LavText(stringResource(if (section.tabs.size > 1) section.label else tab.title), Modifier.semantics { heading() }, style = Lav.type.h1, maxLines = 1)
                tab.subtitle?.let { LavText(stringResource(it), style = Lav.type.small, color = Lav.colors.textTertiary, maxLines = 2) }
            }
        }
        if (section.tabs.size > 1) {
            LavTabs(
                section.tabs.map { stringResource(it.title) }, section.tabs.indexOf(tab),
                { navigator.openTab(section.id, section.tabs[it].id) },
                Modifier.padding(horizontal = 8.dp),
            )
        }
    }
}

/** The small mark in the header follows the connection state, so the phone shows it on every screen. */
@Composable
private fun MarkInHeader() {
    val env = LocalEnv.current
    val status by env.vpn.status.collectAsStateWithLifecycle()
    ReflectionMark(phaseOf(status.state).mark, size = 24.dp, small = true)
}

/** The current destination, entering with a short shift in the direction of the move (motion.emphasized). */
@Composable
private fun Destination(state: NavState, navigator: Navigator) {
    val reduced = LocalMotionPrefs.current.reduced
    val spec = destinationOf(state)
    val previous = remember { arrayOf<NavState?>(null) }
    val shift = remember(spec.id) { enterShift(previous[0], state) }
    SideEffect { previous[0] = state }
    key(spec.id) {
        val progress = remember { Animatable(if (shift == Shift.None) 1f else 0f) }
        LaunchedEffect(Unit) { if (progress.value < 1f) progress.animateTo(1f, if (reduced) Motion.fade() else Motion.emphasized()) }
        Box(
            Modifier.fillMaxSize().graphicsLayer {
                val p = progress.value
                alpha = p
                if (!reduced) {
                    translationX = (1f - p) * shift.dx * 12.dp.toPx()
                    translationY = (1f - p) * shift.dy * 12.dp.toPx()
                }
            },
        ) { spec.content() }
    }
}

private fun destinationOf(state: NavState): ScreenSpec {
    state.pushed.lastOrNull()?.let { id -> AppRegistry.pushedScreen(id)?.let { return it } }
    val section = AppRegistry.section(state.section)
    return section.tab(state.tabs[section.id])
}

/** Direction of the entering screen: down the section list enters from below, between tabs from the side. */
data class Shift(val dx: Int, val dy: Int) {
    companion object {
        val None = Shift(0, 0)
    }
}

fun enterShift(previous: NavState?, next: NavState): Shift {
    if (previous == null) return Shift.None
    val sections = AppRegistry.sections.map { it.id }
    return when {
        next.pushed.size != previous.pushed.size -> Shift(if (next.pushed.size > previous.pushed.size) 1 else -1, 0)
        next.section != previous.section -> Shift(0, Integer.signum(sections.indexOf(next.section) - sections.indexOf(previous.section)))
        else -> {
            val section = AppRegistry.section(next.section)
            val a = section.tabs.indexOf(section.tab(previous.tabs[section.id]))
            val b = section.tabs.indexOf(section.tab(next.tabs[section.id]))
            Shift(Integer.signum(b - a), 0)
        }
    }
}
