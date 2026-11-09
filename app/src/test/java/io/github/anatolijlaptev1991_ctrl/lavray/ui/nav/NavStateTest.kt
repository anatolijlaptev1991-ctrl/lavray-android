// SPDX-License-Identifier: GPL-3.0-only
package io.github.anatolijlaptev1991_ctrl.lavray.ui.nav

import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNull
import org.junit.Assert.assertSame
import org.junit.Assert.assertTrue
import org.junit.Test

class NavStateTest {
    @Test
    fun startsOnConnectionAndCannotGoBack() {
        val s = NavState()
        assertEquals("connection", s.section)
        assertFalse(s.canPop)
        assertNull(s.pop())
    }

    @Test
    fun backFromAnotherSectionReturnsToTheStartSectionThenExits() {
        val s = NavState().selectSection("servers")
        assertTrue(s.canPop)
        val home = s.pop()!!
        assertEquals("connection", home.section)
        assertNull(home.pop())
    }

    @Test
    fun pushedScreensPopBeforeSections() {
        val s = NavState().selectSection("settings").push("backup").push("scan")
        assertEquals(listOf("backup", "scan"), s.pushed)
        val a = s.pop()!!
        assertEquals(listOf("backup"), a.pushed)
        assertEquals("settings", a.section)
        val b = a.pop()!!
        assertTrue(b.pushed.isEmpty())
        assertEquals("settings", b.section)
        assertEquals("connection", b.pop()!!.section)
    }

    @Test
    fun tabsAreRememberedPerSectionAndAreNotBackEntries() {
        val s = NavState().selectSection("diag").selectTab("diag", "log").selectSection("servers").selectTab("servers", "subscriptions")
        assertEquals("log", s.tabOf("diag", "checks"))
        assertEquals("subscriptions", s.tabOf("servers", "profiles"))
        assertEquals("connection", s.pop()!!.section)
        assertEquals("checks", s.tabOf("rules", "checks"))
    }

    @Test
    fun reselectingTheSectionClearsPushedScreensAndSameStateIsReturnedUnchanged() {
        val s = NavState().selectSection("settings").push("backup")
        assertTrue(s.selectSection("settings").pushed.isEmpty())
        val plain = NavState().selectSection("servers")
        assertSame(plain, plain.selectSection("servers"))
        assertSame(s, s.push("backup"))
    }

    @Test
    fun savedFormRoundTrips() {
        val s = NavState().selectSection("diag").selectTab("diag", "conns").selectTab("servers", "profiles").push("stats")
        assertEquals(s, NavState.fromList(s.toList()))
        assertEquals(NavState(), NavState.fromList(emptyList()))
        assertEquals(NavState(), NavState.fromList(NavState().toList()))
    }

    @Test
    fun enterShiftFollowsTheDirectionOfTheMove() {
        val servers = NavState().selectSection("servers")
        assertEquals(Shift(0, 1), enterShift(NavState(), servers))
        assertEquals(Shift(0, -1), enterShift(servers, NavState()))
        assertEquals(Shift(1, 0), enterShift(servers, servers.selectTab("servers", "subscriptions")))
        assertEquals(Shift(1, 0), enterShift(servers, servers.push("stats")))
        assertEquals(Shift(-1, 0), enterShift(servers.push("stats"), servers))
        assertEquals(Shift.None, enterShift(null, servers))
    }

    @Test
    fun registryHasTheFiveDesktopSectionsWithTheirTabs() {
        val ids = AppRegistry.sections.map { it.id }
        assertEquals(listOf("connection", "servers", "rules", "diag", "settings"), ids)
        assertEquals(listOf("profiles", "subscriptions"), AppRegistry.section("servers").tabs.map { it.id })
        assertEquals(listOf("routing", "dns"), AppRegistry.section("rules").tabs.map { it.id })
        assertEquals(listOf("checks", "conns", "log"), AppRegistry.section("diag").tabs.map { it.id })
        assertEquals(listOf("settings", "updates"), AppRegistry.section("settings").tabs.map { it.id })
        assertTrue(AppRegistry.pushedScreen("backup") != null && AppRegistry.pushedScreen("stats") != null && AppRegistry.pushedScreen("scan") != null)
        assertEquals(AppRegistry.sections.flatMap { s -> s.tabs.map { it.id } }.size, AppRegistry.sections.flatMap { s -> s.tabs.map { it.id } }.toSet().size)
    }
}
