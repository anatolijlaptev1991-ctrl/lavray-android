// SPDX-License-Identifier: GPL-3.0-only
package io.github.anatolijlaptev1991_ctrl.lavray.vpn

import android.net.ConnectivityManager
import android.net.LinkProperties
import android.net.Network
import android.net.NetworkCapabilities
import android.net.NetworkRequest
import android.os.Build
import android.os.Handler
import android.os.HandlerThread
import android.system.Os

/** The network the core should bind to, as the core wants to see it. */
data class DefaultNetwork(
    val network: Network,
    val name: String,
    val index: Int,
    val metered: Boolean,
)

/** How the physical default network is observed; chosen by API level. */
enum class NetworkRegistration {
    /** API 31+: the best network that is not a VPN, with no side effects. */
    BEST_MATCHING,

    /**
     * API 26 to 30: a request for the best non-VPN network. requestNetwork(request, callback, handler) exists since
     * API 26 and needs only CHANGE_NETWORK_STATE (developer.android.com, ConnectivityManager); no documented
     * limitation on 26-27. The plain default-network callback was used there before and never saw a physical change
     * once the tunnel was up (it reports the tunnel itself as the default).
     */
    REQUEST,
    ;

    companion object {
        fun forSdk(sdk: Int): NetworkRegistration = when {
            sdk >= Build.VERSION_CODES.S -> BEST_MATCHING
            else -> REQUEST
        }
    }
}

/**
 * Follows the physical default network through ConnectivityManager callbacks (event
 * driven, delivered on a private handler thread that lives only while it is watching).
 * Reports null when there is none.
 *
 * registerDefaultNetworkCallback is not enough: once our tunnel is up the system
 * reports the tunnel itself as the default, the physical changes stop arriving, and
 * the core keeps binding to a dead Wi-Fi. The request therefore asks for a non-VPN
 * network explicitly.
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
    private var worker: HandlerThread? = null

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
        val thread = HandlerThread("lavray-net").also { it.start() }
        val handler = Handler(thread.looper)
        try {
            register(handler)
        } catch (e: RuntimeException) {
            thread.quitSafely()
            throw e
        }
        worker = thread
        registered = true
    }

    // Версию API проверяет NetworkRegistration.forSdk, lint этого не видит.
    @android.annotation.SuppressLint("NewApi")
    private fun register(handler: Handler) {
        val request = NetworkRequest.Builder()
            .addCapability(NetworkCapabilities.NET_CAPABILITY_INTERNET)
            .addCapability(NetworkCapabilities.NET_CAPABILITY_NOT_RESTRICTED)
            .addCapability(NetworkCapabilities.NET_CAPABILITY_NOT_VPN)
            .build()
        when (NetworkRegistration.forSdk(Build.VERSION.SDK_INT)) {
            NetworkRegistration.BEST_MATCHING -> cm.registerBestMatchingNetworkCallback(request, callback, handler)
            NetworkRegistration.REQUEST -> cm.requestNetwork(request, callback, handler)
        }
    }

    @Synchronized
    fun stop() {
        if (!registered) return
        runCatching { cm.unregisterNetworkCallback(callback) }
        registered = false
        worker?.quitSafely()
        worker = null
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
            val type = transportName(caps)
            return InterfaceInfo(
                name = name,
                index = index,
                mtu = if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.Q) lp.mtu else 0,
                addrs = lp.linkAddresses.map { "${it.address.hostAddress?.substringBefore('%')}/${it.prefixLength}" },
                flags = InterfaceFlags.forType(type, IffMasks.fromOs()),
                metered = !caps.hasCapability(NetworkCapabilities.NET_CAPABILITY_NOT_METERED),
                type = type,
                dns = lp.dnsServers.mapNotNull { it.hostAddress?.substringBefore('%') },
                gateway = lp.routes.filter { it.isDefaultRoute && it.gateway?.isAnyLocalAddress == false }
                    .mapNotNull { it.gateway?.hostAddress?.substringBefore('%') },
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
