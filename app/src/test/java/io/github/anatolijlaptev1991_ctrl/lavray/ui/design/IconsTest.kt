// SPDX-License-Identifier: GPL-3.0-only
package io.github.anatolijlaptev1991_ctrl.lavray.ui.design

import org.junit.Assert.assertEquals
import org.junit.Test
import java.lang.reflect.Field

/** Two actions must not share one picture: every icon is drawn from its own path data. */
class IconsTest {
    private fun data(icon: LavIcon): String {
        val f: Field = LavIcon::class.java.getDeclaredField("data")
        f.isAccessible = true
        return f.get(icon) as String
    }

    @Test fun everyIconHasItsOwnDrawing() {
        val seen = LavIcon.entries.groupBy { data(it) }.filterValues { it.size > 1 }
        assertEquals("icons that share path data: $seen", emptyMap<String, List<LavIcon>>(), seen)
    }

    @Test fun duplicateIsNotCopy() {
        assertEquals(false, data(LavIcon.Duplicate) == data(LavIcon.Copy))
        assertEquals(false, data(LavIcon.Lock) == data(LavIcon.Sliders))
        assertEquals(false, data(LavIcon.Battery) == data(LavIcon.Sliders))
    }
}
