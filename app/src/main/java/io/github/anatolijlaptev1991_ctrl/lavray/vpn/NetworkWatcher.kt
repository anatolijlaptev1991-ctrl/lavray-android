// SPDX-License-Identifier: GPL-3.0-only
package io.github.anatolijlaptev1991_ctrl.lavray.vpn

import android.net.ConnectivityManager
import android.net.Network
import android.net.NetworkCapabilities
import android.net.NetworkRequest

/**
 * Tells the auto-connect logic when a usable network appears or the last one goes.
 * Backed by one NetworkCallback (INTERNET and NOT_VPN) that exists only while
 * auto-connect is waiting or backing off: no polling, no alarm, no wake lock.
 *
 * Networks already present when watching starts are recorded silently, so
 * registering never looks like a "new network" (which would reset the backoff).
 * [isUp] lets the caller reconcile its own belief right after [start].
 */
class NetworkWatcher(
    private val cm: ConnectivityManager,
    private val onAvailable: () -> Unit,
    private val onLost: () -> Unit,
) {
    private val known = HashSet<Network>()
    private var registered = false

    private val callback = object : ConnectivityManager.NetworkCallback() {
        override fun onAvailable(network: Network) {
            val isNew = synchronized(known) { known.add(network) }
            if (isNew) this@NetworkWatcher.onAvailable()
        }

        override fun onLost(network: Network) {
            val lastGone = synchronized(known) { known.remove(network) && known.isEmpty() }
            if (lastGone) this@NetworkWatcher.onLost()
        }
    }

    @Synchronized
    fun start() {
        if (registered) return
        synchronized(known) {
            known.clear()
            @Suppress("DEPRECATION")
            cm.allNetworks.filterTo(known) { isUsable(it) }
        }
        val request = NetworkRequest.Builder()
            .addCapability(NetworkCapabilities.NET_CAPABILITY_INTERNET)
            .addCapability(NetworkCapabilities.NET_CAPABILITY_NOT_VPN)
            .build()
        cm.registerNetworkCallback(request, callback)
        registered = true
    }

    @Synchronized
    fun stop() {
        if (!registered) return
        runCatching { cm.unregisterNetworkCallback(callback) }
        registered = false
        synchronized(known) { known.clear() }
    }

    /** True when some non-VPN network with internet capability exists right now. */
    fun isUp(): Boolean {
        @Suppress("DEPRECATION")
        return cm.allNetworks.any { isUsable(it) }
    }

    private fun isUsable(network: Network): Boolean {
        val caps = cm.getNetworkCapabilities(network) ?: return false
        return caps.hasCapability(NetworkCapabilities.NET_CAPABILITY_INTERNET) &&
            caps.hasCapability(NetworkCapabilities.NET_CAPABILITY_NOT_VPN)
    }
}
