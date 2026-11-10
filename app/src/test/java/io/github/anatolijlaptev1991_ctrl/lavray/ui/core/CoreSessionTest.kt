// SPDX-License-Identifier: GPL-3.0-only
package io.github.anatolijlaptev1991_ctrl.lavray.ui.core

import io.github.anatolijlaptev1991_ctrl.lavray.api.Connection
import io.github.anatolijlaptev1991_ctrl.lavray.api.CoreEndpoint
import io.github.anatolijlaptev1991_ctrl.lavray.api.LavrayClient
import io.github.anatolijlaptev1991_ctrl.lavray.api.gen.coreConnect
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.runBlocking
import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Test
import java.io.ByteArrayInputStream
import java.io.ByteArrayOutputStream
import java.io.IOException
import java.io.InputStream
import java.io.OutputStream
import java.util.concurrent.atomic.AtomicInteger

/**
 * The :vpn process ends with the tunnel, so the endpoint the UI cached goes stale. The first call after that used to
 * fail with "core unavailable" (seen on the emulator when importing right after Disconnect).
 */
class CoreSessionTest {
    private class Conn(override val input: InputStream) : Connection {
        override val output: OutputStream = ByteArrayOutputStream()
        override fun abort() = Unit
        override fun close() = Unit
    }

    private val okBody = "{}".toByteArray()
    private val okHttp = ("HTTP/1.1 200 OK\r\nContent-Type: application/json\r\nContent-Length: ${okBody.size}\r\n\r\n").toByteArray() + okBody

    private fun endpoint(n: Int) = CoreEndpoint("/stale/$n", "t$n")

    @Test
    fun aStaleEndpointIsRefreshedAndTheCallRepeatedOnce() = runBlocking {
        val asked = AtomicInteger()
        val session = CoreSession(
            { endpoint(asked.incrementAndGet()) },
            { ep ->
                // First endpoint: the socket is gone. Second: a live core.
                LavrayClient(ep.token, { if (ep.token == "t1") throw IOException("ECONNREFUSED") else Conn(ByteArrayInputStream(okHttp)) }, Dispatchers.IO)
            },
        )
        val result = session.call { coreConnect() }
        assertTrue("expected Ok but got $result", result is CoreResult.Ok)
        assertEquals(2, asked.get())
    }

    @Test
    fun aCoreThatStaysDownFailsAfterOneRetryNotForever() = runBlocking {
        val asked = AtomicInteger()
        val session = CoreSession(
            { endpoint(asked.incrementAndGet()) },
            { ep -> LavrayClient(ep.token, { throw IOException("ECONNREFUSED") }, Dispatchers.IO) },
        )
        val result = session.call { coreConnect() }
        assertEquals(CoreResult.Fail(ErrorKind.CoreUnavailable, null), result)
        assertEquals(2, asked.get())
    }

    @Test
    fun aRefusedRequestIsNotRepeated() = runBlocking {
        val asked = AtomicInteger()
        val body = "{\"code\":\"failed_precondition\",\"message\":\"x\"}".toByteArray()
        val http = ("HTTP/1.1 400 Bad Request\r\nContent-Type: application/json\r\nContent-Length: ${body.size}\r\n\r\n").toByteArray() + body
        val session = CoreSession(
            { endpoint(asked.incrementAndGet()) },
            { ep -> LavrayClient(ep.token, { Conn(ByteArrayInputStream(http)) }, Dispatchers.IO) },
        )
        val result = session.call { coreConnect() }
        assertEquals(CoreResult.Fail(ErrorKind.Precondition, null), result)
        assertEquals(1, asked.get())
    }
}

/** The service restarts the Go core from below and says so in VpnStatus.coreEpoch; the session must not keep the old endpoint. */
class CoreEpochTest {
    private class Conn(override val input: InputStream) : Connection {
        override val output: OutputStream = ByteArrayOutputStream()
        override fun abort() = Unit
        override fun close() = Unit
    }

    private val okBody = "{}".toByteArray()
    private val okHttp = ("HTTP/1.1 200 OK\r\nContent-Type: application/json\r\nContent-Length: ${okBody.size}\r\n\r\n").toByteArray() + okBody

    private fun session(asked: AtomicInteger, built: MutableList<String>) = CoreSession(
        { CoreEndpoint("/s", "t${asked.incrementAndGet()}") },
        { ep ->
            built += ep.token
            LavrayClient(ep.token, { Conn(ByteArrayInputStream(okHttp)) }, Dispatchers.IO)
        },
    )

    @Test
    fun aNewEpochMakesTheNextCallAskTheServiceForTheEndpointAgain() = runBlocking {
        val asked = AtomicInteger()
        val built = mutableListOf<String>()
        val session = session(asked, built)
        session.call { coreConnect() }
        session.call { coreConnect() }
        assertEquals("one client serves calls while nothing changed", listOf("t1"), built)

        session.onCoreEpoch(1)
        session.call { coreConnect() }
        assertEquals(listOf("t1", "t2"), built)
        assertEquals(2, asked.get())
    }

    @Test
    fun theSameEpochAgainKeepsTheClient() = runBlocking {
        val asked = AtomicInteger()
        val built = mutableListOf<String>()
        val session = session(asked, built)
        session.onCoreEpoch(0)
        session.call { coreConnect() }
        session.onCoreEpoch(0)
        session.call { coreConnect() }
        assertEquals(listOf("t1"), built)
    }

    @Test
    fun anEndpointFetchedBeforeTheEpochMovedIsNotKept() = runBlocking {
        val asked = AtomicInteger()
        val built = mutableListOf<String>()
        lateinit var session: CoreSession
        session = CoreSession(
            {
                val n = asked.incrementAndGet()
                // The service restarts the core while the endpoint is being fetched.
                if (n == 1) session.onCoreEpoch(1)
                CoreEndpoint("/s", "t$n")
            },
            { ep -> built += ep.token; LavrayClient(ep.token, { Conn(ByteArrayInputStream(okHttp)) }, Dispatchers.IO) },
        )
        session.call { coreConnect() }
        session.call { coreConnect() }
        assertEquals("the first client is not cached, the second is", listOf("t1", "t2"), built)
    }
}
