// SPDX-License-Identifier: GPL-3.0-only
package io.github.anatolijlaptev1991_ctrl.lavray.ui.screens.rules

import androidx.compose.runtime.Immutable
import io.github.anatolijlaptev1991_ctrl.lavray.api.gen.Outbound
import io.github.anatolijlaptev1991_ctrl.lavray.api.gen.RoutingPreset
import io.github.anatolijlaptev1991_ctrl.lavray.ui.core.ErrorKind

@Immutable
data class PresetsState(
    /** Built-in presets in the fixed order, then the person's own. */
    val presets: List<RoutingPreset> = emptyList(),
    val activeId: String = "",
    val loading: Boolean = true,
    val error: ErrorKind? = null,
    /** An operation is running; the screen blocks a second one so edits go one after another. */
    val busy: Boolean = false,
    /** Preset open in the editor page, if any. */
    val openId: String? = null,
) {
    val active: RoutingPreset? get() = presets.firstOrNull { it.id == activeId }
    val open: RoutingPreset? get() = openId?.let { id -> presets.firstOrNull { it.id == id } }
}

sealed interface PresetsAction {
    data class Loaded(val presets: List<RoutingPreset>, val activeId: String) : PresetsAction
    data class Failed(val kind: ErrorKind) : PresetsAction
    data class Replace(val preset: RoutingPreset) : PresetsAction
    data class Active(val id: String) : PresetsAction
    data class Open(val id: String?) : PresetsAction
    data class Busy(val on: Boolean) : PresetsAction
}

/** The presets screen's state machine as pure functions; the model runs it off the main thread. */
object PresetsReducer {
    fun reduce(s: PresetsState, a: PresetsAction): PresetsState = when (a) {
        is PresetsAction.Loaded -> {
            val sorted = PresetCatalog.sorted(a.presets)
            val active = PresetCatalog.effectiveActiveId(sorted, a.activeId)
            // An editor open on a preset that no longer exists closes itself.
            val open = s.openId?.takeIf { id -> sorted.any { it.id == id } }
            s.copy(presets = sorted, activeId = active, loading = false, error = null, openId = open)
        }
        is PresetsAction.Failed -> s.copy(loading = false, error = a.kind)
        is PresetsAction.Replace -> s.copy(presets = s.presets.map { if (it.id == a.preset.id) a.preset else it })
        is PresetsAction.Active -> s.copy(activeId = a.id)
        is PresetsAction.Open -> s.copy(openId = a.id)
        is PresetsAction.Busy -> s.copy(busy = a.on)
    }

    /** The outcome used when a preset names none: the core's default is to send everything through the proxy. */
    fun defaultOutbound(p: RoutingPreset): Outbound =
        if (p.defaultOutbound == Outbound.OUTBOUND_UNSPECIFIED) Outbound.OUTBOUND_PROXY else p.defaultOutbound
}
