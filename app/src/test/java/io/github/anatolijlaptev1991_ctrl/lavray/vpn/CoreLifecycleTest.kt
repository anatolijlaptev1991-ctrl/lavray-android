// SPDX-License-Identifier: GPL-3.0-only
package io.github.anatolijlaptev1991_ctrl.lavray.vpn

import org.junit.Assert.assertEquals
import org.junit.Assert.assertThrows
import org.junit.Test

class CoreLifecycleTest {
    private val log = ArrayList<String>()
    private val lifecycle = CoreLifecycle()

    private fun start(who: Any, name: String) = lifecycle.start(who, { log += "stop-$name" }) { log += "start-$name" }

    @Test
    fun ownerStopsItsOwnCore() {
        val a = Any()
        start(a, "a")
        lifecycle.stop(a) { log += "native-stop" }
        assertEquals(listOf("start-a", "native-stop"), log)
    }

    // The old service's exit thread runs after the new service already started its core.
    @Test
    fun lateStopOfAReplacedOwnerChangesNothing() {
        val old = Any()
        val new = Any()
        start(old, "old")
        start(new, "new")
        lifecycle.stop(old) { log += "late-native-stop" }
        assertEquals(listOf("start-old", "stop-old", "start-new"), log)
        lifecycle.stop(new) { log += "stop-new-native" }
        assertEquals("stop-new-native", log.last())
    }

    @Test
    fun newOwnerStopsThePreviousCoreBeforeStarting() {
        val a = Any()
        val b = Any()
        start(a, "a")
        start(b, "b")
        assertEquals(listOf("start-a", "stop-a", "start-b"), log)
    }

    @Test
    fun failedStartLeavesNoOwner() {
        val a = Any()
        assertThrows(IllegalStateException::class.java) {
            lifecycle.start(a, { log += "stop" }) { throw IllegalStateException("boom") }
        }
        lifecycle.stop(a) { log += "native-stop" }
        assertEquals(emptyList<String>(), log)
    }

    @Test
    fun stopWithoutStartIsANoop() {
        lifecycle.stop(Any()) { log += "x" }
        assertEquals(emptyList<String>(), log)
    }
}
