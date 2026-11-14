// SPDX-License-Identifier: GPL-3.0-only
package io.github.anatolijlaptev1991_ctrl.lavray.ui.rules

import io.github.anatolijlaptev1991_ctrl.lavray.api.gen.Outbound
import io.github.anatolijlaptev1991_ctrl.lavray.api.gen.ProcessRule
import io.github.anatolijlaptev1991_ctrl.lavray.api.gen.RoutingPreset
import io.github.anatolijlaptev1991_ctrl.lavray.ui.core.ErrorKind
import io.github.anatolijlaptev1991_ctrl.lavray.ui.screens.rules.AddResult
import io.github.anatolijlaptev1991_ctrl.lavray.ui.screens.rules.ListEdit
import io.github.anatolijlaptev1991_ctrl.lavray.ui.screens.rules.ListKind
import io.github.anatolijlaptev1991_ctrl.lavray.ui.screens.rules.PresetCatalog
import io.github.anatolijlaptev1991_ctrl.lavray.ui.screens.rules.PresetDraft
import io.github.anatolijlaptev1991_ctrl.lavray.ui.screens.rules.PresetPatch
import io.github.anatolijlaptev1991_ctrl.lavray.ui.screens.rules.PresetsAction
import io.github.anatolijlaptev1991_ctrl.lavray.ui.screens.rules.PresetsReducer
import io.github.anatolijlaptev1991_ctrl.lavray.ui.screens.rules.PresetsState
import io.github.anatolijlaptev1991_ctrl.lavray.ui.screens.rules.ProcessPlatform
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test

class PresetLogicTest {
    private fun builtin(id: String) = RoutingPreset(id = id, displayName = "preset.$id", builtin = true, revision = 3)
    private fun custom(id: String, name: String = id) = RoutingPreset(id = "custom:$id", displayName = name, builtin = false, revision = 1)

    private val all = listOf(
        custom("mine"), builtin("all-direct"), builtin("global"), builtin("lav-cn"), builtin("lav-ru"), builtin("all-proxy"), custom("other"),
        builtin("lav-ru-linux"),
    )

    // ---- catalog

    @Test
    fun builtinPresetsComeFirstInTheFixedOrderWithTheLinuxPresetOnTop() {
        val ids = PresetCatalog.sorted(all).map { it.id }
        assertEquals(listOf("lav-ru-linux", "lav-ru", "lav-cn", "global", "all-proxy", "all-direct", "custom:mine", "custom:other"), ids)
    }

    @Test
    fun anUnknownBuiltinGoesAfterTheKnownOnesAndBeforeCustomOnes() {
        val ids = PresetCatalog.sorted(listOf(custom("a"), builtin("future"), builtin("lav-ru"))).map { it.id }
        assertEquals(listOf("lav-ru", "future", "custom:a"), ids)
    }

    @Test
    fun theLinuxPresetIsActiveWhenTheCoreNamesNoPreset() {
        val sorted = PresetCatalog.sorted(all)
        assertEquals("lav-ru-linux", PresetCatalog.effectiveActiveId(sorted, ""))
        assertEquals("lav-ru-linux", PresetCatalog.effectiveActiveId(sorted, "gone"))
        assertEquals("lav-ru", PresetCatalog.effectiveActiveId(sorted, "lav-ru"))
        // A core without the Linux preset (an older one) falls back to LAV Preset RU.
        assertEquals("lav-ru", PresetCatalog.effectiveActiveId(sorted.filter { it.id != "lav-ru-linux" }, ""))
        assertEquals("custom:mine", PresetCatalog.effectiveActiveId(sorted, "custom:mine"))
        assertEquals("global", PresetCatalog.effectiveActiveId(listOf(builtin("global")), ""))
        assertEquals("", PresetCatalog.effectiveActiveId(emptyList(), ""))
    }

    @Test
    fun ruleCountAddsAllListsAndAppRules() {
        val p = RoutingPreset(
            proxyDomain = listOf("a", "b"), directDomain = listOf("c"), blockDomain = listOf("d"),
            proxyIp = listOf("1.1.1.1"), directIp = listOf("geoip:ru"), blockIp = listOf("2.2.2.2"),
            processRules = listOf(ProcessRule(listOf("x.exe"))),
        )
        assertEquals(8, PresetCatalog.ruleCount(p))
        assertEquals(1, PresetCatalog.inertOnPhone(p))
    }

    @Test
    fun aRawDictionaryKeyIsNeverShownAsAName() {
        assertEquals("future", PresetCatalog.fallbackName(builtin("future")))
        assertEquals("My set", PresetCatalog.fallbackName(custom("m", "My set")))
        assertTrue(PresetCatalog.textOf("lav-ru")?.brand == "LAV Preset RU")
        assertTrue(PresetCatalog.textOf("lav-ru-linux")?.brand == "LAV Preset RU Linux")
        assertNull(PresetCatalog.textOf("custom:mine"))
    }

    @Test
    fun processRulePlatformsAreReportedHonestly() {
        assertEquals(ProcessPlatform.AllSystems, PresetCatalog.platformOf(ProcessRule(listOf("a"))))
        assertEquals(ProcessPlatform.WindowsOnly, PresetCatalog.platformOf(ProcessRule(listOf("a.exe"), platforms = listOf("windows"))))
        assertEquals(ProcessPlatform.Listed("Linux, macOS"), PresetCatalog.platformOf(ProcessRule(listOf("a"), platforms = listOf("linux", "macos"))))
    }

    // ---- edits

    @Test
    fun addingAnEntryAppendsItAndIgnoresRepeatsAndBlanks() {
        assertEquals(AddResult.Added(listOf("a.ru", "b.ru")), PresetDraft.addEntry(listOf("a.ru"), " b.ru "))
        assertEquals(AddResult.Exists, PresetDraft.addEntry(listOf("A.RU"), "a.ru"))
        assertEquals(AddResult.Empty, PresetDraft.addEntry(listOf("a.ru"), "   "))
    }

    @Test
    fun aPatchChangesOnlyWhatItNamesAndLeavesTheOriginalAlone() {
        val base = custom("m").copy(proxyDomain = listOf("a.com"), defaultOutbound = Outbound.OUTBOUND_PROXY)
        val added = PresetDraft.apply(base, PresetPatch(listEdit = ListEdit.Add(ListKind.DirectDomain, "b.ru")))
        assertEquals(listOf("b.ru"), added.directDomain)
        assertEquals(listOf("a.com"), added.proxyDomain)
        assertTrue(base.directDomain.isEmpty())
        val removed = PresetDraft.apply(added, PresetPatch(listEdit = ListEdit.Remove(ListKind.ProxyDomain, "a.com")))
        assertTrue(removed.proxyDomain.isEmpty())
        val renamed = PresetDraft.apply(base, PresetPatch(displayName = "  New  ", defaultOutbound = Outbound.OUTBOUND_BLOCK))
        assertEquals("New", renamed.displayName)
        assertEquals(Outbound.OUTBOUND_BLOCK, renamed.defaultOutbound)
    }

    @Test
    fun editsAreAppliedToTheNewestCopySoTwoQuickAddsBothSurvive() {
        val base = custom("m")
        val first = PresetDraft.apply(base, PresetPatch(listEdit = ListEdit.Add(ListKind.ProxyDomain, "a.com")))
        val second = PresetDraft.apply(first, PresetPatch(listEdit = ListEdit.Add(ListKind.ProxyDomain, "b.com")))
        assertEquals(listOf("a.com", "b.com"), second.proxyDomain)
    }

    @Test
    fun removingAnAppRuleTakesOneOccurrence() {
        val rule = ProcessRule(listOf("a.exe"), Outbound.OUTBOUND_DIRECT)
        val base = custom("m").copy(processRules = listOf(rule, ProcessRule(listOf("b.exe")), rule))
        val next = PresetDraft.apply(base, PresetPatch(removeProcessRule = rule))
        assertEquals(2, next.processRules.size)
        assertEquals(listOf("b.exe"), next.processRules[0].processNames)
    }

    @Test
    fun anEditThatChangesNothingOrOnlyBookkeepingIsNotSent() {
        val base = custom("m").copy(proxyDomain = listOf("a.com"))
        assertFalse(PresetDraft.isChanged(base, PresetDraft.apply(base, PresetPatch(listEdit = ListEdit.Add(ListKind.ProxyDomain, "A.com")))))
        assertFalse(PresetDraft.isChanged(base, base.copy(revision = 9, sourceUpdated = true, builtinSourceId = "lav-ru")))
        assertTrue(PresetDraft.isChanged(base, base.copy(defaultOutbound = Outbound.OUTBOUND_BLOCK)))
    }

    @Test
    fun namesAreTrimmedCappedAndMustNotBeBlank() {
        assertFalse(PresetDraft.nameValid("  "))
        assertTrue(PresetDraft.nameValid(" x "))
        assertEquals(64, PresetDraft.cleanName("a".repeat(100)).length)
        assertEquals("x", PresetDraft.cleanName("  x "))
    }

    @Test
    fun everyListKindReadsAndWritesItsOwnList() {
        ListKind.entries.forEach { kind ->
            val p = kind.with(RoutingPreset(), listOf("e-${kind.name}"))
            assertEquals(listOf("e-${kind.name}"), kind.of(p))
            ListKind.entries.filter { it != kind }.forEach { other -> assertTrue(other.of(p).isEmpty()) }
        }
    }

    // ---- state

    @Test
    fun loadingSortsMarksTheDefaultActiveAndClosesAnEditorOfADeletedPreset() {
        var s = PresetsReducer.reduce(PresetsState(openId = "custom:gone"), PresetsAction.Loaded(all, ""))
        assertFalse(s.loading)
        assertEquals("lav-ru-linux", s.presets.first().id)
        assertEquals("lav-ru-linux", s.activeId)
        assertNull(s.openId)
        s = PresetsReducer.reduce(s, PresetsAction.Open("custom:mine"))
        s = PresetsReducer.reduce(s, PresetsAction.Loaded(all, "custom:mine"))
        assertEquals("custom:mine", s.open?.id)
        assertEquals("custom:mine", s.active?.id)
    }

    @Test
    fun aFailedLoadKeepsThePresetsAndNamesTheProblem() {
        val loaded = PresetsReducer.reduce(PresetsState(), PresetsAction.Loaded(all, "global"))
        val s = PresetsReducer.reduce(loaded, PresetsAction.Failed(ErrorKind.CoreUnavailable))
        assertEquals(ErrorKind.CoreUnavailable, s.error)
        assertEquals(all.size, s.presets.size)
    }

    @Test
    fun aSavedPresetReplacesItsOldCopyInPlace() {
        val loaded = PresetsReducer.reduce(PresetsState(), PresetsAction.Loaded(all, "custom:mine"))
        val edited = custom("mine").copy(proxyDomain = listOf("x.com"), revision = 2)
        val s = PresetsReducer.reduce(loaded, PresetsAction.Replace(edited))
        assertEquals(loaded.presets.map { it.id }, s.presets.map { it.id })
        assertEquals(2, s.presets.first { it.id == "custom:mine" }.revision)
    }

    @Test
    fun busyAndActiveAreSimpleFlags() {
        val s = PresetsReducer.reduce(PresetsReducer.reduce(PresetsState(), PresetsAction.Busy(true)), PresetsAction.Active("global"))
        assertTrue(s.busy)
        assertEquals("global", s.activeId)
    }

    @Test
    fun aPresetWithoutADefaultOutcomeSendsEverythingThroughTheProxy() {
        assertEquals(Outbound.OUTBOUND_PROXY, PresetsReducer.defaultOutbound(RoutingPreset()))
        assertEquals(Outbound.OUTBOUND_BLOCK, PresetsReducer.defaultOutbound(RoutingPreset(defaultOutbound = Outbound.OUTBOUND_BLOCK)))
    }
}
