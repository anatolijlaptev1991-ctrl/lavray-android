// SPDX-License-Identifier: GPL-3.0-only
package io.github.anatolijlaptev1991_ctrl.lavray.vpn

import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test

class UiBindTrackerTest {
    private val ui = UiBindTracker()

    @Test
    fun boundAfterBindUnboundAfterUnbind() {
        assertFalse(ui.bound)
        ui.onBind()
        assertTrue(ui.bound)
        ui.onUnbind()
        assertFalse(ui.bound)
    }

    // Regression (B-07): the UI leaves and returns while the tunnel keeps the service alive.
    @Test
    fun reboundAfterTheUiReturns() {
        ui.onBind()
        ui.onUnbind()
        ui.onRebind()
        assertTrue(ui.bound)
    }

    @Test
    fun strayUnbindNeverGoesNegative() {
        ui.onUnbind()
        ui.onBind()
        assertTrue(ui.bound)
    }
}

class ShutdownTransitionTest {
    @Test
    fun transitionKeepsTheServiceAliveAndForeground() {
        assertFalse(ShutdownPolicy.shouldStop(vpnActive = false, uiBound = false, autoConnectPending = false, transition = true))
        assertFalse(ShutdownPolicy.shouldLeaveForeground(false, false, transition = true))
        assertTrue(ShutdownPolicy.shouldBeForeground(false, false, transition = true))
    }

    @Test
    fun foregroundWheneverActiveOrPending() {
        assertTrue(ShutdownPolicy.shouldBeForeground(true, false))
        assertTrue(ShutdownPolicy.shouldBeForeground(false, true))
        assertFalse(ShutdownPolicy.shouldBeForeground(false, false))
    }
}
