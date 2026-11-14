// SPDX-License-Identifier: GPL-3.0-only
package io.github.anatolijlaptev1991_ctrl.lavray.vpn

import io.github.anatolijlaptev1991_ctrl.lavray.ui.screens.settings.canonical
import org.junit.Assert.assertEquals
import org.junit.Assert.assertNotEquals
import org.junit.Assert.assertNull
import org.junit.Assert.assertThrows
import org.junit.Test

/** The "always through VPN" and "always direct" lists: storage, exclusivity, the tunnel's app list and the core's rules. */
class AppRouteListsTest {
    private val core = TunOptions(mtu = 9000, inet4 = listOf("172.19.0.1/30"), excludePackages = listOf("core.excluded"))

    private fun sel(
        mode: AppMode = AppMode.All,
        packages: Set<String> = emptySet(),
        viaVpn: Set<String> = emptySet(),
        direct: Set<String> = emptySet(),
    ) = AppSelection(mode, packages, viaVpn, direct)

    // ---- storage

    @Test
    fun theListsSurviveTheWireAndAnOlderTextStillReads() {
        val s = sel(AppMode.Except, setOf("x.y"), viaVpn = setOf("org.telegram.messenger"), direct = setOf("ru.bank.app", "com.game.one"))
        assertEquals(s, AppSelection.parse(s.toJson()))
        // Written by 0.9.4, before the lists existed.
        assertEquals(sel(AppMode.Only, setOf("a.b")), AppSelection.parse("""{"mode":"only","packages":["a.b"]}"""))
    }

    @Test
    fun anAppInBothListsStaysOnlyInTheVpnList() {
        val s = AppSelection.parse("""{"mode":"all","proxy":["a.b","c.d"],"direct":["a.b","e.f","not a package"]}""")
        assertEquals(setOf("a.b", "c.d"), s.viaVpn)
        assertEquals(setOf("e.f"), s.direct)
    }

    // ---- exclusivity

    @Test
    fun anAppInOneListCannotBeAddedToTheOther() {
        val s = sel(viaVpn = setOf("a.b"))
        assertNull(s.toggledIn(AppRoute.Direct, "a.b"))
        assertEquals(AppRoute.ViaVpn, s.routeOf("a.b"))
        // Out of the first list, it may go to the other.
        val moved = s.toggledIn(AppRoute.ViaVpn, "a.b")!!.toggledIn(AppRoute.Direct, "a.b")!!
        assertEquals(emptySet<String>(), moved.viaVpn)
        assertEquals(setOf("a.b"), moved.direct)
    }

    @Test
    fun togglingOneListLeavesTheRestAlone() {
        val s = sel(AppMode.Only, setOf("p.q"), viaVpn = setOf("a.b"), direct = setOf("c.d"))
        val next = s.toggledIn(AppRoute.ViaVpn, "e.f")!!
        assertEquals(setOf("a.b", "e.f"), next.viaVpn)
        assertEquals(setOf("c.d"), next.direct)
        assertEquals(setOf("p.q"), next.packages)
        assertEquals(AppMode.Only, next.mode)
    }

    // ---- the tunnel's app list

    @Test
    fun allAppsKeepsDirectAppsOutAndVpnAppsIn() {
        val s = sel(viaVpn = setOf("core.excluded"), direct = setOf("d.one"))
        val out = s.applyTo(core)
        assertEquals(emptyList<String>(), out.includePackages)
        assertEquals(listOf("d.one"), out.excludePackages)
    }

    @Test
    fun allExceptGivesWayToTheVpnList() {
        val s = sel(AppMode.Except, setOf("x.one", "v.one"), viaVpn = setOf("v.one"), direct = setOf("d.one"))
        assertEquals(listOf("core.excluded", "x.one", "d.one"), s.applyTo(core).excludePackages)
    }

    @Test
    fun onlyTheChosenTakesTheVpnListInAndTheDirectListOut() {
        val s = sel(AppMode.Only, setOf("a.one", "d.one"), viaVpn = setOf("v.one"), direct = setOf("d.one"))
        val out = s.applyTo(core)
        assertEquals(listOf("a.one", "v.one"), out.includePackages)
        assertEquals(emptyList<String>(), out.excludePackages)
    }

    @Test
    fun onlyTheChosenWithEveryChosenAppDirectTunnelsNothing() {
        val s = sel(AppMode.Only, setOf("d.one"), direct = setOf("d.one"))
        assertThrows(IllegalArgumentException::class.java) { s.applyTo(core) }
        // With an app that must go through VPN there is something to tunnel.
        assertEquals(listOf("v.one"), s.copy(viaVpn = setOf("v.one")).applyTo(core).includePackages)
    }

    @Test
    fun withLockdownDirectAppsStayInTheTunnelSoTheyKeepTheirNetwork() {
        val all = sel(direct = setOf("d.one"))
        assertEquals(listOf("core.excluded"), all.applyTo(core, lockdown = true).excludePackages)
        val except = sel(AppMode.Except, setOf("d.one", "x.one"), direct = setOf("d.one"))
        assertEquals(listOf("core.excluded", "x.one"), except.applyTo(core, lockdown = true).excludePackages)
        val only = sel(AppMode.Only, setOf("a.one"), direct = setOf("d.one"))
        assertEquals(listOf("a.one", "d.one"), only.applyTo(core, lockdown = true).includePackages)
    }

    @Test
    fun withoutListsNothingChanges() {
        for (mode in AppMode.entries) {
            val s = sel(mode, setOf("a.one"))
            assertEquals(s.copy(viaVpn = emptySet()).applyTo(core), s.applyTo(core, lockdown = true))
        }
    }

    @Test
    fun theOwnPackageNeverEntersTheTunnelEvenFromTheVpnList() {
        val rules = AppRules.from(sel(viaVpn = setOf("own.app")).applyTo(core), "own.app")
        assertEquals(listOf("own.app"), rules.disallowed.filter { it == "own.app" })
        val only = AppRules.from(sel(AppMode.Only, setOf("a.one"), viaVpn = setOf("own.app")).applyTo(core), "own.app")
        assertEquals(listOf("a.one"), only.allowed)
    }

    // ---- the core's rules

    @Test
    fun theCoreGetsOnlyWhatARuleCanUse() {
        val s = sel(viaVpn = setOf("z.one", "a.one"), direct = setOf("d.one"))
        assertEquals("""{"proxy":["a.one","z.one"],"direct":[]}""", s.coreRoutesJson(lockdown = false, canFindOwner = true))
        assertEquals("""{"proxy":["a.one","z.one"],"direct":["d.one"]}""", s.coreRoutesJson(lockdown = true, canFindOwner = true))
        // Android 9 and older cannot name a connection's owner: a rule could never match, and would cost a call per connection.
        assertEquals("", s.coreRoutesJson(lockdown = true, canFindOwner = false))
        // Direct apps alone are outside the tunnel: the core never sees them, so it gets nothing and looks up nobody.
        assertEquals("", sel(direct = setOf("d.one")).coreRoutesJson(lockdown = false, canFindOwner = true))
        assertEquals("", sel().coreRoutesJson(lockdown = true, canFindOwner = true))
    }

    // ---- reconnect prompt

    @Test
    fun aChangeOfEitherListIsATunnelChange() {
        val base = sel(AppMode.Except, setOf("x.one"))
        assertNotEquals(base.canonical(), base.copy(viaVpn = setOf("v.one")).canonical())
        assertNotEquals(base.canonical(), base.copy(direct = setOf("d.one")).canonical())
        // "Only" with nothing chosen is "all apps", lists included.
        assertEquals(sel(viaVpn = setOf("v.one")).canonical(), sel(AppMode.Only, viaVpn = setOf("v.one")).canonical())
    }
}
