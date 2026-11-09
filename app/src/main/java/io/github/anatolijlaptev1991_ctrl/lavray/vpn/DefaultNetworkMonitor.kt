// SPDX-License-Identifier: GPL-3.0-only
package io.github.anatolijlaptev1991_ctrl.lavray.vpn

import android.net.ConnectivityManager
import android.net.LinkProperties
import android.net.Network
import android.net.NetworkCapabilities
import android.os.Build
import android.system.Os

/** The network the core should bind to, as the core wants to see it. */
data class DefaultNetwork(
    val network: Network,
    val name: String,
    val index: Int,
    val metered: Boolean,
)

/**
 * Follows the default network through ConnectivityManager callbacks (event driven,
 * delivered on the system's connectivity thread). Reports null when there is none.
 * A VPN network is ignored: the tunnel itself must never become "the network".
 *
 * Android 11+ hides interface enumeration from apps, so names come from
 * LinkProperties and the index from if_nametoindex.
 */
class DefaultNetworkMonitor(
    private val cm: ConnectivityManager,
    private val onChange: (DefaultNetwork?) -> Unit,
) {
    private var current: Network? = null
    private var link: LinkProperties? = null
    private var caps: NetworkCapabilities? = null
    private var registered = false
    private var lastEmitted: DefaultNetwork? = null

    private val callback = object : ConnectivityManager.NetworkCallback() {
        override fun onAvailable(network: Network) {
            synchronized(this@DefaultNetworkMonitor) {
                if (network != current) {
                    current = network
                    link = null
                    caps = null
                }
            }
        }

        override fun onLinkPropertiesChanged(network: Network, linkProperties: LinkProperties) {
            update(network) { link = linkProperties }
        }

        override fun onCapabilitiesChanged(network: Network, capabilities: NetworkCapabilities) {
            update(network) { caps = capabilities }
        }

        override fun onLost(network: Network) {
            val gone = synchronized(this@DefaultNetworkMonitor) {
                if (network == current) {
                    current = null
                    link = null
                    caps = null
                    lastEmitted = null
                    true
                } else {
                    false
                }
            }
            if (gone) onChange(null)
        }
    }

    @Synchronized
    fun start() {
        if (registered) return
        cm.registerDefaultNetworkCallback(callback)
        registered = true
    }

    @Synchronized
    fun stop() {
        if (!registered) return
        runCatching { cm.unregisterNetworkCallback(callback) }
        registered = false
        current = null
        link = null
        caps = null
        lastEmitted = null
    }

    private fun update(network: Network, change: () -> Unit) {
        val snapshot = synchronized(this) {
            if (network != current) return
            change()
            snapshotLocked()?.takeIf { it != lastEmitted }?.also { lastEmitted = it }
        }
        if (snapshot != null) onChange(snapshot)
    }

    private fun snapshotLocked(): DefaultNetwork? {
        val net = current ?: return null
        val lp = link ?: return null
        val c = caps ?: return null
        if (c.hasTransport(NetworkCapabilities.TRANSPORT_VPN)) return null
        val name = lp.interfaceName ?: return null
        val index = runCatching { Os.if_nametoindex(name) }.getOrDefault(0).takeIf { it > 0 } ?: return null
        return DefaultNetwork(net, name, index, !c.hasCapability(NetworkCapabilities.NET_CAPABILITY_NOT_METERED))
    }

    companion object {
        /** Interface list for Platform.Interfaces, built from every non-VPN network's LinkProperties. */
        fun interfaces(cm: ConnectivityManager): List<InterfaceInfo> {
            @Suppress("DEPRECATION")
            val networks = cm.allNetworks
            return networks.mapNotNull { describe(cm, it) }.distinctBy { it.name }
        }

        private fun describe(cm: ConnectivityManager, network: Network): InterfaceInfo? {
            val caps = cm.getNetworkCapabilities(network) ?: return null
            if (caps.hasTransport(NetworkCapabilities.TRANSPORT_VPN)) return null
            val lp = cm.getLinkProperties(network) ?: return null
            val name = lp.interfaceName ?: return null
            val index = runCatching { Os.if_nametoindex(name) }.getOrDefault(0)
            return InterfaceInfo(
                name = name,
                index = index,
                mtu = if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.Q) lp.mtu else 0,
                addrs = lp.linkAddresses.map { "${it.address.hostAddress?.substringBefore('%')}/${it.prefixLength}" },
                metered = !caps.hasCapability(NetworkCapabilities.NET_CAPABILITY_NOT_METERED),
                type = transportName(caps),
            )
        }

        private fun transportName(caps: NetworkCapabilities): String = when {
            caps.hasTransport(NetworkCapabilities.TRANSPORT_WIFI) -> "wifi"
            caps.hasTransport(NetworkCapabilities.TRANSPORT_CELLULAR) -> "cellular"
            caps.hasTransport(NetworkCapabilities.TRANSPORT_ETHERNET) -> "ethernet"
            else -> "other"
        }
    }
}
