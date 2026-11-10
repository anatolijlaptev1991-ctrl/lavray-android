// SPDX-License-Identifier: GPL-3.0-only
package io.github.anatolijlaptev1991_ctrl.lavray.ui.settings

import io.github.anatolijlaptev1991_ctrl.lavray.api.Connection
import io.github.anatolijlaptev1991_ctrl.lavray.api.CoreEndpoint
import io.github.anatolijlaptev1991_ctrl.lavray.api.LavrayClient
import io.github.anatolijlaptev1991_ctrl.lavray.ui.HoldRegistry
import io.github.anatolijlaptev1991_ctrl.lavray.ui.core.CoreSession
import io.github.anatolijlaptev1991_ctrl.lavray.ui.screens.backup.BackupFiles
import io.github.anatolijlaptev1991_ctrl.lavray.ui.screens.backup.BackupModel
import io.github.anatolijlaptev1991_ctrl.lavray.ui.screens.backup.FileRead
import io.github.anatolijlaptev1991_ctrl.lavray.ui.screens.backup.FileTarget
import io.github.anatolijlaptev1991_ctrl.lavray.ui.screens.backup.ServiceHold
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.SupervisorJob
import kotlinx.coroutines.cancel
import kotlinx.coroutines.delay
import kotlinx.coroutines.flow.first
import kotlinx.coroutines.launch
import kotlinx.coroutines.runBlocking
import kotlinx.coroutines.withTimeout
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test
import java.io.ByteArrayInputStream
import java.io.ByteArrayOutputStream
import java.io.InputStream
import java.io.OutputStream
import java.util.concurrent.CopyOnWriteArrayList
import java.util.concurrent.CountDownLatch

/**
 * M1: the UI is unbound (Home, Back) while a restore runs. The hold is ended on the Binder that accepted it, bounded, and
 * never waits for the next bind; the restore's end reaches the model's idle state.
 */
class BackupHoldUnboundTest {
    private class Conn(override val input: InputStream) : Connection {
        override val output: OutputStream = ByteArrayOutputStream()
        override fun abort() = Unit
        override fun close() = Unit
    }

    private class Files : BackupFiles {
        override fun write(target: FileTarget, data: ByteArray) = true
        override fun read(source: FileTarget, limit: Int): FileRead = FileRead.Ok(ByteArray(8), "copy.lavbk")
    }

    private val body = "{}".toByteArray()
    private val ok = ("HTTP/1.1 200 OK\r\nContent-Type: application/json\r\nContent-Length: ${body.size}\r\n\r\n").toByteArray() + body
    private val log = CopyOnWriteArrayList<String>()

    private fun session() = CoreSession(
        { CoreEndpoint("/x", "t") },
        { ep -> LavrayClient(ep.token, { Conn(ByteArrayInputStream(ok)) }, Dispatchers.IO) },
    )

    /** The adapter of AppEnv, over a registry and a fake Binder; `bound` models "the controller is bound now" and is never consulted. */
    private fun hold(registry: HoldRegistry<String>, binderEnd: (String) -> Unit) = object : ServiceHold {
        override suspend fun begin(token: String, timeoutMs: Long) {
            check(registry.begin(token, "binder-1") { log += "begin:$timeoutMs"; true }) { "refused" }
        }

        override suspend fun end(token: String) = registry.end(token) { binderEnd(token) }
    }

    @Test
    fun theRestoreEndsAndTheModelIsIdleEvenThoughNothingIsBoundAnymore() = runBlocking {
        val scope = CoroutineScope(SupervisorJob() + Dispatchers.Default)
        val registry = HoldRegistry<String>(endTimeoutMs = 2_000)
        val model = BackupModel(session(), Files(), scope, hold = hold(registry) { log += "end-on-binder-1" })
        model.pick(FileTarget(null))
        withTimeout(5_000) { model.state.first { !it.busy } }
        model.replace("Correct-Horse-1")!!.join()
        assertEquals(listOf("begin:180000", "end-on-binder-1"), log.toList())
        assertFalse(model.state.value.busy)
        assertTrue(model.state.value.restored)
        assertFalse(registry.isHeld("backup-restore-1"))
        scope.cancel()
    }

    // The Binder of a service that is gone or hung must not keep the screen busy: the end is abandoned at the bound.
    @Test
    fun aBinderThatNeverAnswersTheEndDoesNotKeepTheModelBusy() = runBlocking {
        val scope = CoroutineScope(SupervisorJob() + Dispatchers.Default)
        val registry = HoldRegistry<String>(endTimeoutMs = 300)
        val never = CountDownLatch(1)
        val model = BackupModel(session(), Files(), scope, hold = hold(registry) { never.await() })
        model.pick(FileTarget(null))
        withTimeout(5_000) { model.state.first { !it.busy } }
        val started = System.nanoTime()
        withTimeout(5_000) { model.replace("Correct-Horse-1")!!.join() }
        assertTrue("ended within the bound", (System.nanoTime() - started) / 1_000_000 < 3_000)
        assertFalse(withTimeout(5_000) { model.state.first { !it.busy } }.busy)
        never.countDown()
        scope.cancel()
    }

    @Test
    fun aHoldThatWasNeverAcceptedHasNothingToEnd() = runBlocking {
        val registry = HoldRegistry<String>(endTimeoutMs = 200)
        var called = false
        registry.end("unknown") { called = true }
        assertFalse(called)
    }

    // A begin that completed and was then cancelled must still be ended by token (the model's finally does it).
    @Test
    fun aCancelledBeginIsRecordedSoItCanBeEnded() = runBlocking {
        val registry = HoldRegistry<String>(endTimeoutMs = 2_000)
        val gate = CountDownLatch(1)
        val scope = CoroutineScope(SupervisorJob() + Dispatchers.Default)
        val job = scope.launch { registry.begin("t", "binder") { gate.await(); true } }
        delay(100)
        job.cancel()
        gate.countDown()
        job.join()
        assertTrue("accepted although the caller was cancelled", registry.isHeld("t"))
        var ended = false
        registry.end("t") { ended = true }
        assertTrue(ended)
        scope.cancel()
    }

}
