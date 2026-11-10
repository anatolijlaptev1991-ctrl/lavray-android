// SPDX-License-Identifier: GPL-3.0-only
package io.github.anatolijlaptev1991_ctrl.lavray.ui.importer

import io.github.anatolijlaptev1991_ctrl.lavray.api.Connection
import io.github.anatolijlaptev1991_ctrl.lavray.api.ConnectFraming
import io.github.anatolijlaptev1991_ctrl.lavray.api.CoreEndpoint
import io.github.anatolijlaptev1991_ctrl.lavray.api.LavrayClient
import io.github.anatolijlaptev1991_ctrl.lavray.api.ProtoJson
import io.github.anatolijlaptev1991_ctrl.lavray.api.Transport
import io.github.anatolijlaptev1991_ctrl.lavray.api.gen.SubscriptionProgress
import io.github.anatolijlaptev1991_ctrl.lavray.ui.core.CoreSession
import kotlinx.coroutines.Dispatchers
import kotlinx.serialization.KSerializer
import java.io.ByteArrayInputStream
import java.io.ByteArrayOutputStream
import java.io.InputStream
import java.io.OutputStream
import java.util.concurrent.CopyOnWriteArrayList

/** What the fake core answers to one call. */
sealed interface Reply {
    data class Json(val body: String) : Reply
    data class Fail(val code: String, val message: String = "") : Reply
    data class Frames(val frames: List<SubscriptionProgress>, val endError: Pair<String, String>? = null) : Reply
}

fun <T> reply(serializer: KSerializer<T>, value: T): Reply = Reply.Json(ProtoJson.encodeToString(serializer, value))

/** One request the client sent: the method path and the JSON body (for a stream, the payload of its first frame). */
data class Call(val path: String, val body: String)

/**
 * A core that lives in memory: the real [LavrayClient] and [CoreSession] talk to it through [Transport], so
 * the request encoding and the response decoding in the tests are the ones the app uses.
 * A method with no handler answers "unimplemented", which fails a test that makes a call it did not expect.
 */
class FakeCore {
    val calls = CopyOnWriteArrayList<Call>()
    private val handlers = HashMap<String, (String) -> Reply>()

    fun on(path: String, handler: (String) -> Reply) {
        synchronized(handlers) { handlers[path] = handler }
    }

    fun on(path: String, fixed: Reply) = on(path) { fixed }

    fun callsTo(path: String): List<Call> = calls.filter { it.path == path }

    val session: CoreSession = CoreSession(
        { CoreEndpoint("/fake", "token") },
        { LavrayClient("token", transport, Dispatchers.IO) },
    )

    private val transport = Transport { FakeConnection() }

    private inner class FakeConnection : Connection {
        val written = ByteArrayOutputStream()
        override val output: OutputStream = written
        override val input: InputStream by lazy { ByteArrayInputStream(respond(written.toByteArray())) }
        override fun abort() = Unit
        override fun close() = Unit
    }

    private fun respond(request: ByteArray): ByteArray {
        val text = String(request, Charsets.ISO_8859_1)
        val split = text.indexOf("\r\n\r\n")
        val path = text.substringBefore(" HTTP/").substringAfter("POST ")
        val head = text.substring(0, split).lowercase()
        val rawBody = request.copyOfRange(split + 4, request.size)
        val stream = "application/connect+json" in head
        val body = String(if (stream && rawBody.size >= ConnectFraming.HEADER_SIZE) rawBody.copyOfRange(ConnectFraming.HEADER_SIZE, rawBody.size) else rawBody, Charsets.UTF_8)
        calls += Call(path, body)
        val handler = synchronized(handlers) { handlers[path] }
        val reply = handler?.invoke(body) ?: Reply.Fail("unimplemented", "no handler for $path")
        return when (reply) {
            is Reply.Json -> http(200, "application/json", reply.body.toByteArray())
            is Reply.Fail -> http(400, "application/json", failJson(reply.code, reply.message).toByteArray())
            is Reply.Frames -> http(200, "application/connect+json", frames(reply))
        }
    }

    private fun frames(reply: Reply.Frames): ByteArray {
        val out = ByteArrayOutputStream()
        for (f in reply.frames) out.write(ConnectFraming.encode(0, ProtoJson.encodeToString(SubscriptionProgress.serializer(), f).toByteArray()))
        val end = reply.endError?.let { "{\"error\":" + failJson(it.first, it.second) + "}" } ?: "{}"
        out.write(ConnectFraming.encode(2, end.toByteArray()))
        return out.toByteArray()
    }

    private fun failJson(code: String, message: String) = "{\"code\":\"$code\",\"message\":\"$message\"}"

    private fun http(status: Int, type: String, body: ByteArray): ByteArray {
        val head = "HTTP/1.1 $status X\r\nContent-Type: $type\r\nContent-Length: ${body.size}\r\n\r\n"
        return head.toByteArray(Charsets.ISO_8859_1) + body
    }
}
