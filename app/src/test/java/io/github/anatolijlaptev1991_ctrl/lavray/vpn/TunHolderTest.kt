// SPDX-License-Identifier: GPL-3.0-only
package io.github.anatolijlaptev1991_ctrl.lavray.vpn

import java.io.Closeable
import java.io.IOException
import org.junit.Test
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue

class TunHolderTest {
    private class Fake(val failOnClose: Boolean = false) : Closeable {
        var closed = 0
        override fun close() {
            closed++
            if (failOnClose) throw IOException("already closed")
        }
    }

    // Regression: the descriptor used to be detached and never closed, so the tun interface
    // outlived Disconnect and blackholed the device's traffic.
    @Test
    fun releaseClosesTheDescriptorOnce() {
        val holder = TunHolder<Fake>()
        val tun = Fake()
        holder.replace(tun)
        assertTrue(holder.isHeld())
        holder.release()
        holder.release()
        assertEquals(1, tun.closed)
        assertFalse(holder.isHeld())
    }

    // V-03: an interface that is replaced leaks for a moment; the same interface for the same needs is kept instead.
    @Test
    fun theHeldInterfaceServesTheNextRunWhenItWasBuiltFromTheSameSpec() {
        val holder = TunHolder<Fake>()
        val tun = Fake()
        val first = holder.replace(tun, spec = listOf("mtu 9000", "apps a,b"))
        val reused = holder.reuseIf(listOf("mtu 9000", "apps a,b")) { "copy of it" }
        assertEquals("copy of it", reused!!.first)
        assertEquals("a new run is a new generation", first + 1, reused.second)
        assertEquals("nothing was closed", 0, tun.closed)
        assertTrue(holder.isHeld())
        // A release decided against the previous run must not close what the new run uses.
        assertFalse(holder.releaseIfCurrent(first))
        assertEquals(0, tun.closed)
        assertTrue(holder.releaseIfCurrent(reused.second))
        assertEquals(1, tun.closed)
    }

    @Test
    fun anInterfaceBuiltFromSomethingElseOrWithoutASpecIsNotReused() {
        val holder = TunHolder<Fake>()
        assertEquals("nothing held", null, holder.reuseIf("a") { it })
        holder.replace(Fake(), spec = "a")
        assertEquals(null, holder.reuseIf("b") { it })
        val generation = holder.currentGeneration()
        assertEquals("a refused reuse changes nothing", generation, holder.currentGeneration())
        holder.replace(Fake())
        assertEquals("no spec: never handed on", null, holder.reuseIf("a") { it })
        holder.replace(Fake(), spec = "a")
        holder.release()
        assertEquals("released: gone, and so is its spec", null, holder.reuseIf("a") { it })
        holder.replace(Fake())
        assertEquals(null, holder.reuseIf("a") { it })
    }

    @Test
    fun newerTunnelClosesTheOlder() {
        val holder = TunHolder<Fake>()
        val first = Fake()
        val second = Fake()
        holder.replace(first)
        holder.replace(second)
        assertEquals(1, first.closed)
        assertEquals(0, second.closed)
    }

    @Test
    fun closeFailureIsSwallowed() {
        val holder = TunHolder<Fake>()
        holder.replace(Fake(failOnClose = true))
        holder.release()
        assertFalse(holder.isHeld())
    }

    // A late IDLE of the previous run must not close the interface that the next run just established.
    @Test
    fun releaseOfAnOlderGenerationIsIgnored() {
        val holder = TunHolder<Fake>()
        val first = Fake()
        val second = Fake()
        val g1 = holder.replace(first)
        val g2 = holder.replace(second)
        assertEquals(g2, holder.currentGeneration())
        assertFalse(holder.releaseIfCurrent(g1))
        assertEquals(0, second.closed)
        assertTrue(holder.isHeld())
        assertTrue(holder.releaseIfCurrent(g2))
        assertEquals(1, second.closed)
        assertFalse(holder.releaseIfCurrent(g2))
    }
}
