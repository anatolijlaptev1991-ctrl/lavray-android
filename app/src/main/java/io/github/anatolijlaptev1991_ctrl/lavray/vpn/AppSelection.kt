// SPDX-License-Identifier: GPL-3.0-only
package io.github.anatolijlaptev1991_ctrl.lavray.vpn

import kotlinx.serialization.Serializable
import kotlinx.serialization.json.Json

/** Which apps go through the tunnel: all of them, only the chosen ones, or all except the chosen ones. */
enum class AppMode(val wire: String) {
    All("all"), Only("only"), Except("except");

    companion object {
        fun fromWire(text: String?): AppMode = entries.firstOrNull { it.wire == text } ?: All
    }
}

/**
 * The person's per-app choice. It lives in the :vpn process (the boot receiver builds a tunnel before any UI exists)
 * and reaches VpnService.Builder through [applyTo]. Immutable; every change makes a new value.
 *
 * "Only the chosen apps" with an empty list is treated as "all apps": an empty allow-list would hand the Builder
 * nothing to allow and Android would tunnel everything anyway, so the app says so openly instead of pretending.
 */
data class AppSelection(val mode: AppMode = AppMode.All, val packages: Set<String> = emptySet()) {
    /** The mode that is really in force. */
    val effectiveMode: AppMode get() = if (mode == AppMode.Only && packages.isEmpty()) AppMode.All else mode

    /** TUN options of the core with this choice merged in. The caller's own package is handled by [AppRules]. */
    fun applyTo(options: TunOptions): TunOptions = when (effectiveMode) {
        AppMode.All -> options
        AppMode.Only -> options.copy(includePackages = packages.sorted(), excludePackages = emptyList())
        AppMode.Except -> options.copy(excludePackages = (options.excludePackages + packages.sorted()).distinct())
    }

    fun with(mode: AppMode): AppSelection = copy(mode = mode)

    fun toggled(pkg: String): AppSelection = copy(packages = if (pkg in packages) packages - pkg else packages + pkg)

    fun toJson(): String = WIRE_JSON.encodeToString(Wire.serializer(), Wire(mode.wire, packages.sorted()))

    @Serializable
    private data class Wire(val mode: String = "all", val packages: List<String> = emptyList())

    companion object {
        const val MAX_PACKAGES = 2_000
        private val WIRE_JSON = Json { ignoreUnknownKeys = true; encodeDefaults = true }
        private val PACKAGE_NAME = Regex("^[A-Za-z][A-Za-z0-9_]*(\\.[A-Za-z0-9_]+)+$")

        /** Unreadable text is the default (all apps); a name that cannot be a package is dropped. */
        fun parse(json: String?): AppSelection {
            if (json.isNullOrBlank()) return AppSelection()
            val wire = try {
                WIRE_JSON.decodeFromString(Wire.serializer(), json)
            } catch (_: IllegalArgumentException) {
                return AppSelection()
            }
            val names = wire.packages.filter { PACKAGE_NAME.matches(it) }.take(MAX_PACKAGES).toSet()
            return AppSelection(AppMode.fromWire(wire.mode), names)
        }
    }
}
