// SPDX-License-Identifier: GPL-3.0-only
package io.github.anatolijlaptev1991_ctrl.lavray.vpn

import android.app.PendingIntent
import android.content.pm.PackageManager
import android.net.VpnService
import android.os.Build
import android.os.ParcelFileDescriptor
import java.io.IOException

/**
 * An established interface, the selected packages that are not in it ([skippedPackages]: not installed, or refused), and
 * the ones the Builder itself would not take ([refused]): with any of those the interface is not what its spec says.
 */
internal data class TunBuilt(
    val descriptor: ParcelFileDescriptor,
    val skippedPackages: List<String>,
    val refused: List<String> = emptyList(),
)

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
    /**
     * The uids of the selected packages that are installed, in the order of the list. The system turns names into uids
     * once, when the interface is established: an app that was removed and installed again has a new uid, and an
     * interface kept from before would still carry the old one (the app would go direct in "only the selected", or
     * through the tunnel in "all except"). A changed uid is a changed spec, so a new interface is built.
     */
    val uids: List<Int> = emptyList(),
) {
    companion object {
        /**
         * The spec for [options] with the per-app [rules]; [uidOf] answers null for a package that is not installed.
         * Throws [NoAllowedAppsException] when "only the selected apps" has none installed.
         */
        fun of(options: TunOptions, rules: AppRules, uidOf: (pkg: String) -> Int?): TunSpec {
            val names = rules.allowed.ifEmpty { rules.disallowed }
            val uids = names.associateWith(uidOf)
            val missing = AppRuleApplier.resolve(rules) { uids[it] != null }.skipped
            return TunSpec(options.mtu, options.inet4, options.inet6, options.dns, rules, missing, names.mapNotNull { uids[it] })
        }

        /**
         * Whether the interface that is still up carries uids that are not true for [next]. [held] is what that interface
         * was built from, null when that is not known. The names are the same and the apps behind them are not: one was
         * installed, removed or installed again. Android would then keep the old uids (see [AppRuleApplier.apply]), so the
         * new interface must make it resolve the names anew. Other names are another list anyway.
         */
        fun uidsStale(held: TunSpec?, next: TunSpec): Boolean =
            held == null || (held.rules == next.rules && (held.uids != next.uids || held.missing != next.missing))
    }
}

/** What this app is allowed to know about an installed package. */
internal object InstalledApps {
    /** The uid stands for "installed, uid unknown": the package manager did not answer, which is no ground to call the app missing. */
    const val UNKNOWN_UID = -1

    /** The package's uid for this user, or null when it is not installed (or is hidden from this app). */
    fun uidOf(pm: PackageManager, pkg: String): Int? = try {
        if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.TIRAMISU) {
            pm.getApplicationInfo(pkg, PackageManager.ApplicationInfoFlags.of(0L)).uid
        } else {
            @Suppress("DEPRECATION")
            pm.getApplicationInfo(pkg, 0).uid
        }
    } catch (_: PackageManager.NameNotFoundException) {
        null
    } catch (_: RuntimeException) {
        UNKNOWN_UID
    }
}

/** Turns the core's TUN options into an established VpnService interface. */
internal class TunEstablisher(
    private val service: VpnService,
    private val sessionName: String,
    private val configureIntent: PendingIntent?,
    private val uidOf: (pkg: String) -> Int? = { pkg -> InstalledApps.uidOf(service.packageManager, pkg) },
    private val appSelection: () -> AppSelection = { AppSelection() },
    /** Whether "Block connections without VPN" is on: an app kept out of the tunnel then has no network at all. */
    private val lockdown: () -> Boolean = { Build.VERSION.SDK_INT >= Build.VERSION_CODES.Q && service.isLockdownEnabled },
) {
    /** Whether the interface built last got its first app name twice; only [establish] touches it, one call at a time. */
    @Volatile private var namedTwice = false

    /**
     * The choice and the lockdown state the core's lists of this run were made from ([coreRoutes]); [specOf] builds the
     * interface from the same one. Read twice, a change in between (lockdown switched on, a list edited) could keep a
     * direct app in the tunnel with no core rule to send it direct, and the routing preset would send it to the server.
     */
    @Volatile private var runChoice: RunChoice? = null

    private data class RunChoice(val selection: AppSelection, val lockdown: Boolean)

    /**
     * Read once per run. A lockdown state that cannot be read counts as on: direct apps then stay in the tunnel and the
     * core sends them direct, which works with lockdown and without it; the other guess would cut them off.
     */
    private fun readChoice(): RunChoice =
        RunChoice(appSelection(), runCatching { lockdown() }.getOrDefault(true))

    /**
     * Opens the interface [spec] describes. The caller keeps the descriptor open for as long as the tunnel
     * should exist (the core gets its own duplicate) and closes it to remove the interface.
     *
     * [freshUids]: the interface this one replaces is still up and its uids are stale ([TunSpec.uidsStale]). The list of
     * names handed to the Builder is then made to differ from the previous one, so Android resolves the names anew;
     * otherwise it is handed exactly as last time, and Android may swap the device in place, as it always did.
     */
    fun establish(spec: TunSpec, freshUids: Boolean = false): TunBuilt {
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
        val twice = namedTwice != freshUids
        val refused = applyAppRules(builder, spec.rules, twice).skipped
        // The platform derives "metered" from the underlying networks we keep in sync with
        // setUnderlyingNetworks; the default (always metered) would throttle apps on Wi-Fi.
        if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.Q) builder.setMetered(false)
        configureIntent?.let { builder.setConfigureIntent(it) }
        val pfd = builder.establish() ?: throw IOException("VPN permission is not granted")
        namedTwice = twice
        return TunBuilt(pfd, (spec.missing + refused).distinct(), refused)
    }

    /**
     * What [establish] would build for [options] right now: the per-app choice and which of the chosen apps are installed
     * are read at this moment. Throws [NoAllowedAppsException] when "only the selected apps" has none installed.
     */
    fun specOf(options: TunOptions): TunSpec = TunSpec.of(options, rulesFor(options, runChoice ?: readChoice()), uidOf)

    /**
     * The "always through VPN" and "always direct" lists for this run of the core (Platform.AppRoutes), read at this
     * moment like the app list of the interface ([specOf]), so both follow the same choice and the same lockdown state.
     * Owners of connections can be asked for since Android 10 only.
     */
    fun coreRoutes(): String {
        val choice = readChoice()
        runChoice = choice
        return choice.selection.coreRoutesJson(choice.lockdown, canFindOwner = Build.VERSION.SDK_INT >= Build.VERSION_CODES.Q)
    }

    /**
     * Whether the interface of this run was built for a lockdown state that is not the real one, and it matters (some app
     * always goes direct). Android tells an app about lockdown only while that app's VPN is up: before the first interface
     * of a run started under lockdown it says "off", so "always direct" apps were kept outside, where lockdown cuts them
     * off. Asked once the interface is up; true means one rebuild puts them right.
     */
    fun lockdownStale(): Boolean {
        val choice = runChoice ?: return false
        if (choice.selection.direct.isEmpty()) return false
        val now = runCatching { lockdown() }.getOrNull() ?: return false
        return now != choice.lockdown
    }

    private fun rulesFor(options: TunOptions, choice: RunChoice): AppRules = try {
        AppRules.from(choice.selection.applyTo(options, choice.lockdown), service.packageName)
    } catch (_: IllegalArgumentException) {
        // The allow-list holds only this app (or only apps that always go direct): nothing else would be tunnelled.
        throw NoAllowedAppsException()
    }

    private fun applyAppRules(builder: VpnService.Builder, rules: AppRules, twice: Boolean): AppliedRules =
        AppRuleApplier.apply(rules, twice) { pkg, allowed ->
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
