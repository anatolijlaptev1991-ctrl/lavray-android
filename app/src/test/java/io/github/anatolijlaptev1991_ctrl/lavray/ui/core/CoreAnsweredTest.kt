// SPDX-License-Identifier: GPL-3.0-only
package io.github.anatolijlaptev1991_ctrl.lavray.ui.core

import io.github.anatolijlaptev1991_ctrl.lavray.api.ApiException
import io.github.anatolijlaptev1991_ctrl.lavray.api.Connection
import io.github.anatolijlaptev1991_ctrl.lavray.api.ConnectCode
import io.github.anatolijlaptev1991_ctrl.lavray.api.CoreEndpoint
import io.github.anatolijlaptev1991_ctrl.lavray.api.LavrayClient
import io.github.anatolijlaptev1991_ctrl.lavray.api.gen.coreConnect
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.runBlocking
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test
import java.io.ByteArrayInputStream
import java.io.ByteArrayOutputStream
import java.io.InputStream
import java.io.OutputStream
import java.util.concurrent.atomic.AtomicInteger

/** E-11: "unavailable" that the core itself answered (an outside service did not reply) is not "core unavailable". */
class CoreAnsweredTest {
    private class Conn(override val input: InputStream) : Connection {
        override val output: OutputStream = ByteArrayOutputStream()
        override fun abort() = Unit
        override fun close() = Unit
    }

    private fun http(status: String, body: String): ByteArray =
        ("HTTP/1.1 $status\r\nContent-Type: application/json\r\nContent-Length: ${body.toByteArray().size}\r\n\r\n$body").toByteArray()

    @Test fun anUnavailableAnswerFromTheCoreIsMarkedAsAnswered() = runBlocking {
        val asked = AtomicInteger()
        val reply = http("503 Service Unavailable", "{\"code\":\"unavailable\",\"message\":\"определить адрес не удалось\"}")
        val session = CoreSession(
            { asked.incrementAndGet(); CoreEndpoint("/x", "t") },
            { ep -> LavrayClient(ep.token, { Conn(ByteArrayInputStream(reply)) }, Dispatchers.IO) },
        )
        val first = session.call { coreConnect() }
        assertEquals(CoreResult.Fail(ErrorKind.CoreUnavailable, null, null, coreAnswered = true), first)
        session.call { coreConnect() }
        // The core answered, so the connection to it is fine and is kept: no new endpoint was asked for.
        assertEquals(1, asked.get())
    }

    @Test fun aGatewayStatusWithoutAnErrorBodyIsNotAnAnswerOfTheCore() {
        assertFalse(ApiException(ConnectCode.UNAVAILABLE, "HTTP 503", httpStatus = 503).answeredByCore())
        assertFalse(ApiException(ConnectCode.UNAVAILABLE, ApiException.NOT_REACHABLE).answeredByCore())
        assertFalse(java.io.IOException("ECONNREFUSED").answeredByCore())
        assertTrue(ApiException(ConnectCode.UNAVAILABLE, "no route", httpStatus = 503).answeredByCore())
    }
}
