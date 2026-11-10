// SPDX-License-Identifier: GPL-3.0-only
package io.github.anatolijlaptev1991_ctrl.lavray.ui.settings

import io.github.anatolijlaptev1991_ctrl.lavray.ui.screens.settings.skippedInSelection
import io.github.anatolijlaptev1991_ctrl.lavray.vpn.AppMode
import io.github.anatolijlaptev1991_ctrl.lavray.vpn.AppSelection
import org.junit.Assert.assertEquals
import org.junit.Test

class SkippedAppsTest {
    private val skipped = listOf("a.gone", "b.gone")

    @Test fun onlyTheSkippedAppsOfTheCurrentChoiceAreCounted() {
        assertEquals(2, skippedInSelection(skipped, AppSelection(AppMode.Only, setOf("a.gone", "b.gone", "c.here"))))
        // The choice changed after the tunnel was built: an app that is no longer chosen is not "skipped" any more.
        assertEquals(1, skippedInSelection(skipped, AppSelection(AppMode.Except, setOf("a.gone"))))
        assertEquals(0, skippedInSelection(skipped, AppSelection(AppMode.Only, setOf("c.here"))))
    }

    @Test fun allAppsHasNothingToSkip() {
        assertEquals(0, skippedInSelection(skipped, AppSelection(AppMode.All, setOf("a.gone"))))
        assertEquals(0, skippedInSelection(emptyList(), AppSelection(AppMode.Only, setOf("a.gone"))))
    }
}
