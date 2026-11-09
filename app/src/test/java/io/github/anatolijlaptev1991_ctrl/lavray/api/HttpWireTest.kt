// SPDX-License-Identifier: GPL-3.0-only
package io.github.anatolijlaptev1991_ctrl.lavray.api

import org.junit.Assert.assertArrayEquals
import org.junit.Assert.assertEquals
import org.junit.Assert.assertNull
import org.junit.Assert.assertThrows
import org.junit.Test
import java.io.ByteArrayInputStream
import java.io.EOFException
import java.io.IOException
import java.io.InputStream

/** Hands out at most [step] bytes per read, like a socket that delivers in small pieces. */
internal class TrickleInputStream(private val data: ByteArray, private val step: Int) : InputStream() {
    private var pos = 0
    override fun read(): Int = if (pos >= data.size) -1 else data[pos++].toInt() and 0xFF
    override fun read(b: ByteArray, off: Int, len: Int): Int {
        if (pos >= data.size) return -1
        val n = minOf(len, step, data.size - pos)
        System.arraycopy(data, pos, b, off, n)
        pos += n
        return n
    }
}

internal fun chunked(vararg parts: ByteArray): ByteArray {
    val out = java.io.ByteArrayOutputStream()
    for (p in parts) {
        out.write(Integer.toHexString(p.size).toByteArray())
        out.write("\r\n".toByteArray())
        out.write(p)
        out.write("\r\n".toByteArray())
    }
    out.write("0\r\n\r\n".toByteArray())
    return out.toByteArray()
}

class HttpWireTest {
    @Test
    fun parsesStatusAndHeaders() {
        val raw = "HTTP/1.1 401 Unauthorized\r\nContent-Type: application/json\r\nContent-Length: 2\r\n\r\n{}"
        val input = ByteArrayInputStream(raw.toByteArray())
        val head = HttpWire.readHead(input)
        assertEquals(401, head.status)
        assertEquals("application/json", head.header("CONTENT-TYPE"))
        assertEquals("2", head.header("content-length"))
        assertEquals("{}", String(HttpWire.readAll(HttpWire.bodyStream(head, input), 100)))
    }

    @Test
    fun rejectsGarbageStatusLine() {
        assertThrows(IOException::class.java) { HttpWire.readHead(ByteArrayInputStream("NOPE\r\n\r\n".toByteArray())) }
    }

    @Test
    fun closedBeforeResponseIsEof() {
        assertThrows(EOFException::class.java) { HttpWire.readHead(ByteArrayInputStream(ByteArray(0))) }
    }

    @Test
    fun chunkedBodyDecodesAtEveryReadSize() {
        val body = chunked("hello ".toByteArray(), "chunked ".toByteArray(), "world".toByteArray())
        for (step in 1..body.size) {
            val decoded = HttpWire.readAll(ChunkedInputStream(TrickleInputStream(body, step)), 1000)
            assertEquals("step $step", "hello chunked world", String(decoded))
        }
    }

    @Test
    fun chunkExtensionsAndTrailersAreIgnored() {
        val raw = "5;ext=1\r\nhello\r\n0\r\nX-Trailer: 1\r\n\r\n"
        assertEquals("hello", String(HttpWire.readAll(ChunkedInputStream(ByteArrayInputStream(raw.toByteArray())), 100)))
    }

    @Test
    fun truncatedChunkIsAnError() {
        val raw = "a\r\nshort"
        assertThrows(EOFException::class.java) {
            HttpWire.readAll(ChunkedInputStream(ByteArrayInputStream(raw.toByteArray())), 100)
        }
    }

    @Test
    fun badChunkSizeIsAnError() {
        assertThrows(IOException::class.java) {
            HttpWire.readAll(ChunkedInputStream(ByteArrayInputStream("zz\r\n".toByteArray())), 100)
        }
    }

    @Test
    fun contentLengthBodyStopsAtLength() {
        val raw = "HTTP/1.1 200 OK\r\nContent-Length: 3\r\n\r\nabcEXTRA"
        val input = ByteArrayInputStream(raw.toByteArray())
        val head = HttpWire.readHead(input)
        assertArrayEquals("abc".toByteArray(), HttpWire.readAll(HttpWire.bodyStream(head, input), 100))
    }

    @Test
    fun shortContentLengthBodyIsAnError() {
        val raw = "HTTP/1.1 200 OK\r\nContent-Length: 10\r\n\r\nabc"
        val input = ByteArrayInputStream(raw.toByteArray())
        val head = HttpWire.readHead(input)
        assertThrows(EOFException::class.java) { HttpWire.readAll(HttpWire.bodyStream(head, input), 100) }
    }

    @Test
    fun oversizedBodyIsRefused() {
        assertThrows(IOException::class.java) { HttpWire.readAll(ByteArrayInputStream(ByteArray(50)), 10) }
    }

    @Test
    fun readLineReturnsNullAtCleanEof() {
        assertNull(HttpWire.readLine(ByteArrayInputStream(ByteArray(0))))
    }
}
