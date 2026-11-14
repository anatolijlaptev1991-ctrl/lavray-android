// SPDX-License-Identifier: GPL-3.0-only
package io.github.anatolijlaptev1991_ctrl.lavray.ui.screens.rules

import androidx.annotation.StringRes
import androidx.compose.runtime.Immutable
import io.github.anatolijlaptev1991_ctrl.lavray.R
import io.github.anatolijlaptev1991_ctrl.lavray.api.gen.Outbound
import io.github.anatolijlaptev1991_ctrl.lavray.api.gen.ProcessRule
import io.github.anatolijlaptev1991_ctrl.lavray.api.gen.RoutingPreset

/** How the name and the description of a built-in preset are worded: a brand is not translated. */
@Immutable
data class BuiltinText(val brand: String?, @StringRes val nameRes: Int?, @StringRes val descRes: Int)

/**
 * What the screen knows about the built-in presets (desktop lib/presets.ts): their fixed order, names and
 * descriptions. The core sends a dictionary key as the name of a built-in, which is never shown raw.
 */
object PresetCatalog {
    /** The phone's default: LAV Preset RU without the rules for Windows programs (core-overlay/presets). */
    const val LAV_RU_LINUX = "lav-ru-linux"
    const val LAV_RU = "lav-ru"

    private val builtin: Map<String, BuiltinText> = mapOf(
        LAV_RU_LINUX to BuiltinText("LAV Preset RU Linux", null, R.string.preset_lavrulinux_desc),
        LAV_RU to BuiltinText("LAV Preset RU", null, R.string.preset_lavru_desc),
        "lav-cn" to BuiltinText("LAV Preset CN", null, R.string.preset_lavcn_desc),
        "global" to BuiltinText(null, R.string.preset_global_t, R.string.preset_global_d),
        "all-proxy" to BuiltinText(null, R.string.preset_allproxy_t, R.string.preset_allproxy_d),
        "all-direct" to BuiltinText(null, R.string.preset_alldirect_t, R.string.preset_alldirect_d),
    )

    private val order: List<String> = builtin.keys.toList()

    fun textOf(id: String): BuiltinText? = builtin[id]

    /** Name to show for a preset that is not translated: a custom one as it is, an unknown built-in by its id. */
    fun fallbackName(p: RoutingPreset): String =
        if (p.displayName.startsWith("preset.")) p.id else p.displayName

    /**
     * Built-in presets first in the fixed order (LAV Preset RU Linux on top), then the person's own in the order
     * the core gave. A built-in the app does not know yet goes after the known ones.
     */
    fun sorted(presets: List<RoutingPreset>): List<RoutingPreset> {
        val known = presets.filter { it.builtin && it.id in builtin }.sortedBy { order.indexOf(it.id) }
        val unknownBuiltin = presets.filter { it.builtin && it.id !in builtin }
        val custom = presets.filter { !it.builtin }
        return known + unknownBuiltin + custom
    }

    /**
     * The preset in force. The core names it; when it names none, LAV Preset RU Linux is the default on the phone
     * (core-overlay/defaults.go), then LAV Preset RU (the desktop's default since 0.9.3), then the first preset.
     */
    fun effectiveActiveId(presets: List<RoutingPreset>, activeId: String): String = when {
        presets.any { it.id == activeId } -> activeId
        presets.any { it.id == LAV_RU_LINUX } -> LAV_RU_LINUX
        presets.any { it.id == LAV_RU } -> LAV_RU
        else -> presets.firstOrNull()?.id.orEmpty()
    }

    /** Entries of all lists and the app rules (desktop presetRuleCount). */
    fun ruleCount(p: RoutingPreset): Int =
        p.proxyDomain.size + p.directDomain.size + p.blockDomain.size +
            p.proxyIp.size + p.directIp.size + p.blockIp.size + p.processRules.size

    /**
     * App rules select a connection by process name. The Android core drops that condition (it cannot tell a
     * Windows process from a phone app), so every such rule is inert on a phone; they are kept in the preset.
     */
    fun inertOnPhone(p: RoutingPreset): Int = p.processRules.size

    fun platformOf(rule: ProcessRule): ProcessPlatform = when {
        rule.platforms.isEmpty() -> ProcessPlatform.AllSystems
        rule.platforms.size == 1 && rule.platforms[0] == "windows" -> ProcessPlatform.WindowsOnly
        else -> ProcessPlatform.Listed(rule.platforms.joinToString(", ") { platformName(it) })
    }

    private fun platformName(id: String): String = when (id) {
        "windows" -> "Windows"
        "linux" -> "Linux"
        "macos" -> "macOS"
        "openwrt" -> "OpenWrt"
        else -> id
    }

    @StringRes
    fun outboundLabel(o: Outbound): Int = when (o) {
        Outbound.OUTBOUND_PROXY -> R.string.out_proxy
        Outbound.OUTBOUND_DIRECT -> R.string.out_direct
        Outbound.OUTBOUND_BLOCK -> R.string.out_block
        Outbound.OUTBOUND_UNSPECIFIED -> R.string.out_direct
    }
}

/** Platforms a process rule is limited to. */
sealed interface ProcessPlatform {
    data object AllSystems : ProcessPlatform
    data object WindowsOnly : ProcessPlatform
    data class Listed(val names: String) : ProcessPlatform
}
