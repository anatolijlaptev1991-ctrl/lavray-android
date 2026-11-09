// SPDX-License-Identifier: GPL-3.0-only
package io.github.anatolijlaptev1991_ctrl.lavray.vpn

import io.github.anatolijlaptev1991_ctrl.lavray.api.ProtoJson
import kotlinx.serialization.Serializable

/** optionsJSON of Platform.OpenTun: {mtu, inet4, inet6, dns, includePackages, excludePackages}. */
@Serializable
data class TunOptions(
    val mtu: Int = 0,
    val inet4: List<String> = emptyList(),
    val inet6: List<String> = emptyList(),
    val dns: List<String> = emptyList(),
    val includePackages: List<String> = emptyList(),
    val excludePackages: List<String> = emptyList(),
) {
    companion object {
        fun parse(json: String): TunOptions = ProtoJson.decodeFromString(serializer(), json)
    }
}

data class CidrAddress(val address: String, val prefix: Int)

object Cidr {
    /** "172.19.0.1/30" -> (172.19.0.1, 30). Rejects anything without a valid prefix. */
    fun parse(text: String): CidrAddress {
        val slash = text.lastIndexOf('/')
        require(slash > 0) { "not a CIDR address: $text" }
        val prefix = text.substring(slash + 1).toIntOrNull()
        require(prefix != null && prefix in 0..128) { "bad prefix in $text" }
        return CidrAddress(text.substring(0, slash), prefix)
    }
}

/** Per-app rules for VpnService.Builder. Allowed and disallowed lists are mutually exclusive there. */
data class AppRules(val allowed: List<String>, val disallowed: List<String>) {
    companion object {
        /**
         * Allow-list mode (includePackages set): only those apps are tunnelled; the own
         * package is dropped from it and is excluded implicitly. Otherwise every app is
         * tunnelled except excludePackages and the own package, so the core's sockets and
         * the latency-test instances never loop back into the tunnel.
         */
        fun from(options: TunOptions, ownPackage: String): AppRules {
            val include = options.includePackages.filter { it.isNotBlank() && it != ownPackage }.distinct()
            if (options.includePackages.isNotEmpty()) {
                require(include.isNotEmpty()) { "allow-list holds only the app itself" }
                return AppRules(include, emptyList())
            }
            val exclude = (options.excludePackages.filter { it.isNotBlank() } + ownPackage).distinct()
            return AppRules(emptyList(), exclude)
        }
    }
}
