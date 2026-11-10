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

    @Test
    fun modeIsPassedToTheBuilder() {
        val seen = ArrayList<Boolean>()
        AppRuleApplier.apply(AppRules(listOf("com.a"), emptyList())) { _, allowed -> seen += allowed; true }
        AppRuleApplier.apply(AppRules(emptyList(), listOf("com.a"))) { _, allowed -> seen += allowed; true }
        assertEquals(listOf(true, false), seen)
    }
}
