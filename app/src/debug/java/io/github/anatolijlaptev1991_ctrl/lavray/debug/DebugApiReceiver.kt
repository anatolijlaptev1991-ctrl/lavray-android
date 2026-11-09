// SPDX-License-Identifier: GPL-3.0-only
package io.github.anatolijlaptev1991_ctrl.lavray.debug

import android.content.BroadcastReceiver
import android.content.Context
import android.content.Intent
import android.util.Base64
import android.util.Log
import io.github.anatolijlaptev1991_ctrl.lavray.api.CoreEndpoint
import io.github.anatolijlaptev1991_ctrl.lavray.api.FrameReader
import io.github.anatolijlaptev1991_ctrl.lavray.api.HttpWire
import io.github.anatolijlaptev1991_ctrl.lavray.api.LocalSocketTransport
import io.github.anatolijlaptev1991_ctrl.lavray.ui.VpnController
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.launch

/**
 * Debug-only driver for on-device verification (see src/debug/AndroidManifest.xml).
 *
 *   am broadcast -n <pkg>/.debug.DebugApiReceiver -a lavray.debug.API --es path /lavray.v1.CoreService/GetStatus --es b64 e30=
 *   extras: path, b64 (request JSON, default {}), stream (bool), notoken (bool), max (stream frames)
 *   other actions: lavray.debug.CONNECT (--el profile), lavray.debug.DISCONNECT, lavray.debug.AUTO (--ez on)
 *
 * Results go to logcat under the tag LVDBG. The bearer token is never printed.
 */
class DebugApiReceiver : BroadcastReceiver() {
    override fun onReceive(context: Context, intent: Intent) {
        // A stream can outlive the receiver time limit, so only short calls hold the broadcast open.
        val pending = if (intent.getBooleanExtra("stream", false)) null else goAsync()
        CoroutineScope(Dispatchers.IO).launch {
            try {
                dispatch(context.applicationContext, intent)
            } catch (e: Throwable) {
                Log.e(TAG, "failed: ${e.javaClass.simpleName}: ${e.message}")
            } finally {
                pending?.finish()
            }
        }
    }

    private suspend fun dispatch(context: Context, intent: Intent) {
        val controller = VpnController(context)
        controller.bind()
        try {
            when (intent.action) {
                ACTION_API -> callApi(controller.endpoint(), intent)
                ACTION_CONNECT -> controller.connect(intent.getLongExtra("profile", 0L))
                ACTION_DISCONNECT -> controller.disconnect()
                ACTION_AUTO -> controller.setAutoConnectEnabled(intent.getBooleanExtra("on", false))
                else -> Log.w(TAG, "unknown action ${intent.action}")
            }
            if (intent.action != ACTION_API) kotlinx.coroutines.delay(1500) // let the async binder call leave
        } finally {
            controller.close()
        }
    }

    private fun callApi(endpoint: CoreEndpoint, intent: Intent) {
        val path = intent.getStringExtra("path") ?: return
        val body = intent.getStringExtra("b64")?.let { String(Base64.decode(it, Base64.DEFAULT)) } ?: "{}"
        val stream = intent.getBooleanExtra("stream", false)
        val token = endpoint.token.takeUnless { intent.getBooleanExtra("notoken", false) }
        val conn = LocalSocketTransport(endpoint.socketPath).open()
        try {
            val payload = if (stream) io.github.anatolijlaptev1991_ctrl.lavray.api.ConnectFraming.encode(0, body.toByteArray()) else body.toByteArray()
            val headers = linkedMapOf(
                "Host" to "localhost",
                "Content-Type" to if (stream) "application/connect+json" else "application/json",
                "Connect-Protocol-Version" to "1",
                "Accept-Encoding" to "identity",
                "Connection" to "close",
            )
            if (token != null) headers["Authorization"] = "Bearer $token"
            HttpWire.writeRequest(conn.output, path, headers, payload)
            val head = HttpWire.readHead(conn.input)
            val bodyStream = HttpWire.bodyStream(head, conn.input)
            if (!stream || head.status != 200) {
                emit(path, "status=${head.status} ${String(HttpWire.readAll(bodyStream, 8 shl 20))}")
            } else {
                readFrames(path, FrameReader(bodyStream), intent.getIntExtra("max", 50))
            }
        } finally {
            conn.close()
        }
    }

    private fun readFrames(path: String, reader: FrameReader, max: Int) {
        var n = 0
        while (n < max) {
            val frame = reader.next() ?: break
            emit(path, "frame#$n end=${frame.isEndStream} ${String(frame.payload)}")
            if (frame.isEndStream) break
            n++
        }
    }

    private fun emit(path: String, text: String) {
        val head = "RESULT $path "
        text.chunked(LOG_CHUNK).forEachIndexed { i, part -> Log.i(TAG, if (i == 0) head + part else "  ...$part") }
    }

    private companion object {
        const val TAG = "LVDBG"
        const val LOG_CHUNK = 3000
        const val ACTION_API = "lavray.debug.API"
        const val ACTION_CONNECT = "lavray.debug.CONNECT"
        const val ACTION_DISCONNECT = "lavray.debug.DISCONNECT"
        const val ACTION_AUTO = "lavray.debug.AUTO"
    }
}
