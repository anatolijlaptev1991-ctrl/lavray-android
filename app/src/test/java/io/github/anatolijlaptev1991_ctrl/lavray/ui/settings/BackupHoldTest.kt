// SPDX-License-Identifier: GPL-3.0-only
package io.github.anatolijlaptev1991_ctrl.lavray.ui.settings

import io.github.anatolijlaptev1991_ctrl.lavray.R
import io.github.anatolijlaptev1991_ctrl.lavray.api.Connection
import io.github.anatolijlaptev1991_ctrl.lavray.api.CoreEndpoint
import io.github.anatolijlaptev1991_ctrl.lavray.api.LavrayClient
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
import kotlinx.coroutines.flow.first
import kotlinx.coroutines.runBlocking
import kotlinx.coroutines.withTimeout
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test
import java.io.ByteArrayInputStream
import java.io.ByteArrayOutputStream
import java.io.IOException
import java.io.InputStream
import java.io.OutputStream
import java.util.concurrent.CopyOnWriteArrayList
import java.util.concurrent.CountDownLatch

/** The service is held for the whole restore (it may outlive the screen and the process of the service must stay), and always released. */
class BackupHoldTest {
    private class Conn(override val input: InputStream, val sink: OutputStream = ByteArrayOutputStream()) : Connection {
        override val output: OutputStream = sink
        override fun abort() = Unit
        override fun close() = Unit
    }

    private val log = CopyOnWriteArrayList<String>()
    private val body = "{}".toByteArray()
    private val ok = ("HTTP/1.1 200 OK\r\nContent-Type: application/json\r\nContent-Length: ${body.size}\r\n\r\n").toByteArray() + body

    private class Files : BackupFiles {
        override fun write(target: FileTarget, data: ByteArray) = true
        override fun read(source: FileTarget, limit: Int): FileRead = FileRead.Ok(ByteArray(8), "copy.lavbk")
    }

    private fun session(connect: () -> Connection) = CoreSession(
        { CoreEndpoint("/x", "t") },
        { ep -> LavrayClient(ep.token, { log += "request"; connect() }, Dispatchers.IO) },
    )

    private fun hold(beginFails: Boolean = false, endFails: Boolean = false) = object : ServiceHold {
        override suspend fun begin(token: String, timeoutMs: Long) {
            log += "begin:$timeoutMs"
            if (beginFails) throw IOException("binder dead")
        }

        override suspend fun end(token: String) {
            log += "end"
            if (endFails) throw IOException("binder dead")
        }
    }

    private suspend fun BackupModel.idle() = withTimeout(5_000) { state.first { !it.busy } }

    private suspend fun BackupModel.picked(scope: CoroutineScope): BackupModel {
        pick(FileTarget(null))
        idle()
        return this
    }

    @Test fun theServiceIsHeldBeforeTheRequestAndReleasedAfterIt() = runBlocking {
        val scope = CoroutineScope(SupervisorJob() + Dispatchers.Default)
        val model = BackupModel(session { Conn(ByteArrayInputStream(ok)) }, Files(), scope, hold = hold()).picked(scope)
        log.clear()
        model.replace("Correct-Horse-1")!!.join()
        assertEquals(listOf("begin:180000", "request", "end"), log.toList())
        assertTrue(model.state.value.restored)
        scope.cancel()
    }

    @Test fun aRefusedRestoreReleasesTheHoldToo() = runBlocking {
        val refused = "{\"code\":\"failed_precondition\",\"message\":\"x\"}".toByteArray()
        val http = ("HTTP/1.1 400 Bad Request\r\nContent-Type: application/json\r\nContent-Length: ${refused.size}\r\n\r\n").toByteArray() + refused
        val scope = CoroutineScope(SupervisorJob() + Dispatchers.Default)
        val model = BackupModel(session { Conn(ByteArrayInputStream(http)) }, Files(), scope, hold = hold()).picked(scope)
        log.clear()
        model.replace("Correct-Horse-1")!!.join()
        assertEquals(listOf("begin:180000", "request", "end"), log.toList())
        assertFalse(model.state.value.restored)
        scope.cancel()
    }

    @Test fun aRestoreThatIsCancelledReleasesTheHold() = runBlocking {
        val gate = CountDownLatch(1)
        val scope = CoroutineScope(SupervisorJob() + Dispatchers.Default)
        val model = BackupModel(session { gate.await(); Conn(ByteArrayInputStream(ok)) }, Files(), scope, hold = hold()).picked(scope)
        log.clear()
        val job = model.replace("Correct-Horse-1")!!
        withTimeout(5_000) { while ("request" !in log) kotlinx.coroutines.delay(5) }
        job.cancel()
        gate.countDown()
        job.join()
        assertTrue("released although cancelled: $log", "end" in log)
        assertFalse(model.idle().busy)
        scope.cancel()
    }

    // R7 P5: Back from the root cancels the app scope while the core replaces the data; the outcome must still be known.
    @Test fun aRestoreWhoseScopeIsCancelledMidRequestStillFinishesAndHandsItsOutcomeOn() = runBlocking {
        val gate = CountDownLatch(1)
        val scope = CoroutineScope(SupervisorJob() + Dispatchers.Default)
        val restored = CountDownLatch(1)
        val model = BackupModel(
            session { gate.await(); Conn(ByteArrayInputStream(ok)) }, Files(), scope,
            onRestored = { restored.countDown() }, hold = hold(),
        ).picked(scope)
        log.clear()
        val job = model.replace("Correct-Horse-1")!!
        withTimeout(5_000) { while ("request" !in log) kotlinx.coroutines.delay(5) }
        scope.cancel()
        gate.countDown()
        job.join()
        assertTrue("the outcome is handed on although the scope is gone", restored.await(5, java.util.concurrent.TimeUnit.SECONDS))
        assertTrue(model.state.value.restored)
        assertEquals(listOf("begin:180000", "request", "end"), log.toList())
        assertFalse(model.state.value.busy)
    }

    @Test fun aRefusedRestoreWhoseScopeIsGoneHandsTheReasonOn() = runBlocking {
        val refused = "{\"code\":\"failed_precondition\",\"message\":\"x\"}".toByteArray()
        val http = ("HTTP/1.1 400 Bad Request\r\nContent-Type: application/json\r\nContent-Length: ${refused.size}\r\n\r\n").toByteArray() + refused
        val gate = CountDownLatch(1)
        val scope = CoroutineScope(SupervisorJob() + Dispatchers.Default)
        val reasons = CopyOnWriteArrayList<Int>()
        val model = BackupModel(
            session { gate.await(); Conn(ByteArrayInputStream(http)) }, Files(), scope,
            onRestoreFailed = { reasons += it }, hold = hold(),
        ).picked(scope)
        log.clear()
        val job = model.replace("Correct-Horse-1")!!
        withTimeout(5_000) { while ("request" !in log) kotlinx.coroutines.delay(5) }
        scope.cancel()
        gate.countDown()
        job.join()
        assertEquals(1, reasons.size)
        assertEquals(model.state.value.error, reasons.single())
        assertFalse(model.state.value.restored)
    }

    @Test fun theNewsOfARestoreIsToldOnce() {
        val news = io.github.anatolijlaptev1991_ctrl.lavray.ui.screens.backup.RestoreNews()
        assertEquals(null, news.take())
        news.leave(R.string.bk_done)
        assertEquals(R.string.bk_done, news.take())
        assertEquals(null, news.take())
    }

    @Test fun withoutAHoldNothingIsReplacedAndTheReasonIsShown() = runBlocking {
        val scope = CoroutineScope(SupervisorJob() + Dispatchers.Default)
        val model = BackupModel(session { Conn(ByteArrayInputStream(ok)) }, Files(), scope, hold = hold(beginFails = true)).picked(scope)
        log.clear()
        model.replace("Correct-Horse-1")!!.join()
        assertFalse("the core was never asked", "request" in log)
        assertEquals(R.string.bk_err_hold, model.state.value.error)
        assertFalse(model.state.value.restored)
        assertFalse(model.idle().busy)
        scope.cancel()
    }

    @Test fun aReleaseThatFailsDoesNotLoseTheResult() = runBlocking {
        val scope = CoroutineScope(SupervisorJob() + Dispatchers.Default)
        val model = BackupModel(session { Conn(ByteArrayInputStream(ok)) }, Files(), scope, hold = hold(endFails = true)).picked(scope)
        model.replace("Correct-Horse-1")!!.join()
        assertTrue(model.state.value.restored)
        scope.cancel()
    }
}
