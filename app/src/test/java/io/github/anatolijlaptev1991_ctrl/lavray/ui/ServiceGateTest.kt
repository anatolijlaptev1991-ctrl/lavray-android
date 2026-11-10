// SPDX-License-Identifier: GPL-3.0-only
package io.github.anatolijlaptev1991_ctrl.lavray.ui

import kotlinx.coroutines.CompletableDeferred
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.async
import kotlinx.coroutines.delay
import kotlinx.coroutines.runBlocking
import kotlinx.coroutines.withTimeout
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test
import java.util.concurrent.CopyOnWriteArrayList

class ServiceGateTest {
    @Test
    fun aWaiterIsFailedWhenTheBindingIsReplaced() = runBlocking {
        val gate = ServiceGate<String>()
        val waiter = async(Dispatchers.Default) { runCatching { gate.await() } }
        delay(50)
        gate.replace()
        val result = withTimeout(2_000) { waiter.await() }
        assertTrue(result.exceptionOrNull() is IllegalStateException)
    }

    @Test
    fun aConnectionCompletesTheDeferredItCapturedNotTheCurrentOne() = runBlocking {
        val gate = ServiceGate<String>()
        val old = gate.capture()
        gate.replace()
        old.complete("stale")
        val waiter = async(Dispatchers.Default) { gate.await() }
        delay(50)
        assertFalse("the new binding has not connected", waiter.isCompleted)
        gate.capture().complete("fresh")
        assertEquals("fresh", withTimeout(2_000) { waiter.await() })
    }

    // N1: a command queued while unbound was waiting for binding #1; the UI unbinds and rebinds. The worker must not stay
    // on #1 for ever, and the stale command must not be replayed on binding #2.
    @Test
    fun serialCallsRecoverFromATargetThatIsSweptAwayWhileAwaiting() = runBlocking {
        val gate = ServiceGate<String>()
        val seen = CopyOnWriteArrayList<String>()
        val done = CompletableDeferred<Unit>()
        val calls = SerialCalls(this, { gate.await() })
        calls.submit { seen += "stale:$it" } // queued for binding #1
        delay(50)
        gate.replace() // unbind
        delay(50)
        gate.capture().complete("svc2") // rebind connects
        calls.submit { seen += "fresh:$it"; done.complete(Unit) }
        withTimeout(3_000) { done.await() }
        assertEquals(listOf("fresh:svc2"), seen.toList())
        calls.close()
    }
}
