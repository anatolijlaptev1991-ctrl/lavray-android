// SPDX-License-Identifier: GPL-3.0-only
package io.github.anatolijlaptev1991_ctrl.lavray.vpn

import android.app.PendingIntent
import android.content.pm.PackageManager
import android.net.VpnService
import android.os.Build
import android.os.ParcelFileDescriptor
import java.io.IOException

/** An established interface and the selected packages the Builder could not find. */
internal data class TunBuilt(val descriptor: ParcelFileDescriptor, val skippedPackages: List<String>)

/** Turns the core's TUN options into an established VpnService interface. */
internal class TunEstablisher(
    private val service: VpnService,
    private val sessionName: String,
    private val configureIntent: PendingIntent?,
    private val appSelection: () -> AppSelection = { AppSelection() },
) {
    /**
     * Opens the interface. The caller keeps the descriptor open for as long as the tunnel
     * should exist (the core gets its own duplicate) and closes it to remove the interface.
     */
    fun establish(options: TunOptions): TunBuilt {
        require(options.inet4.isNotEmpty() || options.inet6.isNotEmpty()) { "TUN options carry no address" }
        val builder = service.Builder().setSession(sessionName)
        if (options.mtu > 0) builder.setMtu(options.mtu)
        for (text in options.inet4) Cidr.parse(text).let { builder.addAddress(it.address, it.prefix) }
        for (text in options.inet6) Cidr.parse(text).let { builder.addAddress(it.address, it.prefix) }
        if (options.inet4.isNotEmpty()) builder.addRoute("0.0.0.0", 0)
        if (options.inet6.isNotEmpty()) builder.addRoute("::", 0)
        for (server in options.dns) builder.addDnsServer(server)
        val applied = applyAppRules(builder, rulesFor(options))
        // The platform derives "metered" from the underlying networks we keep in sync with
        // setUnderlyingNetworks; the default (always metered) would throttle apps on Wi-Fi.
        if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.Q) builder.setMetered(false)
        configureIntent?.let { builder.setConfigureIntent(it) }
        val pfd = builder.establish() ?: throw IOException("VPN permission is not granted")
        return TunBuilt(pfd, applied.skipped)
    }

    private fun rulesFor(options: TunOptions): AppRules = try {
        AppRules.from(appSelection().applyTo(options), service.packageName)
    } catch (_: IllegalArgumentException) {
        // The allow-list holds only this app: nothing else would be tunnelled.
        throw NoAllowedAppsException()
    }

    private fun applyAppRules(builder: VpnService.Builder, rules: AppRules): AppliedRules =
        AppRuleApplier.apply(rules) { pkg, allowed ->
            try {
                if (allowed) builder.addAllowedApplication(pkg) else builder.addDisallowedApplication(pkg)
                true
            } catch (_: PackageManager.NameNotFoundException) {
                // Not installed, or hidden by package visibility.
                false
            } catch (_: IllegalArgumentException) {
                false
            }
        }
}
