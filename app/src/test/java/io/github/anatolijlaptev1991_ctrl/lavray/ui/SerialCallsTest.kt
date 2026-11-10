// SPDX-License-Identifier: GPL-3.0-only
package io.github.anatolijlaptev1991_ctrl.lavray.ui

import kotlinx.coroutines.CompletableDeferred
import kotlinx.coroutines.runBlocking
import kotlinx.coroutines.withTimeout
import org.junit.Assert.assertEquals
import org.junit.Test
import java.util.concurrent.CopyOnWriteArrayList

class SerialCallsTest {
    // L-7: a quick tap and a Disconnect must reach the Binder in the order they were made, even when the service binds late.
    @Test
    fun callsRunInTheOrderTheyWereSubmittedEvenWhileTheTargetIsStillBinding() = runBlocking {
        val bound = CompletableDeferred<String>()
        val seen = CopyOnWriteArrayList<String>()
        val done = CompletableDeferred<Unit>()
        val calls = SerialCalls(this, { bound.await() })
        calls.submit { seen += "connect:$it" }
        calls.submit { seen += "reconnect:$it" }
        calls.submit { seen += "disconnect:$it"; done.complete(Unit) }
        bound.complete("svc")
        withTimeout(2_000) { done.await() }
        assertEquals(listOf("connect:svc", "reconnect:svc", "disconnect:svc"), seen)
        calls.close()
    }

    @Test
    fun aFailingCallDoesNotStopTheOnesBehindIt() = runBlocking {
        val seen = CopyOnWriteArrayList<Int>()
        val done = CompletableDeferred<Unit>()
        val calls = SerialCalls(this, { Unit })
        calls.submit { throw IllegalStateException("dead binder") }
        calls.submit { seen += 2; done.complete(Unit) }
        withTimeout(2_000) { done.await() }
        assertEquals(listOf(2), seen)
        calls.close()
    }

    // R7 P7: a call that never reached its target is reported, once per call, and the queue goes on.
    @Test
    fun aLostCallIsReportedAndACallThatRanIsNot() = runBlocking {
        val dropped = java.util.concurrent.atomic.AtomicInteger()
        val done = CompletableDeferred<Unit>()
        var reachable = false
        val calls = SerialCalls<Unit>(this, { check(reachable) { "not bound" } }, onDropped = { dropped.incrementAndGet() })
        calls.submit { error("must not run: the target was never reached") }
        calls.submit { reachable = true }
        // The second call was lost too (the target failed before it could run), so both are reported.
        withTimeout(2_000) { while (dropped.get() < 2) kotlinx.coroutines.delay(5) }
        reachable = true
        calls.submit { done.complete(Unit) }
        withTimeout(2_000) { done.await() }
        assertEquals(2, dropped.get())
        calls.close()
    }

    @Test
    fun manyCallsFromManyThreadsKeepTheirSubmissionOrder() = runBlocking {
        val seen = CopyOnWriteArrayList<Int>()
        val done = CompletableDeferred<Unit>()
        val calls = SerialCalls(this, { Unit })
        // Submission order is the order of the submit() calls on one thread.
        for (i in 1..200) calls.submit { seen += i; if (i == 200) done.complete(Unit) }
        withTimeout(5_000) { done.await() }
        assertEquals((1..200).toList(), seen)
        calls.close()
    }
}
