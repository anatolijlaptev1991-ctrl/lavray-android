// SPDX-License-Identifier: GPL-3.0-only
package io.github.anatolijlaptev1991_ctrl.lavray.vpn

import android.system.OsConstants
import kotlinx.serialization.Serializable
import kotlinx.serialization.builtins.ListSerializer
import kotlinx.serialization.json.Json

/** The Linux IFF_* bit values the Go side decodes (core-overlay/ifaces.go). */
data class IffMasks(val up: Int, val running: Int, val broadcast: Int, val multicast: Int, val pointToPoint: Int) {
    companion object {
        /** The Linux ABI values, the same on every architecture. Used by tests as the reference. */
        val LINUX = IffMasks(up = 0x1, running = 0x40, broadcast = 0x2, multicast = 0x1000, pointToPoint = 0x10)

        /** What the running system reports: android.system.OsConstants.IFF_*. */
        fun fromOs() = IffMasks(
            up = OsConstants.IFF_UP,
            running = OsConstants.IFF_RUNNING,
            broadcast = OsConstants.IFF_BROADCAST,
            multicast = OsConstants.IFF_MULTICAST,
            pointToPoint = OsConstants.IFF_POINTOPOINT,
        )
    }
}

/** Link flags per transport, as Linux IFF_* masks. Wi-Fi and Ethernet are broadcast media, cellular is point-to-point. */
object InterfaceFlags {
    fun forType(type: String, masks: IffMasks): Int {
        val base = masks.up or masks.running or masks.multicast
        return when (type) {
            "wifi", "ethernet" -> base or masks.broadcast
            "cellular" -> base or masks.pointToPoint
            else -> base
        }
    }
}

/**
 * One entry of Platform.Interfaces: {name,index,mtu,addrs:[cidr],flags,metered,type,dns:[ip],gateway:[ip]}.
 * flags are Linux IFF_* masks (see [InterfaceFlags]), never Go's net.Flags bits; type is one of wifi,
 * cellular, ethernet, other. dns and gateway feed the core's local DNS environment.
 */
@Serializable
data class InterfaceInfo(
    val name: String,
    val index: Int,
    val mtu: Int = 0,
    val addrs: List<String> = emptyList(),
    /** Required: the Go side reads an absent key as "UP|RUNNING|MULTICAST", so every entry says what it means. */
    val flags: Int,
    val metered: Boolean = false,
    val type: String = "other",
    val dns: List<String> = emptyList(),
    val gateway: List<String> = emptyList(),
) {
    companion object {
        // Defaults are written out so the Go side never has to guess what a missing key means.
        private val WIRE = Json { encodeDefaults = true }

        fun encodeList(items: List<InterfaceInfo>): String =
            WIRE.encodeToString(ListSerializer(serializer()), items)
    }
}
