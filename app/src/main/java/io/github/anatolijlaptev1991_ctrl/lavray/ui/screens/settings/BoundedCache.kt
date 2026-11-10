// SPDX-License-Identifier: GPL-3.0-only
package io.github.anatolijlaptev1991_ctrl.lavray.ui.screens.settings

/**
 * A small least-recently-used cache with a fixed entry limit and no Android dependency, so its policy is tested on
 * the JVM. Thread-safe: icons are loaded on a background dispatcher and read from composition.
 */
class BoundedCache<K : Any, V : Any>(private val maxEntries: Int) {
    private val map = object : LinkedHashMap<K, V>(16, 0.75f, true) {
        override fun removeEldestEntry(eldest: MutableMap.MutableEntry<K, V>): Boolean = size > maxEntries
    }

    init {
        require(maxEntries > 0) { "maxEntries must be positive" }
    }

    @Synchronized fun get(key: K): V? = map[key]

    @Synchronized fun put(key: K, value: V) {
        map[key] = value
    }

    @Synchronized fun evictAll() = map.clear()

    val size: Int @Synchronized get() = map.size
}
