// SPDX-License-Identifier: GPL-3.0-only
package io.github.anatolijlaptev1991_ctrl.lavray.vpn

import io.github.anatolijlaptev1991_ctrl.lavray.api.gen.CoreState
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test

class LinkWatchTest {
    private val watch = LinkWatch(graceMs = 1000)

    @Test
    fun coreRestartIsOnlyAGracePeriod() {
        assertEquals(emptyList<LinkEffect>(), watch.onCore(CoreState.CORE_STATE_CONNECTED))
        assertTrue(watch.established)
        assertEquals(emptyList<LinkEffect>(), watch.onCore(CoreState.CORE_STATE_DISCONNECTING))
        val grace = watch.onCore(CoreState.CORE_STATE_IDLE)
        assertEquals(listOf<LinkEffect>(LinkEffect.StartGrace(1, 1000)), grace)
        assertTrue(watch.inGrace)
        assertEquals(listOf<LinkEffect>(LinkEffect.CancelGrace), watch.onCore(CoreState.CORE_STATE_CONNECTING))
        assertFalse(watch.inGrace)
        assertEquals(emptyList<LinkEffect>(), watch.onCore(CoreState.CORE_STATE_CONNECTED))
        // The timer that was cancelled in the service may still fire: it changes nothing.
        assertEquals(emptyList<LinkEffect>(), watch.onGraceElapsed(1))
        assertTrue(watch.established)
    }

    @Test
    fun graceThatEndsWithTheCoreStillDownIsLost() {
        watch.onCore(CoreState.CORE_STATE_CONNECTED)
        watch.onCore(CoreState.CORE_STATE_ERROR)
        assertEquals(listOf<LinkEffect>(LinkEffect.Lost), watch.onGraceElapsed(1))
        assertTrue(watch.lost)
        assertTrue(watch.established) // the rebuild after a loss may still be held as a transition
        assertFalse(watch.inGrace)
    }

    @Test
    fun afterALossFurtherDownEventsStartNoNewGraceUntilConnectedAgain() {
        watch.onCore(CoreState.CORE_STATE_CONNECTED)
        watch.onCore(CoreState.CORE_STATE_IDLE)
        watch.onGraceElapsed(1)
        assertEquals(emptyList<LinkEffect>(), watch.onCore(CoreState.CORE_STATE_ERROR))
        watch.onCore(CoreState.CORE_STATE_CONNECTED)
        assertFalse(watch.lost)
        assertEquals(listOf<LinkEffect>(LinkEffect.StartGrace(2, 1000)), watch.onCore(CoreState.CORE_STATE_IDLE))
    }

    @Test
    fun staleTimerOfAnEarlierGraceDoesNotEndTheLaterOne() {
        watch.onCore(CoreState.CORE_STATE_CONNECTED)
        watch.onCore(CoreState.CORE_STATE_IDLE)
        watch.onCore(CoreState.CORE_STATE_CONNECTING)
        watch.onCore(CoreState.CORE_STATE_CONNECTED)
        val second = watch.onCore(CoreState.CORE_STATE_IDLE)
        assertEquals(listOf<LinkEffect>(LinkEffect.StartGrace(2, 1000)), second)
        assertEquals(emptyList<LinkEffect>(), watch.onGraceElapsed(1))
        assertTrue(watch.inGrace)
        assertEquals(listOf<LinkEffect>(LinkEffect.Lost), watch.onGraceElapsed(2))
    }

    @Test
    fun downBeforeAnythingWasEstablishedIsTheAttemptsBusiness() {
        assertEquals(emptyList<LinkEffect>(), watch.onCore(CoreState.CORE_STATE_ERROR))
        assertFalse(watch.inGrace)
    }

    @Test
    fun downWhileOurOwnAttemptRunsIsIgnored() {
        watch.onCore(CoreState.CORE_STATE_CONNECTED)
        assertEquals(emptyList<LinkEffect>(), watch.onCore(CoreState.CORE_STATE_IDLE, attemptRunning = true))
        assertTrue(watch.established)
        assertFalse(watch.inGrace)
    }

    @Test
    fun repeatedDownEventsKeepOneTimer() {
        watch.onCore(CoreState.CORE_STATE_CONNECTED)
        watch.onCore(CoreState.CORE_STATE_IDLE)
        assertEquals(emptyList<LinkEffect>(), watch.onCore(CoreState.CORE_STATE_ERROR))
    }

    @Test
    fun resetEndsTheSessionAndCancelsTheTimer() {
        watch.onCore(CoreState.CORE_STATE_CONNECTED)
        watch.onCore(CoreState.CORE_STATE_IDLE)
        assertEquals(listOf<LinkEffect>(LinkEffect.CancelGrace), watch.reset())
        assertFalse(watch.established)
        assertFalse(watch.lost)
        assertEquals(emptyList<LinkEffect>(), watch.onCore(CoreState.CORE_STATE_ERROR))
    }
}
