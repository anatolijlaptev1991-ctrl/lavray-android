// SPDX-License-Identifier: GPL-3.0-only
package io.github.anatolijlaptev1991_ctrl.lavray.ui

import org.junit.Assert.assertEquals
import org.junit.Test

class DataChangesTest {
    @Test
    fun everyNotificationMovesTheVersionSoAKeyedReloadRuns() {
        val d = DataChanges()
        assertEquals(0, d.version.value)
        d.notifyChanged()
        d.notifyChanged()
        assertEquals(2, d.version.value)
    }
}
