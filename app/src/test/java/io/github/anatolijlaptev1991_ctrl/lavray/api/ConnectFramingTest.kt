// SPDX-License-Identifier: GPL-3.0-only
package io.github.anatolijlaptev1991_ctrl.lavray.api

import org.junit.Assert.assertArrayEquals
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNotNull
import org.junit.Assert.assertNull
import org.junit.Assert.assertThrows
import org.junit.Assert.assertTrue
import org.junit.Test
import java.io.ByteArrayInputStream
import java.io.EOFException

class ConnectFramingTest {
    private val message = "{\"state\":\"CORE_STATE_CONNECTED\"}".toByteArray()

    @Test
    fun encodeWritesFlagAndBigEndianLength() {
        val frame = ConnectFraming.encode(0, ByteArray(0x0102))
        assertEquals(5 + 0x0102, frame.size)
        assertArrayEquals(byteArrayOf(0, 0, 0, 1, 2), frame.copyOfRange(0, 5))
    }

    @Test
    fun readsFramesBackToBack() {
        val bytes = ConnectFraming.encode(0, message) + ConnectFraming.encode(0, "{}".toByteArray()) +
            ConnectFraming.encode(Frame.FLAG_END_STREAM, "{}".toByteArray())
        val reader = FrameReader(ByteArrayInputStream(bytes))
        assertArrayEquals(message, reader.next()!!.payload)
        assertEquals("{}", String(reader.next()!!.payload))
        val end = reader.next()!!
        assertTrue(end.isEndStream)
        assertNull(reader.next())
    }

    @Test
    fun frameSplitAtEveryReadSizeComesOutWhole() {
        val bytes = ConnectFraming.encode(0, message) + ConnectFraming.encode(Frame.FLAG_END_STREAM, "{}".toByteArray())
        for (step in 1..bytes.size) {
            val reader = FrameReader(TrickleInputStream(bytes, step))
            assertArrayEquals("step $step", message, reader.next()!!.payload)
            assertTrue("step $step", reader.next()!!.isEndStream)
        }
    }

    @Test
    fun framesSurviveChunkBoundariesInsideHeaderAndPayload() {
        val wire = ConnectFraming.encode(0, message) + ConnectFraming.encode(0, message) +
            ConnectFraming.encode(Frame.FLAG_END_STREAM, "{}".toByteArray())
        // Cut the wire into chunks of every size so boundaries land inside 5-byte headers and payloads.
        for (cut in 1..wire.size) {
            val parts = wire.toList().chunked(cut).map { it.toByteArray() }.toTypedArray()
            val body = chunked(*parts)
            val reader = FrameReader(ChunkedInputStream(TrickleInputStream(body, 3)))
            assertArrayEquals("cut $cut", message, reader.next()!!.payload)
            assertArrayEquals("cut $cut", message, reader.next()!!.payload)
            assertTrue("cut $cut", reader.next()!!.isEndStream)
            assertNull("cut $cut", reader.next())
        }
    }

    @Test
    fun emptyPayloadFrame() {
        val reader = FrameReader(ByteArrayInputStream(ConnectFraming.encode(0, ByteArray(0))))
        assertEquals(0, reader.next()!!.payload.size)
    }

    @Test
    fun endOfStreamInsideHeaderOrPayloadIsAnError() {
        val whole = ConnectFraming.encode(0, message)
        for (cut in 1 until whole.size) {
            val reader = FrameReader(ByteArrayInputStream(whole.copyOf(cut)))
            assertThrows("cut $cut", EOFException::class.java) { reader.next() }
        }
    }

    @Test
    fun oversizedLengthIsRefusedBeforeAllocating() {
        val header = byteArrayOf(0, 0x7f, 0xff.toByte(), 0xff.toByte(), 0xff.toByte())
        val ex = assertThrows(ApiException::class.java) { FrameReader(ByteArrayInputStream(header)).next() }
        assertEquals(ConnectCode.RESOURCE_EXHAUSTED, ex.code)
    }

    @Test
    fun flagsAreDecoded() {
        val reader = FrameReader(ByteArrayInputStream(ConnectFraming.encode(0x03, ByteArray(0))))
        val frame = reader.next()!!
        assertTrue(frame.isCompressed)
        assertTrue(frame.isEndStream)
        assertFalse(Frame(0, ByteArray(0)).isEndStream)
    }

    @Test
    fun cleanEndStreamHasNoError() {
        assertNull(errorFromEndStream("{}".toByteArray()))
        assertNull(errorFromEndStream(ByteArray(0)))
        assertNull(errorFromEndStream("{\"metadata\":{\"a\":[\"b\"]}}".toByteArray()))
    }

    @Test
    fun endStreamErrorCarriesCodeAndMessage() {
        val err = errorFromEndStream("{\"error\":{\"code\":\"unavailable\",\"message\":\"core is stopping\"}}".toByteArray())
        assertNotNull(err)
        assertEquals(ConnectCode.UNAVAILABLE, err!!.code)
        assertEquals("core is stopping", err.message)
    }

    @Test
    fun unknownErrorCodeBecomesUnknown() {
        val err = errorFromEndStream("{\"error\":{\"code\":\"brand_new\",\"message\":\"x\"}}".toByteArray())
        assertEquals(ConnectCode.UNKNOWN, err!!.code)
    }

    @Test
    fun malformedEndStreamIsAnInternalError() {
        val ex = assertThrows(ApiException::class.java) { errorFromEndStream("not json".toByteArray()) }
        assertEquals(ConnectCode.INTERNAL, ex.code)
    }

    @Test
    fun errorBodyFallsBackToHttpStatus() {
        assertEquals(ConnectCode.UNAUTHENTICATED, errorFromBody("".toByteArray(), 401).code)
        assertEquals(ConnectCode.UNAVAILABLE, errorFromBody("<html>".toByteArray(), 503).code)
        assertEquals(ConnectCode.NOT_FOUND, errorFromBody("{\"code\":\"not_found\",\"message\":\"no\"}".toByteArray(), 404).code)
    }
}
