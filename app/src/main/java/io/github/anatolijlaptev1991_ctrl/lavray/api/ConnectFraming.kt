// SPDX-License-Identifier: GPL-3.0-only
package io.github.anatolijlaptev1991_ctrl.lavray.api

import java.io.EOFException
import java.io.InputStream

/** One Connect streaming envelope: flag byte, 4-byte big-endian length, payload. */
internal class Frame(val flags: Int, val payload: ByteArray) {
    val isCompressed: Boolean get() = flags and FLAG_COMPRESSED != 0
    val isEndStream: Boolean get() = flags and FLAG_END_STREAM != 0

    companion object {
        const val FLAG_COMPRESSED = 0x01
        const val FLAG_END_STREAM = 0x02
    }
}

internal object ConnectFraming {
    const val HEADER_SIZE = 5
    const val MAX_PAYLOAD = 32 * 1024 * 1024

    fun encode(flags: Int, payload: ByteArray): ByteArray {
        val out = ByteArray(HEADER_SIZE + payload.size)
        out[0] = flags.toByte()
        out[1] = (payload.size ushr 24).toByte()
        out[2] = (payload.size ushr 16).toByte()
        out[3] = (payload.size ushr 8).toByte()
        out[4] = payload.size.toByte()
        payload.copyInto(out, HEADER_SIZE)
        return out
    }
}

/**
 * Pulls frames out of a byte stream. Reads are exact: a frame split across any
 * number of chunks or socket reads comes out whole. Returns null at a clean EOF
 * between frames and throws when the stream ends inside one.
 */
internal class FrameReader(private val input: InputStream) {
    private val header = ByteArray(ConnectFraming.HEADER_SIZE)

    fun next(): Frame? {
        if (!readFully(header, allowCleanEof = true)) return null
        val flags = header[0].toInt() and 0xFF
        val length = ((header[1].toInt() and 0xFF) shl 24) or
            ((header[2].toInt() and 0xFF) shl 16) or
            ((header[3].toInt() and 0xFF) shl 8) or
            (header[4].toInt() and 0xFF)
        if (length < 0 || length > ConnectFraming.MAX_PAYLOAD) {
            throw ApiException(ConnectCode.RESOURCE_EXHAUSTED, "stream frame of $length bytes is too large")
        }
        val payload = ByteArray(length)
        readFully(payload, allowCleanEof = false)
        return Frame(flags, payload)
    }

    private fun readFully(buf: ByteArray, allowCleanEof: Boolean): Boolean {
        var done = 0
        while (done < buf.size) {
            val n = input.read(buf, done, buf.size - done)
            if (n < 0) {
                if (done == 0 && allowCleanEof) return false
                throw EOFException("stream ended inside a frame")
            }
            done += n
        }
        return true
    }
}
