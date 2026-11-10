// SPDX-License-Identifier: GPL-3.0-only
package io.github.anatolijlaptev1991_ctrl.lavray.ui.connection

import io.github.anatolijlaptev1991_ctrl.lavray.api.Connection
import io.github.anatolijlaptev1991_ctrl.lavray.api.CoreEndpoint
import io.github.anatolijlaptev1991_ctrl.lavray.api.LavrayClient
import io.github.anatolijlaptev1991_ctrl.lavray.ui.core.CoreSession
import io.github.anatolijlaptev1991_ctrl.lavray.ui.screens.connection.ConnectionModel
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.SupervisorJob
import kotlinx.coroutines.awaitCancellation
import kotlinx.coroutines.cancel
import kotlinx.coroutines.flow.first
import kotlinx.coroutines.runBlocking
import kotlinx.coroutines.withTimeoutOrNull
import org.junit.Assert.assertEquals
import org.junit.Assert.assertNotNull
import org.junit.Test
import java.io.ByteArrayInputStream
import java.io.ByteArrayOutputStream
import java.io.InputStream
import java.io.OutputStream
import java.util.concurrent.atomic.AtomicInteger

/** E-15: a refresh that is still running when a newer one starts must not outlive it. */
class RefreshReplacesTest {
    private class Conn(override val input: InputStream) : Connection {
        override val output: OutputStream = ByteArrayOutputStream()
        override fun abort() = Unit
        override fun close() = Unit
    }

    private val body = "{}".toByteArray()
    private val http = ("HTTP/1.1 200 OK\r\nContent-Type: application/json\r\nContent-Length: ${body.size}\r\n\r\n").toByteArray() + body

    @Test fun theNewerRefreshPassesThePreviousOneThatIsStuck() = runBlocking {
        val asked = AtomicInteger()
        // The first look for the core never answers (a stuck pass); every later one does.
        val session = CoreSession(
            { if (asked.incrementAndGet() == 1) awaitCancellation() else CoreEndpoint("/x", "t") },
            { ep -> LavrayClient(ep.token, { Conn(ByteArrayInputStream(http)) }, Dispatchers.IO) },
        )
        val scope = CoroutineScope(SupervisorJob() + Dispatchers.IO)
        val model = ConnectionModel(session, scope, kotlinx.coroutines.flow.MutableStateFlow(io.github.anatolijlaptev1991_ctrl.lavray.ipc.VpnStatus.IDLE))
        model.refresh(1L)
        withTimeoutOrNull(300) { model.info.first { it.loaded } }
        model.refresh(2L)
        val loaded = withTimeoutOrNull(5_000) { model.info.first { it.loaded } }
        assertNotNull("the second refresh was blocked behind the first", loaded)
        assertEquals(2, asked.get())
        scope.cancel()
    }
}
