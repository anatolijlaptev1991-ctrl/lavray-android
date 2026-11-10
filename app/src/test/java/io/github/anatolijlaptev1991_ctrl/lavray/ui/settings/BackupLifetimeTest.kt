// SPDX-License-Identifier: GPL-3.0-only
package io.github.anatolijlaptev1991_ctrl.lavray.ui.settings

import io.github.anatolijlaptev1991_ctrl.lavray.ui.screens.backup.FileTarget
import io.github.anatolijlaptev1991_ctrl.lavray.api.Connection
import io.github.anatolijlaptev1991_ctrl.lavray.api.CoreEndpoint
import io.github.anatolijlaptev1991_ctrl.lavray.api.LavrayClient
import io.github.anatolijlaptev1991_ctrl.lavray.ui.core.CoreSession
import io.github.anatolijlaptev1991_ctrl.lavray.ui.screens.backup.BackupFiles
import io.github.anatolijlaptev1991_ctrl.lavray.ui.screens.backup.BackupModel
import io.github.anatolijlaptev1991_ctrl.lavray.ui.screens.backup.BackupState
import io.github.anatolijlaptev1991_ctrl.lavray.ui.screens.backup.FileRead
import io.github.anatolijlaptev1991_ctrl.lavray.ui.screens.connection.Phase
import io.github.anatolijlaptev1991_ctrl.lavray.ui.screens.connection.hasTunnel
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.SupervisorJob
import kotlinx.coroutines.cancel
import kotlinx.coroutines.flow.first
import kotlinx.coroutines.runBlocking
import kotlinx.coroutines.withTimeout
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNotNull
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test
import java.io.ByteArrayInputStream
import java.io.ByteArrayOutputStream
import java.io.InputStream
import java.util.concurrent.CountDownLatch
import java.util.concurrent.atomic.AtomicInteger

/**
 * E-07, E-17, E-24 and the review items H1/H2: the backup model belongs to the app, so whatever the screen does, an
 * operation ends with the model idle and usable, and the restore gets the password that was typed.
 */
class BackupLifetimeTest {
    private class Conn(override val input: InputStream, override val output: ByteArrayOutputStream) : Connection {
        override fun abort() = Unit
        override fun close() = Unit
    }

    private val body = "{}".toByteArray()
    private val ok = ("HTTP/1.1 200 OK\r\nContent-Type: application/json\r\nContent-Length: ${body.size}\r\n\r\n").toByteArray() + body

    private class Files(val gate: CountDownLatch? = null) : BackupFiles {
        override fun write(target: FileTarget, data: ByteArray): Boolean = true
        override fun read(source: FileTarget, limit: Int): FileRead {
            gate?.await()
            return FileRead.Ok(ByteArray(8), "copy.lavbk")
        }
    }

    private val requests = java.util.concurrent.CopyOnWriteArrayList<ByteArrayOutputStream>()

    private fun session(gate: CountDownLatch? = null) = CoreSession(
        { CoreEndpoint("/x", "t") },
        { ep ->
            LavrayClient(ep.token, {
                gate?.await()
                Conn(ByteArrayInputStream(ok), ByteArrayOutputStream().also { requests += it })
            }, Dispatchers.IO)
        },
    )

    private fun model(scope: CoroutineScope, files: BackupFiles = Files(), session: CoreSession = session()) =
        BackupModel(session, files, scope)

    private suspend fun BackupModel.idle() = withTimeout(5_000) { state.first { !it.busy } }

    @Test fun leavingTheScreenMidOperationLeavesTheModelUsableAgain() = runBlocking {
        val gate = CountDownLatch(1)
        val scope = CoroutineScope(SupervisorJob() + Dispatchers.Default)
        val model = model(scope, Files(gate))
        assertNotNull(model.pick(FileTarget(null)))
        assertTrue(model.state.value.busy)
        model.release() // the screen is left while the file is still being read
        gate.countDown()
        val after = model.idle()
        assertFalse("every button of the screen is enabled again", after.busy)
        assertNull("what the screen showed is gone with it", after.pickedName)
        // A second visit can start a new operation at once.
        model.attach()
        assertNotNull(model.pick(FileTarget(null)))
        assertEquals("copy.lavbk", model.idle().pickedName)
        scope.cancel()
    }

    @Test fun anOperationThatIsCancelledStillClearsBusy() = runBlocking {
        val gate = CountDownLatch(1)
        val scope = CoroutineScope(SupervisorJob() + Dispatchers.Default)
        val model = model(scope, Files(gate))
        val job = model.pick(FileTarget(null))!!
        assertTrue(model.state.value.busy)
        job.cancel()
        gate.countDown()
        assertFalse(model.idle().busy)
        scope.cancel()
    }

    @Test fun aCheckThatIsCancelledAlsoClearsBusy() = runBlocking {
        val scope = CoroutineScope(SupervisorJob() + Dispatchers.Default)
        val gate = CountDownLatch(1)
        val slow = BackupModel(session(gate), Files(), scope)
        slow.pick(FileTarget(null))
        slow.idle()
        val job = slow.check("password")!!
        job.cancel()
        gate.countDown()
        assertFalse(slow.idle().busy)
        scope.cancel()
    }

    @Test fun aSecondOperationWhileOneRunsIsRefused() = runBlocking {
        val gate = CountDownLatch(1)
        val scope = CoroutineScope(SupervisorJob() + Dispatchers.Default)
        val model = model(scope, Files(gate))
        assertNotNull(model.pick(FileTarget(null)))
        assertNull(model.pick(FileTarget(null)))
        assertNull(model.createBackup("password-123"))
        gate.countDown()
        model.idle()
        scope.cancel()
    }

    @Test fun theRestoreSendsThePasswordThatWasGivenAndKeepsItsResultAfterLeaving() = runBlocking {
        val scope = CoroutineScope(SupervisorJob() + Dispatchers.Default)
        val restored = AtomicInteger()
        val before = AtomicInteger()
        val model = BackupModel(session(), Files(), scope, onBeforeReplace = { before.incrementAndGet() }, onRestored = { restored.incrementAndGet() })
        model.pick(FileTarget(null))
        model.idle()
        val password = "Correct-Horse-1"
        model.replace(password)!!.join()
        assertTrue(model.state.value.restored)
        assertTrue("the typed password reached the core", requests.any { it.toString(Charsets.UTF_8).contains(password) })
        assertEquals(1, before.get())
        assertEquals(1, restored.get())
        model.release()
        assertTrue("the finished restore survives leaving the screen", model.state.value.restored)
        scope.cancel()
    }

    @Test fun aReplaceWithoutACheckedFileDoesNothing() {
        val scope = CoroutineScope(SupervisorJob() + Dispatchers.Default)
        assertNull(model(scope).replace("x"))
        scope.cancel()
    }

    @Test fun afterReleaseOfAFinishedStateTheFileState_isForgottenButARestoreIsKept() {
        assertEquals(BackupState(restored = true, needsRestart = true), BackupState(restored = true, needsRestart = true).withoutFileState())
        val after = BackupState(pickedName = "copy.lavbk").withoutFileState()
        assertNull(after.pickedName)
        assertNull(after.summary)
        assertFalse(after.saved)
    }

    @Test fun theCoreRefusesARestoreWhileAnyConnectionExists() {
        assertTrue(Phase.On.hasTunnel)
        assertTrue(Phase.Connecting.hasTunnel)
        assertTrue(Phase.WaitingNetwork.hasTunnel)
        assertTrue(Phase.Disconnecting.hasTunnel)
        assertFalse(Phase.Off.hasTunnel)
        assertFalse(Phase.Error.hasTunnel)
        assertFalse(Phase.NeedsPermission.hasTunnel)
    }
}
