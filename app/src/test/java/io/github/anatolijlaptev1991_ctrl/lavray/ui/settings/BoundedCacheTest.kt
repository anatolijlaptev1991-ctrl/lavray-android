// SPDX-License-Identifier: GPL-3.0-only
package io.github.anatolijlaptev1991_ctrl.lavray.ui.settings

import io.github.anatolijlaptev1991_ctrl.lavray.ui.screens.settings.BoundedCache
import org.junit.Assert.assertEquals
import org.junit.Assert.assertNull
import org.junit.Test

class BoundedCacheTest {
    @Test fun dropsTheLeastRecentlyUsedEntryWhenFull() {
        val cache = BoundedCache<String, Int>(2)
        cache.put("a", 1)
        cache.put("b", 2)
        cache.get("a")
        cache.put("c", 3)
        assertNull(cache.get("b"))
        assertEquals(1, cache.get("a"))
        assertEquals(3, cache.get("c"))
        assertEquals(2, cache.size)
    }

    @Test fun evictAllEmptiesTheCache() {
        val cache = BoundedCache<String, Int>(4)
        cache.put("a", 1)
        cache.put("b", 2)
        cache.evictAll()
        assertEquals(0, cache.size)
        assertNull(cache.get("a"))
    }

    @Test fun aSecondPutOfTheSameKeyReplacesTheValueWithoutGrowing() {
        val cache = BoundedCache<String, Int>(2)
        cache.put("a", 1)
        cache.put("a", 5)
        assertEquals(1, cache.size)
        assertEquals(5, cache.get("a"))
    }
}
