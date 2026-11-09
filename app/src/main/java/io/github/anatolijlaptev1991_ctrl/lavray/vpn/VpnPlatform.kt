// SPDX-License-Identifier: GPL-3.0-only
package io.github.anatolijlaptev1991_ctrl.lavray.vpn

import android.net.ConnectivityManager
import android.net.InetAddresses
import android.net.Network
import android.net.VpnService
import android.os.Build
import android.os.ParcelFileDescriptor
import lavray.core.mobile.NetworkListener
import lavray.core.mobile.Platform
import java.io.IOException
import java.net.InetSocketAddress

/**
 * The host side of the Go core: everything it cannot do from native code on Android.
 * Called from Go threads. Holds no UI references.
 */
internal class VpnPlatform(
    private val service: VpnService,
    private val connectivity: ConnectivityManager,
    private val establisher: TunEstablisher,
) : Platform {
    private var monitor: DefaultNetworkMonitor? = null
    private var listener: NetworkListener? = null

    private val tun = TunHolder<ParcelFileDescriptor>()

    override fun openTun(optionsJSON: String): Int {
        val pfd = establisher.establish(TunOptions.parse(optionsJSON))
        // The core duplicates the descriptor; this one stays open (and so does the interface)
        // until the tunnel ends. A reconnect replaces and closes the previous one.
        tun.replace(pfd)
        // The new interface follows the default network until the monitor says otherwise.
        syncUnderlying(currentUnderlying)
        return pfd.fd
    }

    /** Ends the interface once the core has stopped using it. Safe to call at any time. */
    fun releaseTun() = tun.release()

    override fun protect(fd: Int) {
        if (!service.protect(fd)) throw IOException("VpnService.protect failed")
    }

    override fun findOwnerUid(proto: Int, srcIP: String, srcPort: Int, dstIP: String, dstPort: Int): Int {
        if (Build.VERSION.SDK_INT < Build.VERSION_CODES.Q) return -1
        return try {
            val src = InetSocketAddress(InetAddresses.parseNumericAddress(srcIP), srcPort)
            val dst = InetSocketAddress(InetAddresses.parseNumericAddress(dstIP), dstPort)
            connectivity.getConnectionOwnerUid(proto, src, dst)
        } catch (_: IllegalArgumentException) {
            -1
        } catch (_: SecurityException) {
            -1
        }
    }

    override fun packagesForUid(uid: Int): String =
        service.packageManager.getPackagesForUid(uid)?.joinToString(",").orEmpty()

    @Volatile
    private var currentUnderlying: Network? = null

    @Synchronized
    override fun startNetworkMonitor(l: NetworkListener) {
        closeNetworkMonitor()
        listener = l
        monitor = DefaultNetworkMonitor(connectivity) { net -> onDefaultNetwork(l, net) }.also { it.start() }
    }

    @Synchronized
    override fun closeNetworkMonitor() {
        monitor?.stop()
        monitor = null
        listener = null
        currentUnderlying = null
    }

    override fun interfaces(): String = InterfaceInfo.encodeList(DefaultNetworkMonitor.interfaces(connectivity))

    private fun onDefaultNetwork(l: NetworkListener, net: DefaultNetwork?) {
        currentUnderlying = net?.network
        syncUnderlying(net?.network)
        if (net == null) l.update("", -1, false) else l.update(net.name, net.index, net.metered)
    }

    // Keeps the tunnel's underlying network equal to the physical default so the system
    // reports the VPN's real transport and metered state. Harmless before the tunnel exists.
    private fun syncUnderlying(network: Network?) {
        service.setUnderlyingNetworks(network?.let { arrayOf(it) })
    }

    fun close() {
        closeNetworkMonitor()
        releaseTun()
    }
}
