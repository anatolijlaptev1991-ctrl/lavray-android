// SPDX-License-Identifier: GPL-3.0-only
package io.github.anatolijlaptev1991_ctrl.lavray.api

import io.github.anatolijlaptev1991_ctrl.lavray.api.gen.CoreService
import io.github.anatolijlaptev1991_ctrl.lavray.api.gen.CoreState
import io.github.anatolijlaptev1991_ctrl.lavray.api.gen.CoreStatus
import io.github.anatolijlaptev1991_ctrl.lavray.api.gen.ConnectMode
import io.github.anatolijlaptev1991_ctrl.lavray.api.gen.ConnectRequest
import io.github.anatolijlaptev1991_ctrl.lavray.api.gen.ConnectResponse
import io.github.anatolijlaptev1991_ctrl.lavray.api.gen.CoreEvent
import io.github.anatolijlaptev1991_ctrl.lavray.api.gen.SubscribeEventsRequest
import io.github.anatolijlaptev1991_ctrl.lavray.api.gen.coreConnect
import io.github.anatolijlaptev1991_ctrl.lavray.api.gen.coreSubscribeEvents
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.flow.first
import kotlinx.coroutines.flow.toList
import kotlinx.coroutines.runBlocking
import kotlinx.coroutines.withTimeout
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNotNull
import org.junit.Assert.assertThrows
import org.junit.Assert.assertTrue
import org.junit.Test
import java.io.ByteArrayInputStream
import java.io.ByteArrayOutputStream
import java.io.IOException
import java.io.InputStream
import java.io.OutputStream
import java.util.concurrent.CountDownLatch
import java.util.concurrent.LinkedBlockingQueue
import java.util.concurrent.TimeUnit
import java.util.concurrent.atomic.AtomicBoolean

/** In-memory connection: a canned response and a capture of what the client wrote. */
private class FakeConnection(
    override val input: InputStream,
    val written: ByteArrayOutputStream = ByteArrayOutputStream(),
) : Connection {
    val aborted = AtomicBoolean(false)
    val closed = AtomicBoolean(false)
    override val output: OutputStream = written
    override fun abort() { aborted.set(true); closed.set(true) }
    override fun close() { closed.set(true) }
}

/** Blocks readers until data arrives or the stream is aborted, like a live socket. */
private class LiveInput : InputStream() {
    private val queue = LinkedBlockingQueue<ByteArray>()
    private var current = ByteArray(0)
    private var pos = 0
    @Volatile var aborted = false
    private val poison = ByteArray(0)
    val readerBlocked = CountDownLatch(1)

    fun push(bytes: ByteArray) { queue.put(bytes) }
    fun abort() { aborted = true; queue.put(poison) }

    override fun read(): Int {
        val one = ByteArray(1)
        return if (read(one, 0, 1) < 0) -1 else one[0].toInt() and 0xFF
    }

    override fun read(b: ByteArray, off: Int, len: Int): Int {
        while (pos >= current.size) {
            readerBlocked.countDown()
            current = queue.take()
            pos = 0
            if (aborted) throw IOException("socket aborted")
        }
        val n = minOf(len, current.size - pos)
        System.arraycopy(current, pos, b, off, n)
        pos += n
        return n
    }
}

class LavrayClientTest {
    private val token = "tok-for-tests"

    private fun client(conn: Connection) = LavrayClient(token, { conn }, Dispatchers.IO)

    private fun http(status: String, contentType: String, body: ByteArray, chunkedBody: Boolean = false): ByteArray {
        val head = StringBuilder("HTTP/1.1 $status\r\nContent-Type: $contentType\r\n")
        head.append(if (chunkedBody) "Transfer-Encoding: chunked\r\n\r\n" else "Content-Length: ${body.size}\r\n\r\n")
        return head.toString().toByteArray() + if (chunkedBody) body else body
    }

    @Test
    fun unaryRoundTripSendsAuthAndJsonAndDecodesInt64Strings() = runBlocking {
        val reply = "{\"status\":{\"state\":\"CORE_STATE_CONNECTED\",\"activeProfileId\":\"9007199254740993\"},\"warnings\":[\"w\"]}"
        val conn = FakeConnection(ByteArrayInputStream(http("200 OK", "application/json", reply.toByteArray())))
        val response = client(conn).coreConnect(ConnectRequest(profileId = 12, mode = ConnectMode.CONNECT_MODE_TUN))

        assertEquals(CoreState.CORE_STATE_CONNECTED, response.status!!.state)
        assertEquals(9007199254740993L, response.status!!.activeProfileId)
        assertEquals(listOf("w"), response.warnings)

        val sent = conn.written.toString(Charsets.UTF_8)
        assertTrue(sent.startsWith("POST /lavray.v1.CoreService/Connect HTTP/1.1\r\n"))
        assertTrue(sent.contains("Authorization: Bearer $token\r\n"))
        assertTrue(sent.contains("Content-Type: application/json\r\n"))
        assertTrue(sent.contains("Connect-Protocol-Version: 1\r\n"))
        assertTrue(sent.endsWith("{\"profileId\":\"12\",\"mode\":\"CONNECT_MODE_TUN\"}"))
        assertTrue(conn.closed.get())
    }

    @Test
    fun unaryAcceptsChunkedResponse() = runBlocking {
        val json = "{\"state\":\"CORE_STATE_IDLE\"}".toByteArray()
        val raw = http("200 OK", "application/json", chunked(json.copyOf(5), json.copyOfRange(5, json.size)), chunkedBody = true)
        val status = client(FakeConnection(TrickleInputStream(raw, 2))).unary(
            CoreService.GET_STATUS, io.github.anatolijlaptev1991_ctrl.lavray.api.gen.GetStatusRequest,
            io.github.anatolijlaptev1991_ctrl.lavray.api.gen.GetStatusRequest.serializer(), CoreStatus.serializer(),
        )
        assertEquals(CoreState.CORE_STATE_IDLE, status.state)
    }

    @Test
    fun unaryMapsErrorBodyToApiException() {
        val body = "{\"code\":\"failed_precondition\",\"message\":\"already running\"}".toByteArray()
        val conn = FakeConnection(ByteArrayInputStream(http("400 Bad Request", "application/json", body)))
        val ex = assertThrows(ApiException::class.java) { runBlocking { client(conn).coreConnect() } }
        assertEquals(ConnectCode.FAILED_PRECONDITION, ex.code)
        assertEquals("already running", ex.message)
        assertEquals(400, ex.httpStatus)
    }

    @Test
    fun unaryWithRejectedTokenIsUnauthenticated() {
        val conn = FakeConnection(ByteArrayInputStream(http("401 Unauthorized", "text/plain", "no".toByteArray())))
        val ex = assertThrows(ApiException::class.java) { runBlocking { client(conn).coreConnect() } }
        assertEquals(ConnectCode.UNAUTHENTICATED, ex.code)
    }

    @Test
    fun unreachableSocketIsUnavailable() {
        val dead = LavrayClient(token, { throw IOException("connect refused") }, Dispatchers.IO)
        val ex = assertThrows(ApiException::class.java) { runBlocking { dead.coreConnect() } }
        assertEquals(ConnectCode.UNAVAILABLE, ex.code)
    }

    @Test
    fun serverStreamDeliversEventsThenCleanEnd() = runBlocking {
        val frames = ConnectFraming.encode(0, "{\"tsMs\":\"1\",\"stateChanged\":{\"state\":\"CORE_STATE_CONNECTING\"}}".toByteArray()) +
            ConnectFraming.encode(0, "{\"tsMs\":\"2\",\"shutdownImminent\":\"exit\"}".toByteArray()) +
            ConnectFraming.encode(Frame.FLAG_END_STREAM, "{}".toByteArray())
        // Worst case for the decoder: tiny chunks that cut headers and payloads.
        val body = chunked(*frames.toList().chunked(7).map { it.toByteArray() }.toTypedArray())
        val conn = FakeConnection(TrickleInputStream(http("200 OK", "application/connect+json", body, chunkedBody = true), 5))

        val events: List<CoreEvent> = client(conn).coreSubscribeEvents(SubscribeEventsRequest()).toList()

        assertEquals(2, events.size)
        assertEquals(CoreState.CORE_STATE_CONNECTING, events[0].stateChanged!!.state)
        assertEquals("exit", events[1].shutdownImminent)
        val sent = conn.written.toString(Charsets.ISO_8859_1)
        assertTrue(sent.contains("Content-Type: application/connect+json\r\n"))
        assertTrue(sent.contains("Authorization: Bearer $token\r\n"))
    }

    @Test
    fun serverStreamErrorInEndFrameThrowsAfterEvents() {
        val frames = ConnectFraming.encode(0, "{\"tsMs\":\"1\"}".toByteArray()) +
            ConnectFraming.encode(Frame.FLAG_END_STREAM, "{\"error\":{\"code\":\"internal\",\"message\":\"boom\"}}".toByteArray())
        val conn = FakeConnection(ByteArrayInputStream(http("200 OK", "application/connect+json", chunked(frames), chunkedBody = true)))
        val seen = ArrayList<CoreEvent>()
        val ex = assertThrows(ApiException::class.java) {
            runBlocking { client(conn).coreSubscribeEvents().collect { seen.add(it) } }
        }
        assertEquals(1, seen.size)
        assertEquals(ConnectCode.INTERNAL, ex.code)
        assertEquals("boom", ex.message)
    }

    @Test
    fun serverStreamWithoutEndFrameIsUnavailable() {
        val frames = ConnectFraming.encode(0, "{}".toByteArray())
        val conn = FakeConnection(ByteArrayInputStream(http("200 OK", "application/connect+json", chunked(frames), chunkedBody = true)))
        val ex = assertThrows(ApiException::class.java) { runBlocking { client(conn).coreSubscribeEvents().toList() } }
        assertEquals(ConnectCode.UNAVAILABLE, ex.code)
    }

    @Test
    fun serverStreamHttpErrorUsesErrorBody() {
        val body = "{\"code\":\"unauthenticated\",\"message\":\"bad token\"}".toByteArray()
        val conn = FakeConnection(ByteArrayInputStream(http("401 Unauthorized", "application/json", body)))
        val ex = assertThrows(ApiException::class.java) { runBlocking { client(conn).coreSubscribeEvents().toList() } }
        assertEquals(ConnectCode.UNAUTHENTICATED, ex.code)
    }

    @Test
    fun cancellingTheCollectorAbortsTheSocket() = runBlocking {
        val live = LiveInput()
        val conn = object : Connection {
            val aborted = AtomicBoolean(false)
            override val input: InputStream = live
            override val output: OutputStream = ByteArrayOutputStream()
            override fun abort() { aborted.set(true); live.abort() }
            override fun close() {}
        }
        val head = "HTTP/1.1 200 OK\r\nContent-Type: application/connect+json\r\nTransfer-Encoding: chunked\r\n\r\n".toByteArray()
        val frame = ConnectFraming.encode(0, "{\"tsMs\":\"7\"}".toByteArray())
        live.push(head + chunked(frame).copyOf(("%x\r\n".format(frame.size)).length + frame.size + 2))

        val first = withTimeout(5_000) { client(conn).coreSubscribeEvents().first() }

        assertEquals(7L, first.tsMs)
        // first() cancelled the collector while the reader waits for more data.
        withTimeout(5_000) {
            while (!conn.aborted.get()) kotlinx.coroutines.delay(10)
        }
        assertTrue(conn.aborted.get())
    }

    @Test
    fun endpointAndClientNeverPrintTheToken() {
        val ep = CoreEndpoint("/tmp/x.sock", "super-secret-token")
        assertFalse(ep.toString().contains("super-secret-token"))
        assertFalse(LavrayClient(ep).toString().contains("super-secret-token"))
        assertNotNull(ep.socketPath)
    }
}
