// SPDX-License-Identifier: GPL-3.0-only
package io.github.anatolijlaptev1991_ctrl.lavray.ui.screens.settings

import io.github.anatolijlaptev1991_ctrl.lavray.api.gen.SettingDescriptor
import io.github.anatolijlaptev1991_ctrl.lavray.api.gen.SettingValue

/** Keys of the core settings that have a meaning on a phone. Everything else of the desktop catalog is hidden. */
object SettingKeys {
    const val HEALTH_CHECK = "connect.health_check"

    /**
     * Not shown: on a phone the core always uses gVisor (core-overlay/patch.go, androidStack).
     * The system and mixed stacks pass no data while the app is excluded from its own VPN.
     */
    const val TUN_STACK = "tun.impl"
    const val TUN_MTU = "tun.mtu"
    const val TUN_IPV6 = "tun.ipv6"
    const val TUN_FAKE_DNS = "tun.fake_dns"
    const val TUN_EXCLUDE_ADDRESSES = "tun.bypass_cidr"
    const val MUX_PROTOCOL = "mux.protocol"
    const val MUX_CONCURRENCY = "mux.concurrency"
    const val MUX_DEFAULT_ON = "mux.default_on"
    const val INSECURE_TLS = "security.allow_insecure_default"

    /** The keys this screen reads, in the order they are shown. */
    val SHOWN: List<String> = listOf(
        HEALTH_CHECK, TUN_MTU, TUN_IPV6, TUN_FAKE_DNS, TUN_EXCLUDE_ADDRESSES,
        MUX_PROTOCOL, MUX_CONCURRENCY, MUX_DEFAULT_ON, INSECURE_TLS,
    )

    /** Keys that DescribeSettings does not list are dropped, so a core without a setting never shows a dead row. */
    fun available(descriptors: List<SettingDescriptor>): Set<String> {
        if (descriptors.isEmpty()) return SHOWN.toSet()
        val known = descriptors.mapTo(HashSet()) { it.key }
        return SHOWN.filterTo(HashSet()) { it in known }
    }
}

/** Multiplexing protocols the core accepts (cfgbuild/outbounds.go), as the desktop lists them. */
val MUX_PROTOCOLS: List<String> = listOf("h2mux", "smux", "yamux")

/** Result of reading a number a person typed. */
sealed interface NumberInput {
    data class Valid(val value: Long) : NumberInput
    data object NotANumber : NumberInput
    data object OutOfRange : NumberInput
}

object SettingsMapper {
    const val MTU_MIN = 1280L
    const val MTU_MAX = 9000L
    const val CONCURRENCY_MAX = 1024L

    fun bool(v: SettingValue?, default: Boolean): Boolean = v?.boolValue ?: default

    fun int(v: SettingValue?, default: Long): Long = v?.intValue ?: default

    fun string(v: SettingValue?, default: String): String = v?.stringValue ?: default

    /** `default_value_json` of a descriptor read as a number (`9000`), or null when it is not one. */
    fun defaultInt(d: SettingDescriptor?): Long? = d?.defaultValueJson?.trim()?.toLongOrNull()

    fun defaultString(d: SettingDescriptor?): String? = d?.defaultValueJson?.trim()?.takeIf { it.length >= 2 && it.startsWith("\"") && it.endsWith("\"") }?.drop(1)?.dropLast(1)

    fun defaultBool(d: SettingDescriptor?): Boolean? = when (d?.defaultValueJson?.trim()) {
        "true" -> true
        "false" -> false
        else -> null
    }

    fun parseMtu(text: String): NumberInput = parseNumber(text, MTU_MIN, MTU_MAX)

    /** Concurrent streams: -1 keeps the core's own choice, otherwise 1..1024. */
    fun parseConcurrency(text: String): NumberInput {
        val n = text.trim().toLongOrNull() ?: return NumberInput.NotANumber
        return if (n == -1L || n in 1..CONCURRENCY_MAX) NumberInput.Valid(n) else NumberInput.OutOfRange
    }

    private fun parseNumber(text: String, min: Long, max: Long): NumberInput {
        val n = text.trim().toLongOrNull() ?: return NumberInput.NotANumber
        return if (n in min..max) NumberInput.Valid(n) else NumberInput.OutOfRange
    }

    /** One value per line (desktop tun.exclHint); blank lines and `#` comments are dropped. */
    fun lines(text: String): List<String> =
        text.lineSequence().map { it.trim() }.filter { it.isNotEmpty() && !it.startsWith("#") }.toList()

    private val ipv4 = Regex("^(\\d{1,3})(\\.\\d{1,3}){3}(/\\d{1,2})?$")
    private val ipv6 = Regex("^[0-9A-Fa-f:.]*:[0-9A-Fa-f:.]*(/\\d{1,3})?$")

    /** An address or a network in CIDR form; names are not accepted here because the tunnel routes by address. */
    fun isAddressOrCidr(line: String): Boolean {
        if (ipv4.matches(line)) {
            val (address, prefix) = line.substringBefore('/') to line.substringAfter('/', "")
            val octetsOk = address.split('.').all { (it.toIntOrNull() ?: 256) in 0..255 }
            return octetsOk && (prefix.isEmpty() || prefix.toInt() in 0..32)
        }
        if (ipv6.matches(line)) {
            val prefix = line.substringAfter('/', "")
            return prefix.isEmpty() || prefix.toInt() in 0..128
        }
        return false
    }

    /** The first line that is not an address or a network, or null when all are fine. */
    fun firstInvalidAddress(text: String): String? = lines(text).firstOrNull { !isAddressOrCidr(it) }
}
