// SPDX-License-Identifier: GPL-3.0-only
package io.github.anatolijlaptev1991_ctrl.lavray.api

import kotlinx.coroutines.CoroutineDispatcher
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.async
import kotlinx.coroutines.channels.awaitClose
import kotlinx.coroutines.coroutineScope
import kotlinx.coroutines.flow.Flow
import kotlinx.coroutines.flow.channelFlow
import kotlinx.coroutines.launch
import kotlinx.coroutines.withContext
import kotlinx.serialization.KSerializer
import java.io.IOException

/**
 * Connect client for the core's unix socket. One socket per call, no pooling:
 * unary calls use application/json, server streams application/connect+json.
 * Cancelling a call (or a Flow collector) aborts its socket.
 */
class LavrayClient(
    private val token: String,
    private val transport: Transport,
    private val io: CoroutineDispatcher = Dispatchers.IO,
    private val maxUnaryBytes: Int = MAX_UNARY_BYTES,
) {
    constructor(endpoint: CoreEndpoint) : this(endpoint.token, LocalSocketTransport(endpoint.socketPath))

    override fun toString(): String = "LavrayClient(token=<redacted>)"

    suspend fun <Req, Res> unary(
        path: String,
        request: Req,
        requestSerializer: KSerializer<Req>,
        responseSerializer: KSerializer<Res>,
    ): Res {
        val body = ProtoJson.encodeToString(requestSerializer, request).toByteArray(Charsets.UTF_8)
        val bytes = callBlocking { conn -> exchangeUnary(conn, path, body) }
        return decode(responseSerializer, bytes)
    }

    fun <Req, Res> serverStream(
        path: String,
        request: Req,
        requestSerializer: KSerializer<Req>,
        responseSerializer: KSerializer<Res>,
    ): Flow<Res> = channelFlow {
        val body = ConnectFraming.encode(0, ProtoJson.encodeToString(requestSerializer, request).toByteArray(Charsets.UTF_8))
        val conn = withContext(io) { openOrUnavailable() }
        launch(io) {
            try {
                readStream(conn, path, body, responseSerializer) { send(it) }
                close()
            } catch (e: CancellationException) {
                throw e
            } catch (e: Throwable) {
                close(wrap(e))
            }
        }
        awaitClose { conn.abort() }
    }

    // ---- unary

    private fun exchangeUnary(conn: Connection, path: String, body: ByteArray): ByteArray {
        HttpWire.writeRequest(conn.output, path, headers("application/json"), body)
        val head = HttpWire.readHead(conn.input)
        val bytes = HttpWire.readAll(HttpWire.bodyStream(head, conn.input), maxUnaryBytes)
        if (head.status != 200) throw errorFromBody(bytes, head.status)
        return bytes
    }

    private fun <Res> decode(serializer: KSerializer<Res>, bytes: ByteArray): Res = try {
        ProtoJson.decodeFromString(serializer, String(bytes, Charsets.UTF_8))
    } catch (e: IllegalArgumentException) {
        throw ApiException(ConnectCode.INTERNAL, "unreadable response: ${e.message}", cause = e)
    }

    // Runs blocking socket work off the caller's thread; cancellation aborts the socket.
    private suspend fun <T> callBlocking(work: (Connection) -> T): T = coroutineScope {
        val conn = withContext(io) { openOrUnavailable() }
        val job = async(io) { guarded { work(conn) } }
        try {
            job.await()
        } catch (e: CancellationException) {
            conn.abort()
            throw e
        } finally {
            conn.close()
        }
    }

    // ---- server streams

    private suspend fun <Res> readStream(
        conn: Connection,
        path: String,
        body: ByteArray,
        serializer: KSerializer<Res>,
        emit: suspend (Res) -> Unit,
    ) {
        HttpWire.writeRequest(conn.output, path, headers("application/connect+json"), body)
        val head = HttpWire.readHead(conn.input)
        val stream = HttpWire.bodyStream(head, conn.input)
        if (head.status != 200) throw errorFromBody(HttpWire.readAll(stream, maxUnaryBytes), head.status)
        val reader = FrameReader(stream)
        while (true) {
            val frame = reader.next() ?: throw ApiException(ConnectCode.UNAVAILABLE, "stream closed without an end-of-stream frame")
            if (frame.isCompressed) throw ApiException(ConnectCode.INTERNAL, "compressed stream frames are not supported")
            if (frame.isEndStream) {
                errorFromEndStream(frame.payload)?.let { throw it }
                return
            }
            emit(decode(serializer, frame.payload))
        }
    }

    // ---- shared

    private fun openOrUnavailable(): Connection = try {
        transport.open()
    } catch (e: IOException) {
        throw ApiException(ConnectCode.UNAVAILABLE, ApiException.NOT_REACHABLE, cause = e)
    }

    private fun headers(contentType: String): Map<String, String> = linkedMapOf(
        "Host" to "localhost",
        "Content-Type" to contentType,
        "Connect-Protocol-Version" to "1",
        "Accept-Encoding" to "identity",
        "Authorization" to "Bearer $token",
        "Connection" to "close",
    )

    private inline fun <T> guarded(block: () -> T): T = try {
        block()
    } catch (e: IOException) {
        throw wrap(e)
    }

    /**
     * A transport failure is UNAVAILABLE (the screen then says the core is not there, and the session drops its client). A response that
     * was merely too large to hold is not that: the core answered, so it is RESOURCE_EXHAUSTED and the client stays.
     */
    private fun wrap(e: Throwable): Throwable = when {
        e is ApiException || e !is IOException -> e
        e is ResponseTooLargeException -> ApiException(ConnectCode.RESOURCE_EXHAUSTED, e.message ?: "response too large", cause = e)
        else -> ApiException(ConnectCode.UNAVAILABLE, e.message ?: "transport error", cause = e)
    }

    companion object {
        const val MAX_UNARY_BYTES = 32 * 1024 * 1024
    }
}
