// SPDX-License-Identifier: GPL-3.0-only
package io.github.anatolijlaptev1991_ctrl.lavray.vpn

import kotlinx.serialization.Serializable
import kotlinx.serialization.builtins.ListSerializer
import kotlinx.serialization.json.Json

/**
 * One entry of Platform.Interfaces: {name,index,mtu,addrs:[cidr],flags,metered,type}.
 * flags use Go's net.Flags bits (up=1, broadcast=2, loopback=4, pointToPoint=8, multicast=16);
 * type is one of wifi, cellular, ethernet, other.
 */
@Serializable
data class InterfaceInfo(
    val name: String,
    val index: Int,
    val mtu: Int = 0,
    val addrs: List<String> = emptyList(),
    val flags: Int = FLAGS_UP_BROADCAST_MULTICAST,
    val metered: Boolean = false,
    val type: String = "other",
) {
    companion object {
        // Defaults must be written out: the Go side reads absent keys as zero (flags 0 = interface down).
        private val WIRE = Json { encodeDefaults = true }

        const val FLAGS_UP_BROADCAST_MULTICAST = 1 or 2 or 16

        fun encodeList(items: List<InterfaceInfo>): String =
            WIRE.encodeToString(ListSerializer(serializer()), items)
    }
}
