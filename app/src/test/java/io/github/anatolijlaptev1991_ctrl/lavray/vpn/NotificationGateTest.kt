// SPDX-License-Identifier: GPL-3.0-only
package io.github.anatolijlaptev1991_ctrl.lavray.vpn

import io.github.anatolijlaptev1991_ctrl.lavray.ipc.VpnState
import org.junit.Assert.assertEquals
import org.junit.Assert.assertNull
import org.junit.Test

class NotificationGateTest {
    private val connecting = NotificationModel(VpnState.CONNECTING)
    private val connected = NotificationModel(VpnState.CONNECTED)
    private val waiting = NotificationModel(VpnState.WAITING_NETWORK)

    @Test
    fun postsWhileTheScreenIsOnAndSkipsRepeats() {
        val g = NotificationGate()
        assertEquals(connecting, g.forcePost(connecting))
        assertNull(g.offer(connecting, screenOn = true))
        assertEquals(connected, g.offer(connected, screenOn = true))
        assertNull(g.offer(connected, screenOn = true))
    }

    @Test
    fun nothingIsPostedWhileTheScreenIsOff() {
        val g = NotificationGate()
        g.forcePost(connecting)
        assertNull(g.offer(connected, screenOn = false))
        assertNull(g.offer(waiting, screenOn = false))
    }

    @Test
    fun screenOnReleasesOnlyTheLatestHeldModel() {
        val g = NotificationGate()
        g.forcePost(connecting)
        g.offer(connected, false)
        g.offer(waiting, false)
        assertEquals(waiting, g.onScreenOn())
        assertNull(g.onScreenOn())
    }

    @Test
    fun holdingTheAlreadyPostedModelReleasesNothing() {
        val g = NotificationGate()
        g.forcePost(connecting)
        g.offer(connected, false)
        g.offer(connecting, false) // back to what is on screen
        assertNull(g.onScreenOn())
    }
}
