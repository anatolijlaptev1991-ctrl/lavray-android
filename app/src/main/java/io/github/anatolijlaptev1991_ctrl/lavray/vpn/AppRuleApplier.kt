// SPDX-License-Identifier: GPL-3.0-only
package io.github.anatolijlaptev1991_ctrl.lavray.vpn

import java.io.IOException

/** "Only the selected apps" resolved to nothing installed: the tunnel would carry no app, so it is not built. */
class NoAllowedAppsException : IOException("none of the selected apps is installed")

/** Which package names the Builder took and which it could not find (not installed, or not visible). */
data class AppliedRules(val applied: List<String>, val skipped: List<String>)

/** Applies [AppRules] package by package; a name the Builder refuses is skipped and reported, never fatal on its own. */
object AppRuleApplier {
    /**
     * The selected names split by what this device has, asked before anything is built: [AppliedRules.applied] are
     * installed, [AppliedRules.skipped] are not (or are hidden from this app by package visibility).
     *
     * The Builder cannot be trusted with this question. Its addAllowedApplication declares NameNotFoundException but
     * takes any name (it asks the package manager through a call that answers null for an unknown package instead of
     * throwing), and the system then tunnels the uids it can resolve. An allow-list of names that resolve to nothing
     * gives an interface that is up and carries no app at all: "Connected", and every app on the direct connection.
     * So an allow-list with nothing installed is refused here, and what is missing is reported from here.
     */
    fun resolve(rules: AppRules, installed: (pkg: String) -> Boolean): AppliedRules {
        val allowMode = rules.allowed.isNotEmpty()
        val names = if (allowMode) rules.allowed else rules.disallowed
        val (present, missing) = names.partition(installed)
        if (allowMode && present.isEmpty()) throw NoAllowedAppsException()
        return AppliedRules(present, missing)
    }

    /**
     * [add] hands one package to the Builder (allowed or disallowed as the mode requires) and returns false when
     * the platform does not know it. In allow-list mode an empty result is the one fatal case.
     *
     * [twice] hands the first accepted name a second time. The apps in the tunnel are the same (the system collects
     * uids into a set), but the list the system sees is another list, and that is the point: when an interface replaces
     * one that is still up and the lists of names are equal, Android keeps the uids it resolved for the old interface
     * (Vpn.updateLinkPropertiesInPlaceIfPossible) and only swaps the device. An app that was installed or installed
     * again in between, under the same name and a new uid, stays outside the tunnel. A list that differs makes Android
     * register the VPN network anew, resolving every name again, before it drops the old one.
     */
    fun apply(rules: AppRules, twice: Boolean = false, add: (pkg: String, allowed: Boolean) -> Boolean): AppliedRules {
        val allowMode = rules.allowed.isNotEmpty()
        val names = if (allowMode) rules.allowed else rules.disallowed
        val applied = ArrayList<String>()
        val skipped = ArrayList<String>()
        for (pkg in names) {
            if (add(pkg, allowMode)) applied += pkg else skipped += pkg
        }
        // An empty allow-list would silently tunnel every app: refuse instead.
        if (allowMode && applied.isEmpty()) throw NoAllowedAppsException()
        if (twice) applied.firstOrNull()?.let { add(it, allowMode) }
        return AppliedRules(applied, skipped)
    }
}
