// SPDX-License-Identifier: GPL-3.0-only
package io.github.anatolijlaptev1991_ctrl.lavray.vpn

import android.net.ConnectivityManager
import android.net.InetAddresses
import android.net.Network
import android.net.VpnService
import android.os.Build
import android.os.ParcelFileDescriptor
import android.util.Log
import io.github.anatolijlaptev1991_ctrl.lavray.ipc.FailureKind
import lavray.core.mobile.NetworkListener
import lavray.core.mobile.Platform
import java.io.IOException
import java.net.InetSocketAddress

/** What the platform tells the service about the interface it builds; called from Go threads, must not block. */
internal interface PlatformListener {
    /** A new interface replaced the previous one. [skippedPackages] are selected apps that are not installed. */
    fun onTunEstablished(generation: Int, skippedPackages: List<String>)

    /** The interface could not be built for a reason that retrying cannot fix. */
    fun onTunFailed(failure: TunFailure)
}

/**
 * The host side of the Go core: everything it cannot do from native code on Android.
 * Called from Go threads. Holds no UI references.
 */
internal class VpnPlatform(
    private val service: VpnService,
    private val connectivity: ConnectivityManager,
    private val establisher: TunEstablisher,
    private val events: PlatformListener,
) : Platform {
    private var monitor: DefaultNetworkMonitor? = null
    private var listener: NetworkListener? = null

    private val tun = TunHolder<PfdFd>()
    private val handoff = TunHandoff(tun)

    override fun openTun(optionsJSON: String): Int {
        val options = TunOptions.parse(optionsJSON)
        val spec: TunSpec
        val built = try {
            spec = establisher.specOf(options)
            // A rebuild that needs the very interface that is still held (a server switch, a reconnect, a core restart
            // for routing or DNS) keeps it: replacing an interface lets connections out through the physical network
            // for a moment, keeping it does not. Only a change of the interface itself (MTU, addresses, DNS, the app
            // list, or which of the listed apps are installed) builds a new one.
            handoff.handHeld(spec) { generation -> onTunReady(generation, spec.missing) }?.let { return it }
            // The interface the system still has is replaced in place. If it was built for the same names but other
            // apps were behind them then (one was installed or installed again since), Android must resolve them anew.
            val freshUids = tun.isHeld() && TunSpec.uidsStale(tun.heldSpec() as? TunSpec, spec)
            establisher.establish(spec, freshUids)
        } catch (e: NoAllowedAppsException) {
            events.onTunFailed(TunFailure(FailureKind.APPS_NONE, e.message))
            throw e
        }
        // The core gets an independent descriptor (dup + detach) and owns it; this one stays open, and so does
        // the interface, until the tunnel ends. A late release from an older run cannot close a descriptor the
        // core is using, and the core closing its own never ends the interface.
        // A selected app that is not installed does not stop the interface from serving the next run: what is missing
        // (and every uid) is part of the spec, so an install, a removal or a reinstall makes another spec. Only an
        // interface the Builder itself refused a name for is never kept: it is not what its spec says.
        return handoff.hand(PfdFd(built.descriptor), spec.takeIf { built.refused.isEmpty() }) { generation ->
            onTunReady(generation, built.skippedPackages)
        }
    }

    private fun onTunReady(generation: Int, skippedPackages: List<String>) {
        events.onTunEstablished(generation, skippedPackages)
        // The interface follows the default network until the monitor says otherwise; a refusal is not fatal.
        runCatching { syncUnderlying(currentUnderlying) }
    }

    /** Ends the interface once the core has stopped using it. Safe to call at any time. */
    fun releaseTun() = tun.release()

    /** Ends the interface only if it is still the one of [generation]; false when a newer one replaced it. */
    fun releaseTunIfCurrent(generation: Int): Boolean = tun.releaseIfCurrent(generation)

    fun tunGeneration(): Int = tun.currentGeneration()

    /** See [TunEstablisher.lockdownStale]: one Binder call, only when some app always goes direct. */
    fun lockdownStale(): Boolean = establisher.lockdownStale()

    fun isTunHeld(): Boolean = tun.isHeld()

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

    // No error result in the Go interface: an exception escaping into native code would kill the process.
    override fun packagesForUid(uid: Int): String = runCatching {
        service.packageManager.getPackagesForUid(uid)?.joinToString(",").orEmpty()
    }.getOrDefault("")

    /** The "always through VPN" and "always direct" lists, asked for at every start of the core's run; "" is none. */
    override fun appRoutes(): String = try {
        establisher.coreRoutes()
    } catch (e: RuntimeException) {
        // Never into native code. The run goes on without the lists; the interface is still built from the stored choice.
        Log.w(TAG, "app lists for the core could not be made", e)
        ""
    }

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

    private companion object {
        const val TAG = "LavrayPlatform"
    }
}
