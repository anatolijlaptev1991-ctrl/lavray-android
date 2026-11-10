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

/**
 * Everything [TunEstablisher.establish] builds an interface from. Two equal specs give two interchangeable interfaces, so
 * the one that is already up can serve the next run of the core.
 */
internal data class TunSpec(
    val mtu: Int,
    val inet4: List<String>,
    val inet6: List<String>,
    val dns: List<String>,
    val rules: AppRules,
    /** Selected packages this device does not have at the moment the spec was made. Part of the identity on purpose. */
    val missing: List<String> = emptyList(),
)

/** Whether a package is installed for this user, as far as this app is allowed to see. */
internal object InstalledApps {
    fun has(pm: PackageManager, pkg: String): Boolean = try {
        if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.TIRAMISU) {
            pm.getApplicationInfo(pkg, PackageManager.ApplicationInfoFlags.of(0L))
        } else {
            @Suppress("DEPRECATION")
            pm.getApplicationInfo(pkg, 0)
        }
        true
    } catch (_: PackageManager.NameNotFoundException) {
        false
    } catch (_: RuntimeException) {
        // The package manager did not answer: no ground to call the app missing.
        true
    }
}

/** Turns the core's TUN options into an established VpnService interface. */
internal class TunEstablisher(
    private val service: VpnService,
    private val sessionName: String,
    private val configureIntent: PendingIntent?,
    private val installed: (pkg: String) -> Boolean = { pkg -> InstalledApps.has(service.packageManager, pkg) },
    private val appSelection: () -> AppSelection = { AppSelection() },
) {
    /**
     * Opens the interface [spec] describes. The caller keeps the descriptor open for as long as the tunnel
     * should exist (the core gets its own duplicate) and closes it to remove the interface.
     */
    fun establish(spec: TunSpec): TunBuilt {
        require(spec.inet4.isNotEmpty() || spec.inet6.isNotEmpty()) { "TUN options carry no address" }
        val builder = service.Builder().setSession(sessionName)
        if (spec.mtu > 0) builder.setMtu(spec.mtu)
        for (text in spec.inet4) Cidr.parse(text).let { builder.addAddress(it.address, it.prefix) }
        for (text in spec.inet6) Cidr.parse(text).let { builder.addAddress(it.address, it.prefix) }
        if (spec.inet4.isNotEmpty()) builder.addRoute("0.0.0.0", 0)
        if (spec.inet6.isNotEmpty()) builder.addRoute("::", 0)
        for (server in spec.dns) builder.addDnsServer(server)
        // Every selected name goes to the Builder, the missing ones too: the system resolves names this app cannot see
        // (an app without a launcher entry) and ignores the ones that are not there.
        val refused = applyAppRules(builder, spec.rules).skipped
        // The platform derives "metered" from the underlying networks we keep in sync with
        // setUnderlyingNetworks; the default (always metered) would throttle apps on Wi-Fi.
        if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.Q) builder.setMetered(false)
        configureIntent?.let { builder.setConfigureIntent(it) }
        val pfd = builder.establish() ?: throw IOException("VPN permission is not granted")
        return TunBuilt(pfd, (spec.missing + refused).distinct())
    }

    /**
     * What [establish] would build for [options] right now: the per-app choice and which of the chosen apps are installed
     * are read at this moment. Throws [NoAllowedAppsException] when "only the selected apps" has none installed.
     */
    fun specOf(options: TunOptions): TunSpec {
        val rules = rulesFor(options)
        val missing = AppRuleApplier.resolve(rules, installed).skipped
        return TunSpec(options.mtu, options.inet4, options.inet6, options.dns, rules, missing)
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
