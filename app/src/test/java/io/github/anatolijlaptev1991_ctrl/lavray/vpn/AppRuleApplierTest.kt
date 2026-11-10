// SPDX-License-Identifier: GPL-3.0-only
package io.github.anatolijlaptev1991_ctrl.lavray.vpn

import org.junit.Assert.assertEquals
import org.junit.Assert.assertThrows
import org.junit.Assert.assertTrue
import org.junit.Test

class AppRuleApplierTest {
    private val installed = setOf("com.a", "com.b")
    private val add = { pkg: String, _: Boolean -> pkg in installed }

    @Test
    fun missingSelectedAppIsSkippedAndReported() {
        val r = AppRuleApplier.apply(AppRules(listOf("com.a", "com.gone", "com.b"), emptyList()), add)
        assertEquals(listOf("com.a", "com.b"), r.applied)
        assertEquals(listOf("com.gone"), r.skipped)
    }

    @Test
    fun allowListResolvingToNothingIsTheOnlyFatalCase() {
        assertThrows(NoAllowedAppsException::class.java) {
            AppRuleApplier.apply(AppRules(listOf("com.gone", "com.gone2"), emptyList()), add)
        }
    }

    @Test
    fun missingExcludedAppNeedsNoRuleButIsReported() {
        val r = AppRuleApplier.apply(AppRules(emptyList(), listOf("com.gone", "com.a")), add)
        assertEquals(listOf("com.a"), r.applied)
        assertEquals(listOf("com.gone"), r.skipped)
    }

    @Test
    fun excludeModeWithNothingInstalledStillBuilds() {
        val r = AppRuleApplier.apply(AppRules(emptyList(), listOf("com.gone")), add)
        assertTrue(r.applied.isEmpty())
        assertEquals(listOf("com.gone"), r.skipped)
    }

    // V-04: the Builder takes any name, so the question "is it installed" is asked before anything is built.
    @Test
    fun anAllowListWithNothingInstalledIsRefusedBeforeAnythingIsBuilt() {
        assertThrows(NoAllowedAppsException::class.java) {
            AppRuleApplier.resolve(AppRules(listOf("com.gone", "com.gone2"), emptyList())) { it in installed }
        }
    }

    @Test
    fun whatIsMissingIsReportedInBothModes() {
        val only = AppRuleApplier.resolve(AppRules(listOf("com.a", "com.gone"), emptyList())) { it in installed }
        assertEquals(listOf("com.a"), only.applied)
        assertEquals(listOf("com.gone"), only.skipped)
        val except = AppRuleApplier.resolve(AppRules(emptyList(), listOf("com.gone", "com.b"))) { it in installed }
        assertEquals(listOf("com.b"), except.applied)
        assertEquals(listOf("com.gone"), except.skipped)
    }

    @Test
    fun anExcludeListWithNothingInstalledIsNotAnError() {
        val r = AppRuleApplier.resolve(AppRules(emptyList(), listOf("com.gone"))) { it in installed }
        assertTrue(r.applied.isEmpty())
        assertEquals(listOf("com.gone"), r.skipped)
    }

    // The held interface serves the next run only while the same apps are installed: a spec made after one of them
    // was removed is another spec, so a new interface is built (or the run is refused when none is left).
    @Test
    fun aSpecMadeAfterASelectedAppWasRemovedIsAnotherSpec() {
        val rules = AppRules(listOf("com.a", "com.b"), emptyList())
        val before = TunSpec(9000, listOf("172.19.0.1/30"), emptyList(), listOf("172.19.0.2"), rules)
        val after = before.copy(missing = AppRuleApplier.resolve(rules) { it == "com.a" }.skipped)
        assertEquals(listOf("com.b"), after.missing)
        assertTrue(before != after)
        assertEquals(before, before.copy(missing = AppRuleApplier.resolve(rules) { it in installed }.skipped))
    }

    @Test
    fun modeIsPassedToTheBuilder() {
        val seen = ArrayList<Boolean>()
        AppRuleApplier.apply(AppRules(listOf("com.a"), emptyList())) { _, allowed -> seen += allowed; true }
        AppRuleApplier.apply(AppRules(emptyList(), listOf("com.a"))) { _, allowed -> seen += allowed; true }
        assertEquals(listOf(true, false), seen)
    }
}
