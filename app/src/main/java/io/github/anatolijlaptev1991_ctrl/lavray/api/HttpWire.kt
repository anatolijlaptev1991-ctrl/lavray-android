// SPDX-License-Identifier: GPL-3.0-only
package io.github.anatolijlaptev1991_ctrl.lavray.api

import java.io.ByteArrayOutputStream
import java.io.EOFException
import java.io.IOException
import java.io.InputStream
import java.io.OutputStream

/** Status line and headers (names lower-cased) of an HTTP/1.1 response. */
internal class HttpHead(val status: Int, private val headers: Map<String, String>) {
    fun header(name: String): String? = headers[name.lowercase()]
}

/** A response beyond the size the client is willing to hold; the core answered, so this is not a transport failure. */
class ResponseTooLargeException(val limit: Int) : IOException("response larger than $limit bytes")

/** Minimal HTTP/1.1 over a byte stream: just what the unix-socket core needs. */
internal object HttpWire {
    private const val MAX_LINE = 8192
    private const val MAX_HEADERS = 100

    fun writeRequest(out: OutputStream, path: String, headers: Map<String, String>, body: ByteArray) {
        val head = StringBuilder(256)
        head.append("POST ").append(path).append(" HTTP/1.1\r\n")
        for ((name, value) in headers) head.append(name).append(": ").append(value).append("\r\n")
        head.append("Content-Length: ").append(body.size).append("\r\n\r\n")
        out.write(head.toString().toByteArray(Charsets.ISO_8859_1))
        out.write(body)
        out.flush()
    }

    fun readHead(input: InputStream): HttpHead {
        val statusLine = readLine(input) ?: throw EOFException("connection closed before a response")
        val parts = statusLine.split(' ', limit = 3)
        val status = parts.getOrNull(1)?.toIntOrNull()
        if (parts.size < 2 || !parts[0].startsWith("HTTP/1.") || status == null) {
            throw IOException("malformed status line")
        }
        val headers = HashMap<String, String>()
        while (true) {
            val line = readLine(input) ?: throw EOFException("connection closed inside headers")
            if (line.isEmpty()) break
            if (headers.size >= MAX_HEADERS) throw IOException("too many headers")
            val colon = line.indexOf(':')
            if (colon <= 0) throw IOException("malformed header line")
            headers[line.substring(0, colon).trim().lowercase()] = line.substring(colon + 1).trim()
        }
        return HttpHead(status, headers)
    }

    /** Body reader chosen from the head: chunked, Content-Length, or until close. */
    fun bodyStream(head: HttpHead, input: InputStream): InputStream {
        val encoding = head.header("transfer-encoding")
        if (encoding != null && encoding.lowercase().contains("chunked")) return ChunkedInputStream(input)
        val length = head.header("content-length")?.toLongOrNull()
        return if (length != null) BoundedInputStream(input, length) else input
    }

    fun readAll(input: InputStream, limit: Int): ByteArray {
        val buf = ByteArrayOutputStream()
        val chunk = ByteArray(8192)
        while (true) {
            val n = input.read(chunk)
            if (n < 0) return buf.toByteArray()
            if (buf.size() + n > limit) throw ResponseTooLargeException(limit)
            buf.write(chunk, 0, n)
        }
    }

    /** Reads up to LF and strips the CR. Returns null at EOF before any byte. */
    fun readLine(input: InputStream): String? {
        val sb = StringBuilder(64)
        while (true) {
            val b = input.read()
            if (b < 0) return if (sb.isEmpty()) null else throw EOFException("connection closed mid-line")
            if (b == '\n'.code) {
                if (sb.isNotEmpty() && sb[sb.length - 1] == '\r') sb.setLength(sb.length - 1)
                return sb.toString()
            }
            if (sb.length >= MAX_LINE) throw IOException("header line too long")
            sb.append(b.toChar())
        }
    }
}

/** Decodes Transfer-Encoding: chunked. Chunk boundaries may fall anywhere in the data it wraps. */
internal class ChunkedInputStream(private val src: InputStream) : InputStream() {
    private var remaining = 0L
    private var finished = false

    override fun read(): Int {
        val one = ByteArray(1)
        return if (read(one, 0, 1) < 0) -1 else one[0].toInt() and 0xFF
    }

    override fun read(b: ByteArray, off: Int, len: Int): Int {
        if (len == 0) return 0
        if (finished) return -1
        if (remaining == 0L && !nextChunk()) return -1
        val n = src.read(b, off, minOf(len.toLong(), remaining).toInt())
        if (n < 0) throw EOFException("connection closed inside a chunk")
        remaining -= n
        if (remaining == 0L) expectCrlf()
        return n
    }

    private fun nextChunk(): Boolean {
        val line = HttpWire.readLine(src) ?: throw EOFException("connection closed before the final chunk")
        val size = line.substringBefore(';').trim().toLongOrNull(16)
        if (size == null || size < 0) throw IOException("malformed chunk size")
        if (size == 0L) {
            skipTrailers()
            finished = true
            return false
        }
        remaining = size
        return true
    }

    private fun skipTrailers() {
        while (true) {
            val line = HttpWire.readLine(src) ?: return
            if (line.isEmpty()) return
        }
    }

    private fun expectCrlf() {
        if (src.read() != '\r'.code || src.read() != '\n'.code) throw IOException("missing CRLF after chunk")
    }
}

/** Reads exactly [left] bytes from [src]; EOF before that is an error. */
internal class BoundedInputStream(private val src: InputStream, private var left: Long) : InputStream() {
    override fun read(): Int {
        val one = ByteArray(1)
        return if (read(one, 0, 1) < 0) -1 else one[0].toInt() and 0xFF
    }

    override fun read(b: ByteArray, off: Int, len: Int): Int {
        if (left == 0L) return -1
        if (len == 0) return 0
        val n = src.read(b, off, minOf(len.toLong(), left).toInt())
        if (n < 0) throw EOFException("connection closed before Content-Length bytes arrived")
        left -= n
        return n
    }
}
