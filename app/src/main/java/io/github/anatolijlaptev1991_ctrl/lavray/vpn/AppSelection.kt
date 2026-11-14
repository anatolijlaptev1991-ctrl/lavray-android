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

/** A list whose apps ignore the routing rules: everything through the server, or everything direct. */
enum class AppRoute { ViaVpn, Direct }

/**
 * The person's per-app choice. It lives in the :vpn process (the boot receiver builds a tunnel before any UI exists)
 * and reaches VpnService.Builder through [applyTo] and the core through [coreRoutesJson]. Immutable; every change makes
 * a new value.
 *
 * "Only the chosen apps" with an empty list is treated as "all apps": an empty allow-list would hand the Builder
 * nothing to allow and Android would tunnel everything anyway, so the app says so openly instead of pretending.
 *
 * [viaVpn] and [direct] come before everything else, the tunnel mode and the routing preset alike, and never share an
 * app: an app in [viaVpn] is always in the tunnel and its internet traffic always goes to the server; an app in [direct]
 * never goes through the server.
 */
data class AppSelection(
    val mode: AppMode = AppMode.All,
    val packages: Set<String> = emptySet(),
    val viaVpn: Set<String> = emptySet(),
    val direct: Set<String> = emptySet(),
) {
    /** The mode that is really in force. */
    val effectiveMode: AppMode get() = if (mode == AppMode.Only && packages.isEmpty()) AppMode.All else mode

    fun listOf(route: AppRoute): Set<String> = when (route) {
        AppRoute.ViaVpn -> viaVpn
        AppRoute.Direct -> direct
    }

    /** The list [pkg] is in, null when it is in neither. */
    fun routeOf(pkg: String): AppRoute? = when (pkg) {
        in viaVpn -> AppRoute.ViaVpn
        in direct -> AppRoute.Direct
        else -> null
    }

    /**
     * TUN options of the core with this choice merged in. The caller's own package is handled by [AppRules].
     *
     * An app in [direct] is kept out of the tunnel: its traffic then never touches the core, which is the cheapest
     * direct there is. Not with [lockdown] ("Block connections without VPN"): Android leaves an app outside the VPN
     * without any network then, so the app stays in the tunnel and the core sends it direct ([coreRoutesJson]).
     * Throws [IllegalArgumentException] when "only the chosen apps" is left with none: every chosen app is in [direct].
     */
    fun applyTo(options: TunOptions, lockdown: Boolean = false): TunOptions {
        val base = when (effectiveMode) {
            AppMode.All -> options
            AppMode.Only -> options.copy(includePackages = packages.sorted(), excludePackages = emptyList())
            AppMode.Except -> options.copy(excludePackages = (options.excludePackages + packages.sorted()).distinct())
        }
        val inside = if (lockdown) viaVpn + direct else viaVpn
        val outside = if (lockdown) emptySet() else direct
        if (inside.isEmpty() && outside.isEmpty()) return base
        if (base.includePackages.isNotEmpty()) {
            val include = (base.includePackages + inside.sorted()).distinct().filter { it !in outside }
            require(include.isNotEmpty()) { "every chosen app goes direct" }
            return base.copy(includePackages = include)
        }
        return base.copy(excludePackages = (base.excludePackages.filter { it !in inside } + outside.sorted()).distinct())
    }

    /**
     * The lists for the core (Platform.AppRoutes): {"proxy":[...],"direct":[...]}, or "" when there is nothing to send.
     * A rule by app makes the core ask the system who owns every connection, so nothing is sent that a rule cannot use:
     * without [canFindOwner] (Android 9 and older have no such call) there is no list at all, and [direct] is sent only
     * with [lockdown], when its apps are in the tunnel ([applyTo]).
     */
    fun coreRoutesJson(lockdown: Boolean, canFindOwner: Boolean): String {
        if (!canFindOwner) return ""
        val toDirect = if (lockdown) direct else emptySet()
        if (viaVpn.isEmpty() && toDirect.isEmpty()) return ""
        return WIRE_JSON.encodeToString(CoreRoutes.serializer(), CoreRoutes(viaVpn.sorted(), toDirect.sorted()))
    }

    fun with(mode: AppMode): AppSelection = copy(mode = mode)

    fun toggled(pkg: String): AppSelection = copy(packages = if (pkg in packages) packages - pkg else packages + pkg)

    /** [pkg] added to or removed from [route]; null when it is in the other list, which it must leave first. */
    fun toggledIn(route: AppRoute, pkg: String): AppSelection? {
        val other = routeOf(pkg)
        if (other != null && other != route) return null
        return when (route) {
            AppRoute.ViaVpn -> copy(viaVpn = if (pkg in viaVpn) viaVpn - pkg else viaVpn + pkg)
            AppRoute.Direct -> copy(direct = if (pkg in direct) direct - pkg else direct + pkg)
        }
    }

    fun toJson(): String = WIRE_JSON.encodeToString(Wire.serializer(), Wire(mode.wire, packages.sorted(), viaVpn.sorted(), direct.sorted()))

    @Serializable
    private data class Wire(
        val mode: String = "all",
        val packages: List<String> = emptyList(),
        val proxy: List<String> = emptyList(),
        val direct: List<String> = emptyList(),
    )

    @Serializable
    private data class CoreRoutes(val proxy: List<String>, val direct: List<String>)

    companion object {
        const val MAX_PACKAGES = 2_000
        private val WIRE_JSON = Json { ignoreUnknownKeys = true; encodeDefaults = true; coerceInputValues = true }
        private val PACKAGE_NAME = Regex("^[A-Za-z][A-Za-z0-9_]*(\\.[A-Za-z0-9_]+)+$")

        /**
         * Unreadable text is the default (all apps); a name that cannot be a package is dropped. An app found in both
         * lists stays in [viaVpn] only: when in doubt, its traffic must not leave the tunnel.
         */
        fun parse(json: String?): AppSelection {
            if (json.isNullOrBlank()) return AppSelection()
            val wire = try {
                WIRE_JSON.decodeFromString(Wire.serializer(), json)
            } catch (_: IllegalArgumentException) {
                return AppSelection()
            }
            val viaVpn = names(wire.proxy)
            return AppSelection(AppMode.fromWire(wire.mode), names(wire.packages), viaVpn, names(wire.direct) - viaVpn)
        }

        private fun names(list: List<String>): Set<String> = list.filter { PACKAGE_NAME.matches(it) }.take(MAX_PACKAGES).toSet()
    }
}
